// fleetFacts — 통합 상황판의 *단일 파생 계층*.
//
// 경계 규칙 한 줄: **필터 축으로 갈리는 값이면 cells 에서 파생한다. 안 갈리면
// state 를 쓴다.** 서버 사전 집계(state.dashboard·state.fleet)는 플릿 전체를
// 말하므로 필터를 걸 수 없다 — 축을 하나 더 붙일 때마다 서버에 축별 집계를
// 한 벌씩 더 만드는 건 8축 × N집계로 발산하고, 분류 로직이 서버·화면 두 곳에
// 생겨 드리프트한다(같은 레포에서 이미 겪은 유형).
//
// 그래서 필터 가능한 사실은 전부 *필터된 셀*에서 다시 판다. 축을 추가해도
// applyFleetFilter 한 곳만 고치면 전 섹션이 따라온다. state 가 계속 담당하는
// 것은 필터와 무관한 사실뿐이다 — 시나리오 단계 / 수집 상태 / 자동복구 진행 /
// 감사로그 / 룰셋.
//
// 출력은 컴포넌트가 이미 기대하는 형태 그대로다(props 를 바꾸지 않는다).
// 컴포넌트는 필터의 존재를 모른 채 필터에 반응한다.
import type { CellStatus, FleetCell } from '@/hooks/useFleet';
import type {
  AlertEvent,
  ClusterEvent,
  CSPSummary,
  FleetSummary,
  IdleGPU,
  PoolSummary,
  RegionInfo,
  ScenarioState,
  ServingStats,
  StatusCount,
} from '@/lib/demoApi';

/** 테넌트 요약 — demoApi 의 FleetSummary 인라인 타입을 그대로 쓴다(재선언 금지). */
type TenantSummary = FleetSummary['tenants'][number];

/** 저활용 임계 — KPI 카드의 "< 30% 사용률" 문구와 같은 값(둘이 갈리면 거짓말). */
const UNDERUTIL_PCT = 30;

/** 격리·복구 단계 — statusOf 와 같은 어휘(GpuOverview). 노드 상태도 같은 축을 쓴다. */
const DRAINING_PHASE = 'draining';
const CORDONED_PHASES = new Set([
  'replacing',
  'burnin-1',
  'burnin-failed',
  'burnin-2',
  'ready-to-return',
]);

/** 파생에 필요한 *필터 무관* 컨텍스트. 전부 state 에서 그대로 온다. */
export interface FleetFactsContext {
  scenario: ScenarioState | null;
  /** 수집 중단 노드 — 셀 라벨이 아니라 시나리오가 아는 사실. */
  missingInstances: Set<string>;
  /** CSP 표시명·어댑터와 **카드 순서**의 출처. 롤업 카드가 흔들리지 않게 한다. */
  cspSpecs: CSPSummary[];
  /** 풀·테넌트 표시명 사전(셀에는 id 만 있다). */
  pools: PoolSummary[];
  tenants: TenantSummary[];
  regions: RegionInfo[];
  alerts: AlertEvent[];
  events: ClusterEvent[];
  /** 서빙 풀 상태 — 활성 워크로드(Pod)는 셀이 아니라 이쪽에 있다. */
  serving: ServingStats[];
  idle: IdleGPU[];
  /** 심각도 축 — 셀에는 심각도가 없어 applyFleetFilter 가 못 거른다(알림 전용
   *  축이다). 여기서 걸지 않으면 필터바의 심각도 셀렉터가 아무것도 안 한다. */
  severity?: string;
}

export interface FleetFacts {
  /** state.dashboard.gpus 와 같은 형태·같은 어휘. */
  gpus: StatusCount[];
  nodes: StatusCount[];
  alerts: StatusCount[];
  /** state.fleet.csps 와 같은 형태(순서도 spec 순 유지). */
  csps: CSPSummary[];
  /** csp id → 평균 사용률. CspRollupCards 가 받던 사전 집계 맵과 같은 형태. */
  utilByCsp: Record<string, number>;
  /** state.fleet.models 와 같은 형태. */
  models: Array<{ model: string; count: number; utilAvgPct?: number }>;
  /** state.fleet.pools / tenants 와 같은 형태(필터에 걸린 것만 남는다). */
  pools: PoolSummary[];
  tenants: TenantSummary[];
  regions: RegionInfo[];
  /** 필터에 걸린 알림·이벤트·유휴 목록(피드·분포 차트·회수 후보 소재). */
  alertList: AlertEvent[];
  eventList: ClusterEvent[];
  idleList: IdleGPU[];
  nodeCount: number;
  gpuCount: number;
  allocated: number;
  faulted: number;
  /** 온도 이상 = 상태 분류의 degraded 와 같은 집합. */
  tempAlerts: number;
  agentMissing: number;
  underutilized: number;
  /** 필터에 걸린 서빙 풀의 Ready 레플리카 합(Pod 기준). */
  activeWorkloads: number;
  /** 할당 GPU 평균 사용률. 미할당은 늘 ~0 이라 섞으면 플릿 평균이 주저앉는다. */
  utilAvg: number | null;
}

/** 미할당 GPU 는 pool 라벨이 없다(buildSeriesLabels) — 할당 여부의 유일한 근거. */
function isAllocated(c: FleetCell): boolean {
  return c.pool !== '';
}

/** 수집 중단 노드의 합성 셀은 util 이 NaN 이다 — 평균에서 빼지 않으면 전부 NaN. */
function hasUtil(c: FleetCell): boolean {
  return Number.isFinite(c.util);
}

function mean(values: number[]): number | null {
  return values.length === 0 ? null : values.reduce((a, b) => a + b, 0) / values.length;
}

/**
 * deriveFleetFacts — 필터된 셀에서 상황판이 쓰는 사실을 전부 판다.
 *
 * 서버 집계와의 등가는 필터 "전체"에서 성립해야 한다(회귀 기준):
 * gpus / nodes / alerts / csps / utilAvg / underutilized 가 각각
 * dashboard.gpus / dashboard.nodes / dashboard.alerts / fleet.csps /
 * demo_fleet_util_avg_pct / fleet.underutilized 와 일치한다.
 */
export function deriveFleetFacts(
  cells: FleetCell[],
  statusOf: (cell: FleetCell) => CellStatus,
  ctx: FleetFactsContext,
): FleetFacts {
  const victim = ctx.scenario?.victim.instance ?? '';
  const phase = ctx.scenario?.phase ?? '';

  // ---- GPU 상태 5분류 (상호배타, 합 = 필터된 셀 수) ----
  const gpuBy: Record<CellStatus, number> = {
    normal: 0,
    degraded: 0,
    fault: 0,
    isolated: 0,
    recovering: 0,
  };

  // ---- 노드 상태: 셀을 노드로 접는다. 상태는 노드 단위 사건이라 셀 상태를
  //      그대로 못 쓴다(GPU 1장 장애 → 노드는 Degraded 1대).
  const nodeDegraded = new Set<string>();
  const nodes = new Set<string>();
  // 유휴 목록은 개체 단위(uuid)라 노드 소속으로 거르면 같은 노드의 비유휴까지 남는다.
  const uuids = new Set<string>();

  // ---- CSP / 모델 / 풀 롤업 ----
  const perCsp = new Map<string, { gpuTotal: number; allocated: number; faulted: number }>();
  const cspUtil = new Map<string, number[]>();
  const perModel = new Map<string, { count: number; utils: number[] }>();
  const poolAlloc = new Map<string, number>();
  const tenantIds = new Set<string>();
  const regionIds = new Set<string>();

  let allocated = 0;
  let underutilized = 0;
  const allocUtils: number[] = [];

  for (const c of cells) {
    const s = statusOf(c);
    gpuBy[s] += 1;
    nodes.add(c.instance);
    uuids.add(c.uuid);
    if (s === 'fault' || s === 'degraded') nodeDegraded.add(c.instance);

    const csp = perCsp.get(c.csp) ?? { gpuTotal: 0, allocated: 0, faulted: 0 };
    csp.gpuTotal += 1;
    if (s === 'fault') csp.faulted += 1;
    perCsp.set(c.csp, csp);
    if (c.region) regionIds.add(c.region);

    const alloc = isAllocated(c);
    if (alloc) {
      allocated += 1;
      csp.allocated += 1;
      poolAlloc.set(c.pool, (poolAlloc.get(c.pool) ?? 0) + 1);
      if (c.tenant) tenantIds.add(c.tenant);
      // 사용률 집계는 *할당 GPU 만* — 서버 집계(demo_fleet_util_avg_pct)와 같은
      // 모집단이다. 미할당(util ≈ 0)을 섞으면 필터 "전체"에서 4pp 어긋난다.
      if (hasUtil(c)) {
        allocUtils.push(c.util);
        const bucket = cspUtil.get(c.csp);
        if (bucket) bucket.push(c.util);
        else cspUtil.set(c.csp, [c.util]);
        if (c.util < UNDERUTIL_PCT) underutilized += 1;
      }
    }

    if (c.model) {
      const m = perModel.get(c.model) ?? { count: 0, utils: [] };
      m.count += 1;
      if (hasUtil(c)) m.utils.push(c.util);
      perModel.set(c.model, m);
    }
  }

  let nReady = 0;
  let nDegraded = 0;
  let nCordoned = 0;
  let nDraining = 0;
  let nOffline = 0;
  for (const instance of nodes) {
    if (ctx.missingInstances.has(instance)) nOffline += 1;
    else if (instance === victim && phase === DRAINING_PHASE) nDraining += 1;
    else if (instance === victim && CORDONED_PHASES.has(phase)) nCordoned += 1;
    else if (nodeDegraded.has(instance)) nDegraded += 1;
    else nReady += 1;
  }

  // ---- 알림·이벤트: 필터된 노드에 속한 것만. 인스턴스가 없는 항목은 플릿
  //      전역 사건이라 어느 필터에서도 여전히 일어난 일이다(남긴다).
  const inScope = (instance: string | undefined) => !instance || nodes.has(instance);
  const alertList = ctx.alerts.filter(
    (a) => inScope(a.instance) && (!ctx.severity || a.severity === ctx.severity),
  );
  const eventList = ctx.events.filter((e) => inScope(e.node));
  const sev = { critical: 0, major: 0, warning: 0 };
  for (const a of alertList) {
    if (a.acked) continue;
    if (a.severity in sev) sev[a.severity as keyof typeof sev] += 1;
  }

  const utilByCsp: Record<string, number> = {};
  for (const [id, list] of cspUtil) {
    const avg = mean(list);
    if (avg !== null) utilByCsp[id] = avg;
  }

  return {
    gpus: [
      { key: 'normal', label: '정상', count: gpuBy.normal, tone: 'ok' },
      { key: 'degraded', label: '성능 저하', count: gpuBy.degraded, tone: 'major' },
      { key: 'fault', label: '장애', count: gpuBy.fault, tone: 'crit' },
      { key: 'isolated', label: '격리', count: gpuBy.isolated, tone: 'warn' },
      { key: 'recovering', label: '복구 중', count: gpuBy.recovering, tone: 'info' },
    ],
    nodes: [
      { key: 'ready', label: 'Ready', count: nReady, tone: 'ok' },
      { key: 'degraded', label: 'Degraded', count: nDegraded, tone: 'major' },
      { key: 'cordoned', label: 'Cordoned', count: nCordoned, tone: 'warn' },
      { key: 'draining', label: 'Draining', count: nDraining, tone: 'info' },
      { key: 'offline', label: 'Offline', count: nOffline, tone: 'crit' },
    ],
    alerts: [
      { key: 'critical', label: 'Critical', count: sev.critical, tone: 'crit' },
      { key: 'major', label: 'Major', count: sev.major, tone: 'major' },
      { key: 'warning', label: 'Warning', count: sev.warning, tone: 'warn' },
    ],
    // spec 순서를 유지한다 — Map 순회 순서로 내면 필터를 바꿀 때마다 카드가 자리를 바꾼다.
    csps: ctx.cspSpecs
      .filter((spec) => perCsp.has(spec.id))
      .map((spec) => {
        const v = perCsp.get(spec.id) as { gpuTotal: number; allocated: number; faulted: number };
        return {
          id: spec.id,
          display: spec.display,
          adapter: spec.adapter,
          gpuTotal: v.gpuTotal,
          allocated: v.allocated,
          faulted: v.faulted,
          allocPct: v.gpuTotal > 0 ? (v.allocated / v.gpuTotal) * 100 : 0,
        };
      }),
    utilByCsp,
    models: [...perModel.entries()]
      .map(([model, v]) => ({ model, count: v.count, utilAvgPct: mean(v.utils) ?? 0 }))
      .sort((a, b) => b.count - a.count),
    pools: ctx.pools
      .filter((p) => poolAlloc.has(p.id))
      .map((p) => ({ ...p, allocated: poolAlloc.get(p.id) ?? 0 })),
    tenants: ctx.tenants.filter((t) => tenantIds.has(t.id)),
    regions: ctx.regions.filter((r) => regionIds.has(r.id)),
    alertList,
    eventList,
    idleList: ctx.idle.filter((g) => uuids.has(g.uuid)),
    nodeCount: nodes.size,
    gpuCount: cells.length,
    allocated,
    faulted: gpuBy.fault,
    tempAlerts: gpuBy.degraded,
    agentMissing: nOffline,
    underutilized,
    // 활성 워크로드는 셀이 아니라 서빙 풀에 있다 — 필터에 걸린 풀만 더한다.
    activeWorkloads: ctx.serving
      .filter((s) => poolAlloc.has(s.pool))
      .reduce((n, s) => n + s.replicasReady, 0),
    utilAvg: mean(allocUtils),
  };
}
