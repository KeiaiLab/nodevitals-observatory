// GpuOverview — 통합 상황판. 배치 순서가 곧 서사다(사용자 지시 2026-07-27):
//   ① 지금 문제가 무엇인가 → ② 어디까지 영향인가 → ③ 지금 무엇을 해야 하는가
// SituationBoard 가 그 셋을 한 줄에 답하고, 아래로 CSP 롤업 → 공통 필터 →
// 플릿 히트맵 → 저활용·최근 이벤트 순으로 근거를 깐다.
// 데이터 계층 3단: 카운트 = DemoState.fleet(GpuLayout 5s 폴링 공급) / 집계 % =
// 데모 사전 집계 instant(15s) / 벽면 = useFleet per-GPU instant(30s).
// 데모 off 강등: 벽면 셀 클라이언트 계산(노드 distinct·총 GPU·평균 util)만
// 표시 — 할당·장애·온도 이상·Agent Missing 은 데모 집계 없이는 산출원이 없어
// 타일 자체를 숨긴다(가정 명시: 명세의 숨김 목록에 온도 이상도 포함 취급).
import { useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router';
import CspRollupCards from '@/components/gpu/CspRollupCards';
import EventStream from '@/components/gpu/EventStream';
import SituationBoard from '@/components/gpu/SituationBoard';
import FleetFilterBar from '@/components/gpu/FleetFilterBar';
import FleetHeatmap from '@/components/gpu/FleetHeatmap';
import KpiTile from '@/components/gpu/KpiTile';
import NodeDrillSheet from '@/components/gpu/NodeDrillSheet';
import UtilLegend from '@/components/gpu/UtilLegend';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemo } from '@/hooks/demoContext';
import {
  applyFleetFilter,
  type CellStatus,
  type FleetCell,
  type FleetFilter,
  groupClusterChunks,
  useFleet,
} from '@/hooks/useFleet';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type ApiEnvelope, type VectorData } from '@/lib/api';
import {
  formatCount,
  formatPct,
  METRIC_DEMO_CSP_UTIL_AVG,
  METRIC_DEMO_FLEET_ALLOC_PCT,
  METRIC_DEMO_FLEET_UTIL_AVG,
} from '@/lib/gpuMetrics';

const AGG_POLL_MS = 15_000;

interface DemoAggregates {
  utilAvgPct: number | null;
  allocPct: number | null;
  utilByCsp: Record<string, number>;
}

function firstValue(res: ApiEnvelope<VectorData>): number | null {
  const raw = res.data.result[0]?.value[1];
  if (raw === undefined) return null;
  const v = Number(raw);
  return Number.isFinite(v) ? v : null;
}

export default function GpuOverview() {
  const { demoMode, state, act } = useDemo();
  const [acting, setActing] = useState(false);
  const fleet = useFleet();
  const [filter, setFilter] = useState<FleetFilter>({});
  const [selected, setSelected] = useState<string | null>(null);

  // 데모 사전 집계 % (fleet util/alloc + csp util 3질의 = 1 tick) — demoMode
  // 판별 전/off 는 질의 자체를 skip(null 반환), 확정 시 deps 로 재구독.
  const agg = usePolledQuery<DemoAggregates | null>(
    async () => {
      if (demoMode !== true) return null;
      const [utilRes, allocRes, cspRes] = await Promise.all([
        api.instantQuery(METRIC_DEMO_FLEET_UTIL_AVG),
        api.instantQuery(METRIC_DEMO_FLEET_ALLOC_PCT),
        api.instantQuery(METRIC_DEMO_CSP_UTIL_AVG),
      ]);
      const utilByCsp: Record<string, number> = {};
      for (const s of cspRes.data.result) {
        const csp = s.metric.csp;
        const v = Number(s.value[1]);
        if (csp && Number.isFinite(v)) utilByCsp[csp] = v;
      }
      return { utilAvgPct: firstValue(utilRes), allocPct: firstValue(allocRes), utilByCsp };
    },
    AGG_POLL_MS,
    [demoMode],
  );

  const demoFleet = demoMode === true ? (state?.fleet ?? null) : null;

  // Agent Missing 노드의 GPU 는 시리즈가 없어 벽면에서 빠진다 — state 의
  // missingGpus 로 '데이터 없음' 합성 셀(util NaN)을 주입해 총 셀 수를 인벤토리
  // 전수(예: 7,000)와 일치시킨다(전 화면 단일 목데이터 정합 계약). 이미 셀이
  // 있는 instance 는 중복 주입하지 않는다(복구 직후 폴링 시차 방어).
  const mergedCells = useMemo(() => {
    const base = fleet.cells ?? [];
    const missing = demoFleet?.missingGpus ?? [];
    if (missing.length === 0) return base;
    const present = new Set(base.map((c) => c.instance));
    const synthetic = missing
      .filter((m) => !present.has(m.instance))
      .map((m) => ({
        instance: m.instance,
        device: m.device,
        uuid: m.uuid,
        model: '',
        csp: m.csp,
        region: m.region,
        cluster: m.cluster,
        pool: '',
        tenant: '',
        util: Number.NaN,
        band: 0,
      }));
    if (synthetic.length === 0) return base;
    return [...base, ...synthetic].sort(
      (a, b) =>
        a.csp.localeCompare(b.csp) ||
        a.cluster.localeCompare(b.cluster) ||
        a.instance.localeCompare(b.instance) ||
        a.device.localeCompare(b.device),
    );
  }, [fleet.cells, demoFleet]);

  const faultUuids = useMemo(() => new Set(demoFleet?.faultUuids ?? []), [demoFleet]);
  const tempAlertUuids = useMemo(() => new Set(demoFleet?.tempAlertUuids ?? []), [demoFleet]);
  const missingInstances = useMemo(() => new Set(demoFleet?.missingInstances ?? []), [demoFleet]);

  // 상태(정상/성능저하/장애/격리/복구중)는 라벨이 아니라 시나리오 집합에서
  // 나온다 — 서버 상황판(dashboard.gpus)과 같은 어휘를 쓴다.
  const scenario = state?.scenario ?? null;
  const statusOf = useCallback(
    (cell: FleetCell): CellStatus => {
      if (faultUuids.has(cell.uuid)) return 'fault';
      if (scenario && cell.instance === scenario.victim.instance) {
        if (scenario.phase === 'draining' || scenario.phase === 'replacing') return 'isolated';
        if (
          scenario.phase === 'burnin-1' ||
          scenario.phase === 'burnin-failed' ||
          scenario.phase === 'burnin-2' ||
          scenario.phase === 'ready-to-return'
        ) {
          return 'recovering';
        }
      }
      if (tempAlertUuids.has(cell.uuid)) return 'degraded';
      return 'normal';
    },
    [faultUuids, tempAlertUuids, scenario],
  );

  const filtered = useMemo(
    () => applyFleetFilter(mergedCells, filter, statusOf),
    [mergedCells, filter, statusOf],
  );

  const regionLabels = useMemo(() => {
    const out: Record<string, string> = {};
    for (const r of demoFleet?.regions ?? []) out[r.id] = `${r.display} (${r.csp})`;
    return out;
  }, [demoFleet]);

  async function onAct(action: Parameters<typeof act>[0]) {
    if (acting) return;
    setActing(true);
    await act(action);
    setActing(false);
  }
  const chunks = useMemo(() => groupClusterChunks(filtered), [filtered]);


  // 데모 off 강등 — 벽면 셀 기반 클라이언트 계산.
  const liveNodeCount = useMemo(
    () => (fleet.cells ? new Set(fleet.cells.map((c) => c.instance)).size : null),
    [fleet.cells],
  );
  const liveUtilAvg = useMemo(() => {
    if (!fleet.cells || fleet.cells.length === 0) return null;
    return fleet.cells.reduce((sum, c) => sum + c.util, 0) / fleet.cells.length;
  }, [fleet.cells]);

  // demoMode null = /demo/status 판별 중 — 화면 전체 Skeleton.
  if (demoMode === null) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
          {[0, 1, 2, 3, 4, 5, 6].map((i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </div>
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-72 w-full" />
      </div>
    );
  }

  const demo = demoMode === true;
  const aggData = agg.data ?? null;
  const allocPct = aggData?.allocPct ?? null;
  const utilAvgPct = demo ? (aggData?.utilAvgPct ?? null) : liveUtilAvg;
  const gapPp = allocPct !== null && utilAvgPct !== null ? allocPct - utilAvgPct : null;

  return (
    <div className="flex flex-col gap-4">
      {/* ①②③ 상황판 — 문제 / 영향 / 다음 조치 */}
      {demo && state ? (
        <SituationBoard state={state} onAct={onAct} acting={acting} />
      ) : null}

      {/* 1. KPI 타일 행 */}
      <div
        className={
          demo
            ? 'grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7'
            : 'grid grid-cols-2 gap-3 md:grid-cols-3'
        }
        aria-label="플릿 핵심 지표"
      >
        <KpiTile
          label="총 노드"
          value={
            demo
              ? demoFleet && formatCount(demoFleet.nodeTotal)
              : liveNodeCount === null
                ? null
                : formatCount(liveNodeCount)
          }
        />
        <KpiTile
          label="총 GPU"
          value={
            demo
              ? demoFleet && formatCount(demoFleet.gpuTotal)
              : fleet.cells && formatCount(fleet.cells.length)
          }
        />
        {demo ? (
          <KpiTile
            label="할당"
            value={
              demoFleet &&
              `${formatCount(demoFleet.allocated)} / ${formatCount(demoFleet.gpuTotal)}`
            }
            hint={`할당율 ${formatPct(allocPct)}`}
          />
        ) : null}
        <KpiTile
          label="GPU 사용률"
          value={demo ? aggData && formatPct(aggData.utilAvgPct) : fleet.cells && formatPct(liveUtilAvg)}
          hint={demo ? '할당 기준 평균' : '수집 셀 평균'}
        />
        {demo ? (
          <KpiTile
            label="GPU 장애"
            value={demoFleet && formatCount(demoFleet.faulted)}
            tone="danger"
          />
        ) : null}
        {demo ? (
          <KpiTile
            label="온도 이상"
            value={demoFleet && formatCount(demoFleet.tempAlerts)}
            tone="warn"
          />
        ) : null}
        {demo ? (
          <KpiTile
            label="Agent Missing"
            value={demoFleet && formatCount(demoFleet.agentMissing)}
            tone={demoFleet && demoFleet.agentMissing > 0 ? 'warn' : 'default'}
          />
        ) : null}
      </div>

      {/* 2. CSP 롤업 카드 행 (데모 전용) */}
      {demo ? (
        demoFleet ? (
          <CspRollupCards csps={demoFleet.csps} utilByCsp={aggData ? aggData.utilByCsp : null} />
        ) : (
          <Skeleton className="h-28 w-full" />
        )
      ) : null}

      {/* 3. 할당 vs 실사용 갭 스트립 (데모 전용) */}
      {demo ? (
        <Card className="gap-0 py-0">
          <CardContent className="flex flex-wrap items-center gap-x-4 gap-y-2 p-4">
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-muted-foreground text-sm">할당</span>
              <span className="font-semibold text-3xl tabular-nums">{formatPct(allocPct)}</span>
              <span className="text-muted-foreground text-sm">vs 실사용</span>
              <span className="font-semibold text-3xl text-metric-gpu tabular-nums">
                {formatPct(utilAvgPct)}
              </span>
              {gapPp !== null ? (
                <Badge variant="outline" className="text-metric-thermal">
                  갭 {gapPp.toFixed(1)}pp
                </Badge>
              ) : null}
            </div>
            <Link
              to="/gpu/efficiency"
              className="ml-auto text-primary text-sm underline-offset-4 hover:underline"
            >
              효율 화면에서 상세 →
            </Link>
          </CardContent>
        </Card>
      ) : null}

      {/* 4. 필터 바 + 범례 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <FleetFilterBar
          options={fleet.options}
          filter={filter}
          onChange={setFilter}
          regionLabels={regionLabels}
        />
        <UtilLegend />
      </div>

      {/* 5. 플릿 히트맵 */}
      <Card>
        <CardHeader>
          <CardTitle>플릿 히트맵 — GPU {formatCount(filtered.length)}장</CardTitle>
          <CardDescription>
            {fleet.updatedAt
              ? `마지막 갱신 ${new Date(fleet.updatedAt).toLocaleTimeString('ko-KR')} · 30초마다 자동 갱신`
              : '벽면 수집 중…'}
            {demoFleet && demoFleet.agentMissing > 0
              ? ` · 수집 중단 ${formatCount(demoFleet.agentMissing)}대는 점선 셀(데이터 없음)로 표시`
              : ''}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {fleet.error ? (
            <p role="alert" className="mb-3 text-destructive text-sm">
              벽면 갱신 실패: {fleet.error}
            </p>
          ) : null}
          {fleet.cells === null ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-6 w-48" />
              <Skeleton className="h-40 w-full" />
            </div>
          ) : (
            <FleetHeatmap
              chunks={chunks}
              faultUuids={faultUuids}
              tempAlertUuids={tempAlertUuids}
              missingInstances={missingInstances}
              onSelectNode={setSelected}
            />
          )}
        </CardContent>
      </Card>

      {/* 근거 하단 — 저활용 회수 후보 + 최근 이벤트 스트림 */}
      {demo && state ? (
        <div className="grid gap-3 xl:grid-cols-2">
          <Card className="gap-0 py-0">
            <CardHeader className="p-4 pb-2">
              <CardTitle className="text-sm">저활용 GPU — 회수 검토</CardTitle>
              <CardDescription className="text-xs">
                실시간 저활용 개체와 지속 일수. 상위 6건.
              </CardDescription>
            </CardHeader>
            <CardContent className="p-4 pt-0">
              {state.idle.length === 0 ? (
                <p className="text-xs text-muted-foreground">저활용 대상 없음</p>
              ) : (
                <ul className="flex flex-col gap-1.5">
                  {state.idle.slice(0, 6).map((g) => (
                    <li key={g.uuid} className="flex items-center gap-2 text-xs">
                      <span className="truncate font-medium">{g.instance}</span>
                      <span className="text-muted-foreground">{g.device}</span>
                      <span className="truncate text-muted-foreground">{g.pool || '미할당'}</span>
                      <span className="ml-auto shrink-0 font-semibold tabular-nums text-metric-thermal">
                        {g.idleDays}일
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              <Link
                to="/gpu/efficiency"
                className="mt-3 inline-block text-primary text-xs underline-offset-4 hover:underline"
              >
                효율 화면에서 전체 보기 →
              </Link>
            </CardContent>
          </Card>
          <EventStream events={state.events} />
        </div>
      ) : null}

      <NodeDrillSheet instance={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
