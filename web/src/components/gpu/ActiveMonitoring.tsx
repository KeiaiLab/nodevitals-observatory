// ActiveMonitoring — 실시간 액티브 모니터링. 같은 플릿을 5개 관점으로 가른다
// (목업 demo1 의 좌측 탭): 인프라 계층을 따라가는 토폴로지, 서비스(서빙 풀)
// 관점, 그리고 클러스터·CSP·존/리전 롤업.
//
// 계층 축이 두 개라는 게 핵심이다 — 인프라는 CSP → 클러스터 → 노드풀 → 노드로
// 내려가고, 서비스는 테넌트 → 서빙 풀 → 노드로 내려간다. 같은 GPU 가 두 축
// 모두에 속하므로, 탭을 바꾸면 "같은 장비를 다른 소속으로" 보게 된다.
//
// 데이터는 전부 벽면 셀(useFleet)에서 집계한다 — 새 API 없음.
import { Maximize2, Shrink, ZoomIn, ZoomOut } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import NodeGpuPanel from '@/components/gpu/NodeGpuPanel';
import TopologyGraph, {
  type TopologyAxis,
  type TopologyLayout,
} from '@/components/gpu/TopologyGraph';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import type { CellStatus, FleetCell } from '@/hooks/useFleet';
import type {
  AlertEvent,
  ClusterEvent,
  CSPSummary,
  PoolSummary,
  StatusCount,
} from '@/lib/demoApi';
import { bandForUtil, formatCount, formatPct } from '@/lib/gpuMetrics';

export interface ActiveMonitoringProps {
  cells: FleetCell[] | null;
  csps: CSPSummary[];
  pools: PoolSummary[];
  /** 셀 상태 분류자 — 장애·격리 집계용(호출부가 시나리오를 안다). */
  statusOf?: (cell: FleetCell) => CellStatus;
  /** 노드 선택 → 드릴 시트(자산 대장) 열기. */
  onSelectNode?: (instance: string) => void;
  /** 상단 상태 요약 칩 — 서버가 한 번 분류한 dashboard.gpus 를 그대로 쓴다. */
  gpuStatus?: StatusCount[];
  /** 노드 패널 이벤트 탭 소재. */
  alerts?: AlertEvent[];
  events?: ClusterEvent[];
}

/** 상태 칩 색 — StatusCount.tone 이 색을 정한다(라벨 문자열로 색 추론 금지). */
const TONE_DOT: Record<StatusCount['tone'], string> = {
  ok: 'var(--metric-pod)',
  info: 'var(--metric-cpu)',
  warn: 'var(--metric-thermal)',
  major: 'var(--metric-thermal)',
  crit: 'var(--metric-fault)',
  muted: 'var(--muted-foreground)',
};

interface Agg {
  gpus: number;
  nodes: number;
  utilSum: number;
  fault: number;
  degraded: number;
}

const EMPTY: Agg = { gpus: 0, nodes: 0, utilSum: 0, fault: 0, degraded: 0 };

/** key 별 집계 — nodes 는 instance distinct 라 Set 을 따로 든다. */
function aggregate(
  cells: FleetCell[],
  keyOf: (c: FleetCell) => string,
  statusOf?: (c: FleetCell) => CellStatus,
): Map<string, Agg & { instances: Set<string> }> {
  const out = new Map<string, Agg & { instances: Set<string> }>();
  for (const c of cells) {
    const k = keyOf(c);
    if (!k) continue;
    let a = out.get(k);
    if (!a) {
      a = { ...EMPTY, instances: new Set() };
      out.set(k, a);
    }
    a.gpus += 1;
    a.instances.add(c.instance);
    if (Number.isFinite(c.util)) a.utilSum += c.util;
    const st = statusOf?.(c);
    if (st === 'fault' || st === 'isolated') a.fault += 1;
    else if (st === 'degraded') a.degraded += 1;
  }
  for (const a of out.values()) a.nodes = a.instances.size;
  return out;
}

/** 롤업 한 줄 — 이름 / GPU·노드 수 / 평균 사용률 막대 / 장애 배지. */
function RollupRow({
  label,
  sub,
  agg,
  onClick,
}: {
  label: string;
  sub?: string;
  agg: Agg;
  onClick?: () => void;
}) {
  const util = agg.gpus > 0 ? agg.utilSum / agg.gpus : 0;
  const band = bandForUtil(util);
  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        disabled={!onClick}
        className={`flex w-full items-center gap-3 rounded-md border bg-card p-2.5 text-left ${
          onClick ? 'hover:bg-accent' : ''
        }`}
      >
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium text-sm" title={label}>
            {label}
          </p>
          {sub ? (
            <p className="truncate text-[11px] text-muted-foreground" title={sub}>
              {sub}
            </p>
          ) : null}
        </div>
        <div className="shrink-0 text-right">
          <p className="font-semibold text-sm tabular-nums">{formatCount(agg.gpus)}장</p>
          <p className="text-[11px] text-muted-foreground tabular-nums">
            노드 {formatCount(agg.nodes)}
          </p>
        </div>
        {/* 평균 사용률 — 대역 색으로 한눈에. */}
        <div className="hidden w-28 shrink-0 sm:block">
          <div className="mb-1 flex justify-between text-[11px] tabular-nums">
            <span className="text-muted-foreground">사용률</span>
            <span className="font-medium">{formatPct(util, 0)}</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full"
              style={{
                width: `${Math.max(util, 1)}%`,
                backgroundColor: `var(--util-band-${band.index})`,
              }}
            />
          </div>
        </div>
        <div className="flex shrink-0 gap-1">
          {agg.fault > 0 ? (
            <Badge variant="destructive" className="text-[10px]">
              장애 {agg.fault}
            </Badge>
          ) : null}
          {agg.degraded > 0 ? (
            <Badge variant="outline" className="text-[10px] text-metric-thermal">
              경고 {agg.degraded}
            </Badge>
          ) : null}
        </div>
      </button>
    </li>
  );
}

/** 인프라 토폴로지 — CSP → 클러스터 → 노드풀 → 노드. 계층을 펼쳐 내려간다. */
function TopologyView({
  cells,
  statusOf,
  onSelectNode,
}: {
  cells: FleetCell[];
  statusOf?: (c: FleetCell) => CellStatus;
  onSelectNode?: (i: string) => void;
}) {
  const [csp, setCsp] = useState<string | null>(null);
  const [cluster, setCluster] = useState<string | null>(null);
  const [nodePool, setNodePool] = useState<string | null>(null);

  const level = nodePool ? 'node' : cluster ? 'pool' : csp ? 'cluster' : 'csp';
  const scoped = useMemo(
    () =>
      cells.filter(
        (c) =>
          (!csp || c.csp === csp) &&
          (!cluster || c.cluster === cluster) &&
          (!nodePool || c.nodePool === nodePool),
      ),
    [cells, csp, cluster, nodePool],
  );

  const keyOf =
    level === 'csp'
      ? (c: FleetCell) => c.csp
      : level === 'cluster'
        ? (c: FleetCell) => c.cluster
        : level === 'pool'
          ? (c: FleetCell) => c.nodePool
          : (c: FleetCell) => c.instance;
  const groups = useMemo(() => aggregate(scoped, keyOf, statusOf), [scoped, keyOf, statusOf]);
  const rows = [...groups.entries()].sort((a, b) => b[1].gpus - a[1].gpus);

  const LEVEL_LABEL: Record<string, string> = {
    csp: 'CSP',
    cluster: '클러스터',
    pool: '노드 풀',
    node: 'GPU 워커 노드',
  };

  return (
    <div className="flex flex-col gap-2">
      {/* 브레드크럼 — 계층을 되짚어 올라간다. */}
      <div className="flex flex-wrap items-center gap-1 text-xs">
        <button
          type="button"
          className="text-primary underline-offset-4 hover:underline"
          onClick={() => {
            setCsp(null);
            setCluster(null);
            setNodePool(null);
          }}
        >
          전체 CSP
        </button>
        {csp ? (
          <>
            <span className="text-muted-foreground">›</span>
            <button
              type="button"
              className="text-primary underline-offset-4 hover:underline"
              onClick={() => {
                setCluster(null);
                setNodePool(null);
              }}
            >
              {csp}
            </button>
          </>
        ) : null}
        {cluster ? (
          <>
            <span className="text-muted-foreground">›</span>
            <button
              type="button"
              className="text-primary underline-offset-4 hover:underline"
              onClick={() => setNodePool(null)}
            >
              {cluster}
            </button>
          </>
        ) : null}
        {nodePool ? (
          <>
            <span className="text-muted-foreground">›</span>
            <span className="font-medium">{nodePool}</span>
          </>
        ) : null}
        <Badge variant="secondary" className="ml-auto text-[10px]">
          {LEVEL_LABEL[level]} {rows.length}개 · GPU {formatCount(scoped.length)}장
        </Badge>
      </div>

      {rows.length === 0 ? (
        <p className="py-8 text-center text-muted-foreground text-sm">표시할 대상이 없다.</p>
      ) : (
        <ul className="flex max-h-[26rem] flex-col gap-1.5 overflow-auto pr-1">
          {rows.slice(0, 60).map(([key, agg]) => (
            <RollupRow
              key={key}
              label={key}
              sub={
                level === 'node'
                  ? `${agg.gpus}장 · ${nodePool ?? ''}`
                  : level === 'pool'
                    ? 'K8s 노드 풀'
                    : undefined
              }
              agg={agg}
              onClick={
                level === 'csp'
                  ? () => setCsp(key)
                  : level === 'cluster'
                    ? () => setCluster(key)
                    : level === 'pool'
                      ? () => setNodePool(key)
                      : onSelectNode
                        ? () => onSelectNode(key)
                        : undefined
              }
            />
          ))}
        </ul>
      )}
      {rows.length > 60 ? (
        <p className="text-[11px] text-muted-foreground">
          상위 60개만 표시 — 좁히려면 상단 필터를 쓴다.
        </p>
      ) : null}
    </div>
  );
}

/** 단일 축 롤업 뷰(서비스 앱 / 클러스터 / CSP / 존·리전 공용). */
function RollupView({
  cells,
  keyOf,
  labelOf,
  statusOf,
  empty,
}: {
  cells: FleetCell[];
  keyOf: (c: FleetCell) => string;
  labelOf?: (key: string) => { label: string; sub?: string };
  statusOf?: (c: FleetCell) => CellStatus;
  empty: string;
}) {
  const groups = useMemo(() => aggregate(cells, keyOf, statusOf), [cells, keyOf, statusOf]);
  const rows = [...groups.entries()].sort((a, b) => b[1].gpus - a[1].gpus);
  if (rows.length === 0) {
    return <p className="py-8 text-center text-muted-foreground text-sm">{empty}</p>;
  }
  return (
    <ul className="flex max-h-[26rem] flex-col gap-1.5 overflow-auto pr-1">
      {rows.map(([key, agg]) => {
        const l = labelOf?.(key) ?? { label: key };
        return <RollupRow key={key} label={l.label} sub={l.sub} agg={agg} />;
      })}
    </ul>
  );
}

/** 탭 = 관점. 인프라 축 4개 + 서비스 축 1개, 순서는 목업 그대로. */
type TabId = 'topology' | 'service' | 'cluster' | 'csp' | 'region';

const TAB_ORDER: readonly TabId[] = ['topology', 'service', 'cluster', 'csp', 'region'];

const TAB_LABEL: Record<TabId, string> = {
  topology: '토폴로지',
  service: '서비스 앱',
  cluster: '클러스터',
  csp: 'CSP',
  region: '존·리전',
};

const LAYOUT_LABEL: Record<TopologyLayout, string> = {
  auto: '자동',
  even: '균등',
  compact: '밀집',
};

const TOP_N_MIN = 4;
const TOP_N_MAX = 8;

const WORKLOAD_LABEL: Record<string, string> = { training: '학습', inference: '추론' };

/** 서비스 축에서 테넌트·풀이 비어 있는 GPU 의 대체 키 — 실제 이름과 겹치지 않게. */
const UNASSIGNED = ' unassigned';
const unassignedLabel = (k: string) => (k === UNASSIGNED ? '미할당' : k);

export default function ActiveMonitoring({
  cells,
  csps,
  pools,
  statusOf,
  onSelectNode,
  gpuStatus,
  alerts,
  events,
}: ActiveMonitoringProps) {
  const cspDisplay = useMemo(() => {
    const m: Record<string, string> = {};
    for (const c of csps) m[c.id] = c.display;
    return m;
  }, [csps]);
  const poolMeta = useMemo(() => {
    const m: Record<string, PoolSummary> = {};
    for (const p of pools) m[p.id] = p;
    return m;
  }, [pools]);

  // 토폴로지에서 고른 워커 — 우측 실시간 패널의 대상. 드릴 시트(자산 대장)와는
  // 별개다: 패널은 항상 붙어 있고, 시트는 [상세 보기]로 연다.
  const [picked, setPicked] = useState<string | null>(null);
  const [q, setQ] = useState('');
  const [layout, setLayout] = useState<TopologyLayout>('auto');
  const [topN, setTopN] = useState(TOP_N_MIN);
  const rootRef = useRef<HTMLDivElement>(null);

  const toggleFullscreen = () => {
    const el = rootRef.current;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen();
    else void el.requestFullscreen?.();
  };

  // 축 정의 — 마지막 열은 어느 탭이든 워커 노드(instance)다. 그래야 GPU 칸
  // 그리드와 우측 실시간 패널이 탭과 무관하게 같은 계약으로 붙는다.
  const nodeAxis = useMemo<TopologyAxis>(
    () => ({ label: '워커 노드', keyOf: (c) => c.instance }),
    [],
  );
  // GPU 열 라벨 — uuid 는 화면에 쓸 수 없으니 device(GPU-3)로 바꿔 보여준다.
  const uuidToDevice = useMemo(() => {
    const m: Record<string, string> = {};
    for (const c of cells ?? []) m[c.uuid] = c.device;
    return m;
  }, [cells]);
  const axesFor = useMemo(() => {
    const csp: TopologyAxis = {
      label: 'CSP',
      keyOf: (c) => c.csp,
      displayOf: (k) => cspDisplay[k] ?? k,
    };
    const cluster: TopologyAxis = { label: '클러스터', keyOf: (c) => c.cluster };
    const pool: TopologyAxis = { label: '노드 풀', keyOf: (c) => c.nodePool };
    const region: TopologyAxis = { label: '리전', keyOf: (c) => c.region };
    // GPU 열 — 개체가 첫 열이다. 라벨은 device(GPU-3)만 쓰고 노드명은 노드 열이
    // 바로 옆에서 말해 준다.
    const gpu: TopologyAxis = {
      label: 'GPU',
      keyOf: (c) => c.uuid,
      displayOf: (k) => uuidToDevice[k] ?? k.slice(0, 8),
    };
    const table: Record<TabId, TopologyAxis[]> = {
      // 인프라 축 — PDF(2026-07-30 p3) 지시대로 왼쪽부터 세부→상위다:
      // GPU → Worker Node → Node Pool → Cluster → CSP.
      topology: [gpu, nodeAxis, pool, cluster, csp],
      // 서비스 축 — 같은 장비를 "무엇을 돌리는가"로 다시 묶는다. 방향은 인프라
      // 축과 같이 세부→상위. 미할당 GPU 도 UNASSIGNED 로 끝까지 이어 붙인다 —
      // 중간에서 키가 비면 그 열에서 탈락해 옆 열이 배선 없는 고립 노드로
      // 남는다(실측 482장).
      service: [
        gpu,
        nodeAxis,
        {
          label: '서비스',
          keyOf: (c) => c.pool || UNASSIGNED,
          displayOf: (k) => (k === UNASSIGNED ? '미할당' : (poolMeta[k]?.display ?? k)),
        },
        { label: '테넌트', keyOf: (c) => c.tenant || UNASSIGNED, displayOf: unassignedLabel },
        {
          label: '워크로드',
          keyOf: (c) => c.workload || UNASSIGNED,
          displayOf: (k) => WORKLOAD_LABEL[k] ?? '미할당',
        },
      ],
      cluster: [gpu, nodeAxis, pool, cluster],
      csp: [gpu, nodeAxis, cluster, region, csp],
      // 리전은 이름에 CSP 가 이미 들어 있어(kakao-kr1) 리전↔CSP 는 1:1 평행선이
      // 된다 — 갈래가 생기는 노드풀·클러스터를 사이에 둔다.
      region: [gpu, nodeAxis, pool, cluster, region],
    };
    return (id: TabId) => table[id];
  }, [cspDisplay, poolMeta, nodeAxis, uuidToDevice]);

  /** 드릴다운 목록의 라벨 — 그래프 첫 열과 같은 표시명을 쓴다. */
  const rollupLabelOf = useMemo(
    () =>
      (id: TabId): ((k: string) => { label: string; sub?: string }) | undefined => {
        if (id === 'csp') return (k) => ({ label: cspDisplay[k] ?? k });
        if (id === 'service')
          return (k) => ({ label: WORKLOAD_LABEL[k] ?? '미할당' });
        return undefined;
      },
    [cspDisplay],
  );

  // 검색은 그래프에 먹이기 전에 셀을 좁힌다 — 이름 4축(노드/클러스터/노드풀/CSP)
  // 부분일치. 빈 문자열이면 원본 그대로(불필요한 배열 복사 회피).
  const searched = useMemo(() => {
    const base = cells ?? [];
    const t = q.trim().toLowerCase();
    if (!t) return base;
    return base.filter(
      (c) =>
        c.instance.toLowerCase().includes(t) ||
        c.cluster.toLowerCase().includes(t) ||
        c.nodePool.toLowerCase().includes(t) ||
        c.csp.toLowerCase().includes(t),
    );
  }, [cells, q]);

  return (
    // 전체화면 대상 = 카드 통째. 전체화면에서도 배경이 비치지 않도록 bg 를 준다.
    <Card ref={rootRef} className="bg-card">
      <CardHeader className="pb-2">
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-base">실시간 액티브 모니터링</CardTitle>
          <Badge variant="outline" className="gap-1 text-[10px]">
            <span className="size-1.5 rounded-full bg-metric-pod" />
            Active Monitoring
          </Badge>
          <div className="ml-auto w-full sm:w-56">
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="GPU, 노드, 클러스터 검색…"
              className="h-8 text-xs"
              aria-label="토폴로지 검색"
            />
          </div>
        </div>

        {/* 상태 요약 칩 — 서버가 한 번 분류한 값(dashboard.gpus)이라 화면끼리 숫자가 어긋나지 않는다. */}
        {gpuStatus && gpuStatus.length > 0 ? (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pt-1">
            {gpuStatus.map((s) => (
              <span key={s.key} className="flex items-center gap-1.5 text-xs">
                <span
                  className="size-2 shrink-0 rounded-full"
                  style={{ backgroundColor: TONE_DOT[s.tone] }}
                />
                <span className="text-muted-foreground">{s.label}</span>
                <span className="font-semibold tabular-nums">{formatCount(s.count)}</span>
              </span>
            ))}
          </div>
        ) : null}

        <CardDescription className="text-xs">
          왼쪽부터 GPU → 워커 노드 → 노드 풀 → 클러스터 → CSP. 개체에서 시작해 소속을
          거슬러 올라간다. 탭을 바꾸면 같은 장비가 다른 축(서비스·클러스터·CSP·리전)으로
          다시 묶인다.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {cells === null ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-8 w-72" />
            <Skeleton className="h-64 w-full" />
          </div>
        ) : (
          <Tabs defaultValue="topology">
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <TabsList>
                {TAB_ORDER.map((id) => (
                  <TabsTrigger key={id} value={id} className="text-xs">
                    {TAB_LABEL[id]}
                  </TabsTrigger>
                ))}
              </TabsList>

              {/* 그래프 툴바 — 목업의 [레이아웃 ▾][정렬][확대][전체화면]. */}
              <div className="ml-auto flex shrink-0 items-center gap-1">
                <Select
                  value={layout}
                  onValueChange={(v) => setLayout(v as TopologyLayout)}
                >
                  <SelectTrigger size="sm" aria-label="그래프 레이아웃">
                    <span className="whitespace-nowrap text-xs">
                      레이아웃: {LAYOUT_LABEL[layout]}
                    </span>
                  </SelectTrigger>
                  <SelectContent>
                    {(Object.keys(LAYOUT_LABEL) as TopologyLayout[]).map((k) => (
                      <SelectItem key={k} value={k}>
                        {LAYOUT_LABEL[k]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="size-8"
                  title="선택 해제 후 전체 보기"
                  aria-label="전체 보기로 맞춤"
                  onClick={() => {
                    setPicked(null);
                    setQ('');
                  }}
                >
                  <Shrink className="size-3.5" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="size-8"
                  title={`열마다 상위 ${topN}개 표시 — 눌러서 ${topN === TOP_N_MIN ? TOP_N_MAX : TOP_N_MIN}개로`}
                  aria-label="표시 개수 전환"
                  onClick={() => setTopN((n) => (n === TOP_N_MIN ? TOP_N_MAX : TOP_N_MIN))}
                >
                  {topN === TOP_N_MIN ? (
                    <ZoomIn className="size-3.5" />
                  ) : (
                    <ZoomOut className="size-3.5" />
                  )}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="size-8"
                  title="전체화면"
                  aria-label="전체화면"
                  onClick={toggleFullscreen}
                >
                  <Maximize2 className="size-3.5" />
                </Button>
              </div>
            </div>

            {/* 5개 탭이 같은 그래프를 축만 바꿔 재사용한다 — 같은 장비가 탭마다
                다른 소속으로 보인다. 마지막 열은 어느 축이든 워커 노드다. */}
            {TAB_ORDER.map((id) => (
              <TabsContent key={id} value={id}>
                {/* 2열 전환은 2xl(1536px)부터 — xl(1280px)에서 나누면 우측 패널
                    20rem 을 떼고 남는 그래프 폭이 628px 로, 1열일 때(960px)보다
                    오히려 좁아져 글자가 8.2px 로 뭉갠다. 넓어질 때만 나눈다. */}
                <div className="grid gap-3 2xl:grid-cols-[minmax(0,1fr)_20rem]">
                  <TopologyGraph
                    cells={searched}
                    axes={axesFor(id)}
                    statusOf={statusOf}
                    selected={picked}
                    onSelectNode={setPicked}
                    topN={topN}
                    layout={layout}
                    nodeCol={1}
                  />
                  <NodeGpuPanel
                    instance={picked}
                    cells={searched}
                    statusOf={statusOf}
                    alerts={alerts}
                    events={events}
                    onOpenDetail={onSelectNode}
                  />
                </div>

                {/* 그래프는 열마다 상위 몇 개만 그린다 — 전수 탐색은 이 목록이 담당한다. */}
                <details className="mt-3">
                  <summary className="cursor-pointer text-muted-foreground text-xs hover:text-foreground">
                    전체 목록으로 보기 ({axesFor(id).map((a) => a.label).join(' → ')})
                  </summary>
                  <div className="mt-2">
                    {id === 'topology' ? (
                      <TopologyView
                        cells={searched}
                        statusOf={statusOf}
                        onSelectNode={onSelectNode}
                      />
                    ) : (
                      <RollupView
                        cells={searched}
                        keyOf={axesFor(id)[0].keyOf}
                        labelOf={rollupLabelOf(id)}
                        statusOf={statusOf}
                        empty="표시할 대상이 없다."
                      />
                    )}
                  </div>
                </details>
              </TabsContent>
            ))}
          </Tabs>
        )}
      </CardContent>
    </Card>
  );
}
