// GpuOverview — 임원 시연 Step 1 "통합관제": KPI 타일 행 → CSP 롤업(데모) →
// 할당 vs 실사용 갭 스트립(데모) → 필터/범례 → 플릿 히트맵 → 노드 드릴다운.
// 데이터 계층 3단: 카운트 = DemoState.fleet(GpuLayout 5s 폴링 공급) / 집계 % =
// 데모 사전 집계 instant(15s) / 벽면 = useFleet per-GPU instant(30s).
// 데모 off 강등: 벽면 셀 클라이언트 계산(노드 distinct·총 GPU·평균 util)만
// 표시 — 할당·장애·온도 이상·Agent Missing 은 데모 집계 없이는 산출원이 없어
// 타일 자체를 숨긴다(가정 명시: 명세의 숨김 목록에 온도 이상도 포함 취급).
import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import CspRollupCards from '@/components/gpu/CspRollupCards';
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
  const { demoMode, state } = useDemo();
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

  const filtered = useMemo(() => applyFleetFilter(mergedCells, filter), [mergedCells, filter]);
  const chunks = useMemo(() => groupClusterChunks(filtered), [filtered]);

  const faultUuids = useMemo(() => new Set(demoFleet?.faultUuids ?? []), [demoFleet]);
  const tempAlertUuids = useMemo(() => new Set(demoFleet?.tempAlertUuids ?? []), [demoFleet]);
  const missingInstances = useMemo(() => new Set(demoFleet?.missingInstances ?? []), [demoFleet]);

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
        <FleetFilterBar options={fleet.options} filter={filter} onChange={setFilter} />
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

      <NodeDrillSheet instance={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
