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
import {
  Activity,
  AlertTriangle,
  Boxes,
  Clock,
  Cpu,
  Wrench,
} from 'lucide-react';
import ActiveMonitoring from '@/components/gpu/ActiveMonitoring';
import CollectDonut from '@/components/gpu/CollectDonut';
import EventFeed from '@/components/gpu/EventFeed';
import ModelUtilChart from '@/components/gpu/ModelUtilChart';
import CspRollupCards from '@/components/gpu/CspRollupCards';
import EventStream from '@/components/gpu/EventStream';
import FaultTypeBar from '@/components/gpu/FaultTypeBar';
import GpuModelBar from '@/components/gpu/GpuModelBar';
import LiveStrip from '@/components/gpu/LiveStrip';
import SituationBoard from '@/components/gpu/SituationBoard';
import FleetFilterBar from '@/components/gpu/FleetFilterBar';
import FleetHeatmap from '@/components/gpu/FleetHeatmap';
import KpiTile from '@/components/gpu/KpiTile';
import NodeDrillSheet from '@/components/gpu/NodeDrillSheet';
import TenantDonut from '@/components/gpu/TenantDonut';
import TopGpuTable from '@/components/gpu/TopGpuTable';
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
import { deriveFleetFacts } from '@/lib/fleetFacts';
import { useLiveSeries } from '@/hooks/useLiveSeries';
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
  /** csp id → 최근 30분 사용률 추세(카드 스파크라인). */
  utilSeriesByCsp: Record<string, number[]>;
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
  const live = useLiveSeries(demoMode);
  const [filter, setFilter] = useState<FleetFilter>({});
  const [selected, setSelected] = useState<string | null>(null);

  // 데모 사전 집계 % (fleet util/alloc + csp util 3질의 = 1 tick) — demoMode
  // 판별 전/off 는 질의 자체를 skip(null 반환), 확정 시 deps 로 재구독.
  const agg = usePolledQuery<DemoAggregates | null>(
    async () => {
      if (demoMode !== true) return null;
      const end = Math.floor(Date.now() / 1000);
      const [utilRes, allocRes, cspRes, cspRangeRes] = await Promise.all([
        api.instantQuery(METRIC_DEMO_FLEET_UTIL_AVG),
        api.instantQuery(METRIC_DEMO_FLEET_ALLOC_PCT),
        api.instantQuery(METRIC_DEMO_CSP_UTIL_AVG),
        // CSP 4사 추세를 한 질의로 — 카드마다 따로 부르면 4배가 된다.
        api.rangeQuery(METRIC_DEMO_CSP_UTIL_AVG, end - 1800, end, 15),
      ]);
      const utilByCsp: Record<string, number> = {};
      for (const s of cspRes.data.result) {
        const csp = s.metric.csp;
        const v = Number(s.value[1]);
        if (csp && Number.isFinite(v)) utilByCsp[csp] = v;
      }
      const utilSeriesByCsp: Record<string, number[]> = {};
      for (const s of cspRangeRes.data.result) {
        const csp = s.metric.csp;
        if (!csp) continue;
        utilSeriesByCsp[csp] = s.values
          .map(([, raw]) => Number(raw))
          .filter((v) => Number.isFinite(v));
      }
      return {
        utilAvgPct: firstValue(utilRes),
        allocPct: firstValue(allocRes),
        utilByCsp,
        utilSeriesByCsp,
      };
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
        // 소속 라벨은 서버가 준 그대로 — 없으면 풀·테넌트·워크로드 필터에서
        // 이 노드 8장이 통째로 빠져 할당 집계가 어긋난다.
        nodePool: m.nodePool ?? '',
        pool: m.pool ?? '',
        tenant: m.tenant ?? '',
        workload: m.workload ?? '',
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

  // 단일 파생 계층 — 필터 축으로 갈리는 사실은 전부 여기서 나온다. 각 섹션은
  // state.* 대신 facts.* 를 받으므로 필터의 존재를 모른 채 필터에 반응한다.
  // state 가 계속 담당하는 것: 시나리오 단계 · 수집 상태 · 자동복구 진행 ·
  // 감사로그 · 룰셋(전부 필터 무관).
  const facts = useMemo(
    () =>
      deriveFleetFacts(filtered, statusOf, {
        scenario,
        missingInstances,
        cspSpecs: demoFleet?.csps ?? [],
        pools: demoFleet?.pools ?? [],
        tenants: demoFleet?.tenants ?? [],
        regions: demoFleet?.regions ?? [],
        alerts: state?.alerts ?? [],
        events: state?.events ?? [],
        serving: state?.serving ?? [],
        idle: state?.idle ?? [],
        severity: filter.severity,
      }),
    [filtered, statusOf, scenario, missingInstances, demoFleet, state, filter.severity],
  );

  // 상황판은 state 통째로 받는다 — 필터 반영분만 갈아끼워 주입하면 컴포넌트를
  // 고치지 않고도 필터를 따른다(수집·자동복구는 원본 유지).
  const boardState = useMemo(
    () =>
      state && {
        ...state,
        dashboard: {
          ...state.dashboard,
          gpus: facts.gpus,
          nodes: facts.nodes,
          alerts: facts.alerts,
        },
      },
    [state, facts],
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

  // KPI 파생 — 필터에 걸리는 값은 facts, 걸리지 않는 값(수집·자동복구)은 state.
  const alertSeverityBadges = facts.alerts
    .filter((s) => (s.key === 'critical' || s.key === 'major') && s.count > 0)
    .map((s) => ({
      text: `${s.label} ${s.count}`,
      tone: (s.key === 'critical' ? 'danger' : 'warn') as 'danger' | 'warn',
    }));
  const collectLag =
    state?.dashboard.collect.find((s) => s.tone === 'warn')?.count ?? null;
  const repairRunning =
    state?.dashboard.repair.find((s) => s.key === 'running')?.count ?? null;
  // 미니 막대 — 최근 30분 장애·지연의 이산 분포. 전용 시계열이 없으므로
  // 플릿 장애 롤업 추세를 그대로 쓴다(형태가 목적, 값은 목데이터).
  const faultBars = live.data?.errPct.values?.slice(-24);
  // 할당율·사용률은 필터에 걸리므로 셀에서 판다 — 사전 집계 instant 는 늘
  // 플릿 전체라 필터를 걸 수 없다(할당율은 서버가 안 주던 값이라 신설).
  const allocPct = demo
    ? facts.gpuCount > 0
      ? (facts.allocated / facts.gpuCount) * 100
      : null
    : (aggData?.allocPct ?? null);
  const utilAvgPct = demo ? facts.utilAvg : liveUtilAvg;
  const gapPp = allocPct !== null && utilAvgPct !== null ? allocPct - utilAvgPct : null;

  return (
    <div className="flex flex-col gap-4">
      {/* 0. 제목 부제 + 필터 바 + 범례 — 목업은 제목 아래 한 줄 설명, 그 줄 우측에
             필터칩이 붙는다. 최상단에 둬야 "필터 → KPI → 모니터링" 순서가 같아진다. */}
      <p className="-mt-2 text-muted-foreground text-sm">
        GPU 인프라의 실시간 상태와 주요 이슈를 한눈에 확인하세요.
      </p>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <FleetFilterBar
          options={fleet.options}
          filter={filter}
          onChange={setFilter}
          regionLabels={regionLabels}
        />
        <UtilLegend />
      </div>

      {/* 1. KPI 타일 행 — 데모는 목업 6카드(아이콘·배지·미니차트 슬롯 사용),
             실서비스는 산출원이 없는 지표가 많아 기존 축소 구성을 유지한다. */}
      {demo ? (
        <div
          className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6"
          aria-label="플릿 핵심 지표"
        >
          <KpiTile
            label="전체 GPU"
            icon={<Cpu className="size-4" />}
            value={demoFleet && formatCount(facts.allocated)}
            sub={demoFleet ? `/ ${formatCount(facts.gpuCount)}` : undefined}
            hint={
              allocPct === null
                ? undefined
                : `가용률 ${formatPct(100 - (facts.faulted / Math.max(facts.gpuCount, 1)) * 100)}`
            }
            spark={live.data?.util.values}
          />
          <KpiTile
            label="활성 장애"
            icon={<AlertTriangle className="size-4" />}
            tone="danger"
            value={demoFleet && formatCount(facts.faulted)}
            badges={alertSeverityBadges}
            bars={faultBars}
          />
          <KpiTile
            label="수집 지연"
            icon={<Clock className="size-4" />}
            tone="warn"
            value={collectLag === null ? null : formatCount(collectLag)}
            hint={
              demoFleet && collectLag !== null
                ? formatPct((collectLag / Math.max(demoFleet.nodeTotal, 1)) * 100, 1)
                : undefined
            }
            bars={faultBars}
          />
          <KpiTile
            label="저활용 GPU"
            icon={<Boxes className="size-4" />}
            value={demoFleet && formatCount(facts.underutilized)}
            hint="< 30% 사용률"
            bars={faultBars}
          />
          <KpiTile
            label="자동복구 실행 중"
            icon={<Wrench className="size-4" />}
            tone="info"
            value={repairRunning === null ? null : formatCount(repairRunning)}
            hint={
              demoFleet?.repairSuccessPct === undefined
                ? undefined
                : `성공률 ${formatPct(demoFleet.repairSuccessPct, 1)}`
            }
            progress={demoFleet?.repairSuccessPct}
          />
          <KpiTile
            label="활성 워크로드"
            icon={<Activity className="size-4" />}
            value={demoFleet && formatCount(facts.activeWorkloads)}
            hint="Pod 기준"
            spark={live.data?.rps.values}
          />
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3" aria-label="플릿 핵심 지표">
          <KpiTile
            label="총 노드"
            value={liveNodeCount === null ? null : formatCount(liveNodeCount)}
          />
          <KpiTile label="총 GPU" value={fleet.cells && formatCount(fleet.cells.length)} />
          <KpiTile
            label="GPU 사용률"
            value={fleet.cells && formatPct(liveUtilAvg)}
            hint="수집 셀 평균"
          />
        </div>
      )}

      {/* 2. 실시간 액티브 모니터링 + 우측 레일(이벤트 피드 · 수집 상태).
             목업의 중앙/우측 2열 구성 그대로 — 모니터링을 보면서 이벤트가
             흐르는 걸 같은 화면에서 본다. */}
      {demo ? (
        <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_20rem]">
          <ActiveMonitoring
            cells={fleet.cells === null ? null : filtered}
            csps={facts.csps}
            pools={facts.pools}
            statusOf={statusOf}
            onSelectNode={setSelected}
            gpuStatus={facts.gpus}
            alerts={facts.alertList}
            events={facts.eventList}
          />
          <div className="flex flex-col gap-3">
            {/* moreHref — 시나리오 2→3 이음새. 기본값 /gpu/health 는 구 대본 화면이라
                "전체 이벤트 보기"가 장애 상세를 비껴간다. */}
            <EventFeed
              alerts={facts.alertList}
              moreHref="/gpu/incident"
              modelOf={(a) =>
                mergedCells.find((c) => c.instance === a.instance && c.device === a.device)?.model
              }
            />
            {state ? <CollectDonut collect={state.dashboard.collect} /> : null}
          </div>
        </div>
      ) : null}

      {/* 3. ①②③ 상황판 — 문제 / 영향 / 다음 조치. 목업엔 없는 데모 서사 카드다.
             KPI 와 모니터링 사이에 있었는데, 목업은 그 둘이 붙어 있으므로
             모니터링 뒤로 물렸다(사용자 지적 2026-07-30). */}
      {demo && boardState ? (
        <SituationBoard state={boardState} onAct={onAct} acting={acting} />
      ) : null}

      {/* 4. CSP 롤업 + 5. 플릿 히트맵 — 목업(demo1)엔 없지만 런북 §1 Step 1 이
             "CSP 롤업 4장 + 7,000셀 플릿 히트맵 + 필터 + 노드 드릴다운"을 그 Step 의
             핵심 장면으로 지정한다(F-05-06). 화면 8·9번째에 있어 시연 때 한참
             스크롤해야 닿았다 — 목업 하단부(위젯·Top10) 앞으로 올린다. */}
      {demo ? (
        demoFleet ? (
          <CspRollupCards
            csps={facts.csps}
            utilByCsp={facts.utilByCsp}
            utilSeriesByCsp={aggData?.utilSeriesByCsp ?? null}
          />
        ) : (
          <Skeleton className="h-28 w-full" />
        )
      ) : null}

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

      {/* 6. 분석 위젯 행 — 모델별 현황 / 테넌트별 사용량 / 장애 유형 분포 /
             수집 상태. 전부 이미 서버가 주는 집계라 새 API 없이 조립한다. */}
      {/* 목업 하단은 네 위젯이 한 줄이고 폭이 2.6 : 1.25 : 1.05 : 1 이다(demo1 실측).
          균등 4칸이면 사용률 차트가 좁아 제목이 두 줄로 깨지고, 5칸(2+1+1+1)이면
          나머지 셋이 필요 이상으로 넓다 — 실측 비율을 그대로 준다. */}
      {demo && state ? (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-[2.6fr_1.25fr_1.05fr_1fr]">
          {/* 좌측 = GPU 사용률(실시간) 4탭 차트. 목업 하단의 주역. */}
          <div className="md:col-span-2 xl:col-span-1">
            <ModelUtilChart demoMode={demoMode} />
          </div>
          <GpuModelBar models={facts.models} />
          <TenantDonut pools={facts.pools} tenants={facts.tenants} />
          <FaultTypeBar alerts={facts.alertList} />
        </div>
      ) : null}

      {/* 7. GPU 사용률 Top 10 — 벽면과 같은 per-GPU 셀에서 상위만 표로.
             상태 배지는 시나리오를 아는 이 화면이 분류자를 주입한다.
             목업(demo1) 순서는 1·2·6·7 이 담당한다(KPI → 모니터링 → 위젯 →
             Top10). 3~5 는 런북·데모 보강이고 8 이후도 마찬가지다. */}
      {demo ? (
        <TopGpuTable
          cells={fleet.cells === null ? null : filtered}
          statusOf={statusOf}
          alerts={facts.alertList}
        />
      ) : null}

      {/* 8. 실시간 지표 띠 — 큰 숫자 + 30분 추세선 */}
      {demo ? <LiveStrip series={live.data ?? null} /> : null}

      {/* 9. 할당 vs 실사용 갭 스트립 (데모 전용) */}
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
              {facts.idleList.length === 0 ? (
                <p className="text-xs text-muted-foreground">저활용 대상 없음</p>
              ) : (
                <ul className="flex flex-col gap-1.5">
                  {/* idle[] 에는 1~29일 최근 유휴가 섞여 온다(유휴장비 관리 화면의
                      "전체 유휴" 축) — 무정렬 slice 는 "회수 검토" 제목 아래 12일짜리를
                      올린다. 지속 일수 내림차순으로 자른다. */}
                  {[...facts.idleList]
                    .sort((a, b) => b.idleDays - a.idleDays)
                    .slice(0, 6)
                    .map((g) => (
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
          <EventStream events={facts.eventList} />
        </div>
      ) : null}

      <NodeDrillSheet instance={selected} onClose={() => setSelected(null)} />
    </div>
  );
}
