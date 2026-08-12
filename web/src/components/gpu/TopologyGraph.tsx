// TopologyGraph — 계층 노드-엣지 그래프. 축(열 정의)을 밖에서 받는다.
//
// 목적: "이 GPU 가 어디에 붙어 있는가"를 목록이 아니라 배선으로 보여준다.
// 축이 파라미터라 같은 컴포넌트가 인프라(GPU→워커→노드풀→클러스터→CSP)와
// 서비스(GPU→워커→서비스→테넌트→워크로드) 양쪽을 모두 그린다 — 액티브
// 모니터링 5개 탭이 전부 이 하나를 재사용한다.
//
// 방향은 왼쪽부터 세부→상위다(PDF 2026-07-30 p3 지시). 개체에서 시작해 소속을
// 거슬러 올라간다.
//
// 워커 노드 열은 nodeCol 이 지정한다(기본=마지막). 그 열에서만 GPU 칸 그리드를
// 그리고 클릭이 onSelectNode 로 나간다. 나머지 열은 드릴 필터로만 쓰인다.
//
// Recharts 를 쓰지 않는 이유는 Sparkline 과 같다 — 노드·엣지 그래프는 차트
// 라이브러리의 축·스케일 모델에 얹을 게 없고, path 몇 줄이면 끝난다.
//
// 성능 계약: 셀이 7,000장이라 전부는 못 그린다. 열마다 GPU 수 상위 topN 개만
// 그리고 나머지는 개수로만 알린다. 집계는 전부 useMemo 안 O(n) 단일 패스다.
//
// 레이아웃 계약: 좌표는 전부 viewBox 안 *비율* 계산이다. 열 개수가 3이든
// 5든 박스폭·간격·글자수·GPU 칸이 같은 비율로 다시 잡히고, 화면 폭은
// preserveAspectRatio 가 처리한다 — 고정 픽셀 배열 없음.
import { useMemo, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import type { CellStatus, FleetCell } from '@/hooks/useFleet';
import { formatCount, formatPct } from '@/lib/gpuMetrics';

/** 한 열의 정의 — 무엇으로 묶고 뭐라고 부를지. */
export interface TopologyAxis {
  /** 열 헤더 라벨 */
  label: string;
  /** 셀 → 이 열의 그룹 키. 빈 문자열이면 그 셀은 이 열에서 제외된다. */
  keyOf: (c: FleetCell) => string;
  /** 키 → 표시명 (미지정 시 키 그대로) */
  displayOf?: (key: string) => string;
}

export type TopologyLayout = 'auto' | 'even' | 'compact';

export interface TopologyGraphProps {
  /** 필터 적용된 벽면 셀 — 부모가 내려준다(내부 fetch 없음). */
  cells: FleetCell[];
  /** 열 정의 2개 이상. 마지막 열 = 워커 노드(instance) 규약. */
  axes: TopologyAxis[];
  /** 셀 상태 분류자 — 미주입이면 전부 정상으로 본다(상태는 라벨이 아니다). */
  statusOf?: (c: FleetCell) => CellStatus;
  /** 선택된 워커 노드 instance. */
  selected: string | null;
  onSelectNode: (instance: string | null) => void;
  /** 열당 표시 개수 (기본 4) */
  topN?: number;
  /** 세로 배치 방식 (기본 auto) */
  layout?: TopologyLayout;
  /** 워커 노드 열의 인덱스 — 그 열에서만 GPU 칸 그리드를 그리고 클릭이
   *  onSelectNode 로 나간다. 기본은 마지막 열(상위→하위 방향)이고, PDF 지시대로
   *  세부→상위(GPU→Worker→Pool→Cluster→CSP)로 뒤집으면 1 이 된다. */
  nodeCol?: number;
}

// ---- 상태 → 색·라벨 (색은 CSS 변수만, hex 금지) ----

const STATUS_COLOR: Record<CellStatus, string> = {
  normal: 'var(--metric-pod)',
  degraded: 'var(--metric-thermal)',
  fault: 'var(--metric-fault)',
  isolated: 'var(--metric-fault)',
  recovering: 'var(--metric-cpu)',
};

const STATUS_LABEL: Record<CellStatus, string> = {
  normal: '정상',
  degraded: '경고',
  fault: '장애',
  isolated: '격리',
  recovering: '복구 중',
};

/** 심각도 순위 — 그룹의 대표 상태는 "가장 나쁜 셀"이다. */
const STATUS_RANK: Record<CellStatus, number> = {
  normal: 0,
  recovering: 1,
  degraded: 2,
  isolated: 3,
  fault: 4,
};
const RANK_STATUS: readonly CellStatus[] = ['normal', 'recovering', 'degraded', 'isolated', 'fault'];

// ---- 레이아웃 (전부 비율 — 고정 좌표 배열 없음) ----

/** viewBox 가로 기준값. 실제 픽셀이 아니라 비율 단위다. */
const VIEW_W = 1000;
const PAD = 12;
/** 박스폭 ÷ 열간격. 열이 몇 개든 이 비율로 폭과 여백이 갈린다. */
const BOX_RATIO = 0.754;
const BOX_H = 64;
/** 워커 노드는 GPU 칸 한 줄이 들어가 더 높다. */
const NODE_BOX_H = 72;
/** 한 행이 차지하는 세로 — 노드 박스 + 숨통. */
const ROW_SLOT = 96;
const AREA_TOP = 34;
/** "+N개 더" 줄 자리. */
const FOOT_H = 30;
/** 워커 박스 GPU 칸 — 목업대로 8칸 한 줄. 박스 좌우 여백(12)은 대칭이다. */
const GRID_MAX = 8;
const GRID_GAP = 3;
const GRID_PAD_X = 12;
/** 칸 시작 y(박스 상단 기준)와 박스 하단에 남길 여백. */
const GRID_TOP = 44;
const GRID_BOT = 6;
/** 칸 세로 ÷ 가로. 목업의 칸은 정사각형에 가깝다. */
const GRID_ASPECT = 0.85;

/** GPU 칸 크기 — 가로는 박스폭에서 나누고, 세로는 박스 안에 반드시 가둔다.
 *  세로 상한을 안 두면 박스가 넓은 열(3열 등)에서 칸이 박스를 뚫고 나간다. */
function gridCell(boxW: number): { w: number; h: number } {
  const w = (boxW - GRID_PAD_X * 2 - GRID_GAP * (GRID_MAX - 1)) / GRID_MAX;
  return { w, h: Math.min(w * GRID_ASPECT, NODE_BOX_H - GRID_TOP - GRID_BOT) };
}
/** 글자 하나가 먹는 가로(비율 단위) — 박스폭에서 표시 글자수를 역산한다. */
const CHAR_W = 9.3;

/** 열 개수 → 박스폭·열 x좌표. 마지막 박스의 오른쪽 끝이 정확히 우측 여백에 닿는다. */
function columnGeometry(n: number): { boxW: number; xs: number[] } {
  const usable = VIEW_W - PAD * 2;
  const stride = usable / (BOX_RATIO + Math.max(n, 1) - 1);
  return {
    boxW: stride * BOX_RATIO,
    xs: Array.from({ length: n }, (_, i) => PAD + stride * i),
  };
}

interface Agg {
  key: string;
  gpus: number;
  nodes: number;
  util: number;
  fault: number;
  warn: number;
  worst: CellStatus;
}

interface GraphNode extends Agg {
  id: string;
  col: number;
  title: string;
  line1: string;
  line2: string;
  x: number;
  y: number;
  h: number;
  /** 마지막 열 전용 — GPU 칸 그리드 재료. */
  cells?: FleetCell[];
}

interface Edge {
  id: string;
  from: string;
  to: string;
  d: string;
}

/** key 별 단일 패스 집계 — nodes 는 instance distinct 라 Set 을 따로 든다. */
function aggregate(
  cells: FleetCell[],
  keyOf: (c: FleetCell) => string,
  statusOf?: (c: FleetCell) => CellStatus,
): Agg[] {
  const map = new Map<
    string,
    {
      gpus: number;
      utilSum: number;
      instances: Set<string>;
      fault: number;
      warn: number;
      worst: number;
    }
  >();
  for (const c of cells) {
    const k = keyOf(c);
    if (!k) continue;
    let a = map.get(k);
    if (!a) {
      a = { gpus: 0, utilSum: 0, instances: new Set(), fault: 0, warn: 0, worst: 0 };
      map.set(k, a);
    }
    a.gpus += 1;
    a.instances.add(c.instance);
    if (Number.isFinite(c.util)) a.utilSum += c.util;
    const st = statusOf?.(c) ?? 'normal';
    if (st === 'fault' || st === 'isolated') a.fault += 1;
    else if (st === 'degraded') a.warn += 1;
    if (STATUS_RANK[st] > a.worst) a.worst = STATUS_RANK[st];
  }
  return [...map.entries()]
    .map(([key, a]) => ({
      key,
      gpus: a.gpus,
      nodes: a.instances.size,
      util: a.gpus > 0 ? a.utilSum / a.gpus : 0,
      fault: a.fault,
      warn: a.warn,
      worst: RANK_STATUS[a.worst],
    }))
    // GPU 수 → 심각도 → 사용률 순. 첫 열이 GPU 개체가 되면서(PDF 방향 역전)
    // 전 항목의 gpus 가 1 이라 1차 기준만으로는 uuid 사전순이 된다 — 아무 GPU 나
    // 4장 뜨는 화면. 심각도·사용률 tie-break 가 있어야 "장애난·바쁜 GPU"가 앞에
    // 온다. 상위 열은 gpus 가 서로 달라 이 tie-break 에 닿지 않는다.
    .sort(
      (x, y) =>
        y.gpus - x.gpus ||
        STATUS_RANK[y.worst] - STATUS_RANK[x.worst] ||
        y.util - x.util ||
        x.key.localeCompare(y.key),
    );
}

/** SVG text 는 넘쳐도 잘리지 않는다 — 폭에서 역산한 글자수로 직접 자른다. */
function trunc(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, Math.max(1, n - 1))}…` : s;
}

function deviceOrder(device: string): number {
  const n = Number.parseInt(device.replace(/^\D+/, ''), 10);
  return Number.isNaN(n) ? 0 : n;
}

/** 세로 배치 — 셋 다 같은 영역을 쓰되 슬롯을 나누는 방식이 다르다.
 *  auto: GPU 수에 비례한 슬롯(큰 그룹이 넓게 자리) / even: 균등 분배 /
 *  compact: 위에서부터 고정 간격으로 붙인다. */
function layoutYs(
  items: Agg[],
  h: number,
  areaTop: number,
  areaBottom: number,
  mode: TopologyLayout,
): number[] {
  const n = items.length;
  if (n === 0) return [];
  if (mode === 'compact') {
    return items.map((_, i) => areaTop + i * ROW_SLOT);
  }
  const span = areaBottom - areaTop;
  if (mode === 'even') {
    const slot = span / n;
    return items.map((_, i) => areaTop + slot * i + (slot - h) / 2);
  }
  // auto — GPU 비중에 비례해 슬롯 크기를 나눈다. 0 합은 균등으로 강등.
  const total = items.reduce((s, a) => s + a.gpus, 0);
  if (total <= 0) return layoutYs(items, h, areaTop, areaBottom, 'even');
  // 슬롯 하한을 박스 높이로 묶으면(겹침 방지) 비중이 한쪽에 쏠릴 때 합이 영역을
  // 넘는다. 그때는 균등으로 강등한다 — ROW_SLOT 이 박스 높이보다 크므로 even 은
  // 항상 영역 안에 들어간다. (구 구현은 넘친 만큼 통째로 당겨 영역 밖으로 나갔다.)
  const slots = items.map((a) => Math.max((a.gpus / total) * span, h));
  if (slots.reduce((s, v) => s + v, 0) > span) {
    return layoutYs(items, h, areaTop, areaBottom, 'even');
  }
  const ys: number[] = [];
  let cursor = areaTop;
  for (const slot of slots) {
    ys.push(cursor + (slot - h) / 2);
    cursor += slot;
  }
  return ys;
}

export default function TopologyGraph({
  cells,
  axes,
  statusOf,
  selected,
  onSelectNode,
  topN = 4,
  layout = 'auto',
  nodeCol,
}: TopologyGraphProps) {
  const lastCol = axes.length - 1;
  // 워커 노드 열 — GPU 칸 그리드와 선택 콜백이 붙는 자리. 드릴 방향(왼→오른쪽)과
  // 무관하게 이 열 하나만 "노드"다.
  const nodeIdx = nodeCol ?? lastCol;

  // 드릴 필터 — 상위 열을 고르면 하위 열이 그 선택으로 좁혀진다.
  // 열이 몇 개든 같은 규칙이라 배열 하나로 든다(마지막 열은 selected prop).
  const [drill, setDrill] = useState<(string | null)[]>(() => axes.map(() => null));

  // 열별 유효 범위 — 위에서 고른 값이 아래 열을 자른다. scopes[i] = i번째 열이 볼 셀.
  const scopes = useMemo(() => {
    const out: FleetCell[][] = [cells];
    for (let i = 0; i < lastCol; i += 1) {
      const pick = drill[i];
      const prev = out[i];
      out.push(pick ? prev.filter((c) => axes[i].keyOf(c) === pick) : prev);
    }
    return out;
  }, [cells, drill, axes, lastCol]);

  const { boxW, xs } = useMemo(() => columnGeometry(axes.length), [axes.length]);
  const maxChars = Math.max(6, Math.floor((boxW - GRID_PAD_X * 2) / CHAR_W));
  // GPU 칸도 박스폭 비례 — 열이 늘면 칸도 같이 좁아지되 박스 밖으로는 못 나간다.
  const { w: cellW, h: cellH } = gridCell(boxW);

  const viewH = AREA_TOP + topN * ROW_SLOT + FOOT_H;
  const areaBottom = AREA_TOP + topN * ROW_SLOT;

  const graph = useMemo(() => {
    const aggs = axes.map((ax, i) => aggregate(scopes[i], ax.keyOf, statusOf));
    const more = aggs.map((a) => Math.max(0, a.length - topN));

    // 상위 topN 워커 노드의 GPU 칸을 그리려면 그 셀만 있으면 된다.
    const topKeys = new Set(aggs[nodeIdx].slice(0, topN).map((a) => a.key));
    const lastKeyOf = axes[nodeIdx].keyOf;
    const byKey = new Map<string, FleetCell[]>();
    for (const c of scopes[nodeIdx]) {
      const k = lastKeyOf(c);
      if (!topKeys.has(k)) continue;
      const list = byKey.get(k);
      if (list) list.push(c);
      else byKey.set(k, [c]);
    }
    for (const list of byKey.values()) {
      list.sort((a, b) => deviceOrder(a.device) - deviceOrder(b.device));
    }

    const cols: GraphNode[][] = aggs.map((list, col) => {
      const shown = list.slice(0, topN);
      const isNode = col === nodeIdx;
      const h = isNode ? NODE_BOX_H : BOX_H;
      const ys = layoutYs(shown, h, AREA_TOP, areaBottom, layout);
      const display = axes[col].displayOf;
      return shown.map((a, i) => ({
        ...a,
        id: `${col}:${a.key}`,
        col,
        title: display ? display(a.key) : a.key.replace(/:\d+$/, ''),
        line1: isNode
          ? `GPU ${formatCount(a.gpus)}장 · ${STATUS_LABEL[a.worst]}`
          : a.nodes > 1
            ? `노드 ${formatCount(a.nodes)}대 · GPU ${formatCount(a.gpus)}장`
            : `GPU ${formatCount(a.gpus)}장`,
        line2: isNode ? '' : `평균 사용률 ${formatPct(a.util, 1)}`,
        x: xs[col],
        y: ys[i],
        h,
        cells: isNode ? byKey.get(a.key) : undefined,
      }));
    });

    const byId = new Map<string, GraphNode>();
    for (const list of cols) for (const n of list) byId.set(n.id, n);

    // 엣지는 실제 데이터에 그 쌍이 있을 때만 긋는다 — 가짜 연결 금지.
    const edges: Edge[] = [];
    const seen = new Set<string>();
    for (let col = 0; col < lastCol; col += 1) {
      const from = axes[col].keyOf;
      const to = axes[col + 1].keyOf;
      for (const c of scopes[col + 1]) {
        const a = byId.get(`${col}:${from(c)}`);
        const b = byId.get(`${col + 1}:${to(c)}`);
        if (!a || !b) continue;
        const id = `${a.id}>${b.id}`;
        if (seen.has(id)) continue;
        seen.add(id);
        const x1 = a.x + boxW;
        const y1 = a.y + a.h / 2;
        const x2 = b.x;
        const y2 = b.y + b.h / 2;
        const dx = (x2 - x1) * 0.5;
        edges.push({
          id,
          from: a.id,
          to: b.id,
          d: `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`,
        });
      }
    }

    return { cols, edges, more };
  }, [axes, scopes, statusOf, topN, lastCol, nodeIdx, xs, boxW, areaBottom, layout]);

  // 선택 집합 — 강조 대상과 그 배선만 진하게 남긴다.
  const activeIds = useMemo(() => {
    const s = new Set<string>();
    drill.forEach((v, i) => {
      if (v && i !== nodeIdx) s.add(`${i}:${v}`);
    });
    if (selected) s.add(`${nodeIdx}:${selected}`);
    return s;
  }, [drill, selected, nodeIdx]);
  const anyActive = activeIds.size > 0;

  if (cells.length === 0) {
    return <p className="py-10 text-center text-muted-foreground text-sm">표시할 대상이 없다.</p>;
  }

  /** 상위 열을 고르면 그 아래 드릴은 전부 풀린다 — 없어진 범위를 붙들면 빈 화면이 된다. */
  const activate = (node: GraphNode) => {
    if (node.col === nodeIdx) {
      onSelectNode(node.key === selected ? null : node.key);
      return;
    }
    setDrill((prev) =>
      prev.map((v, i) => (i < node.col ? v : i === node.col ? (v === node.key ? null : node.key) : null)),
    );
  };

  const onKeyDown = (e: ReactKeyboardEvent<SVGGElement>, node: GraphNode) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    e.preventDefault();
    activate(node);
  };

  const clearAll = () => {
    setDrill(axes.map(() => null));
    onSelectNode(null);
  };
  const drilled = drill.some(Boolean) || selected !== null;

  return (
    <div className="flex flex-col gap-2">
      {/* 현재 드릴 범위 — 어디까지 좁혀졌는지 한 줄로. */}
      <div className="flex min-h-6 flex-wrap items-center gap-1.5">
        {axes.map((ax, i) => {
          const v = i === nodeIdx ? selected : drill[i];
          if (!v) return null;
          return (
            <Badge key={ax.label} variant="secondary" className="text-[10px]">
              {ax.label} {ax.displayOf ? ax.displayOf(v) : v}
            </Badge>
          );
        })}
        {drilled ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="ml-auto h-6 px-2 text-xs"
            onClick={clearAll}
          >
            선택 해제
          </Button>
        ) : null}
      </div>

      {/* 가독 하한 — SVG 글자는 viewBox 단위라 컨테이너가 좁아지면 같이 줄어든다.
          폭 820 미만이면 제목이 10.7px 밑으로 떨어져 읽을 수 없으므로, 거기서부터는
          축소 대신 가로 스크롤로 넘긴다. 레이아웃 자체는 그대로 비율이다. */}
      <div className="overflow-x-auto">
        <svg
          viewBox={`0 0 ${VIEW_W} ${viewH}`}
          preserveAspectRatio="xMidYMid meet"
          className="w-full min-w-[820px]"
          role="img"
          aria-label={`${axes.map((a) => a.label).join(' · ')} ${axes.length}계층 토폴로지`}
        >
          {axes.map((ax, i) => (
            <text
              key={ax.label}
              x={xs[i]}
              y={18}
              fontSize="11"
              fontWeight="600"
              fill="var(--muted-foreground)"
            >
              {ax.label}
            </text>
          ))}

          {/* 엣지 먼저 — 박스가 위에 올라와야 글자가 안 가린다. */}
          <g fill="none" strokeLinecap="round">
            {graph.edges.map((e) => {
              const on = activeIds.has(e.from) || activeIds.has(e.to);
              return (
                <path
                  key={e.id}
                  d={e.d}
                  stroke={on ? 'var(--primary)' : 'var(--muted-foreground)'}
                  strokeWidth={on ? 1.8 : 1}
                  opacity={anyActive ? (on ? 0.9 : 0.08) : 0.32}
                />
              );
            })}
          </g>

          {graph.cols.map((list, col) => (
            <g key={axes[col].label}>
              {list.map((n) => {
                const isSel = activeIds.has(n.id);
                const hasFault = n.fault > 0;
                return (
                  // biome-ignore lint/a11y/useSemanticElements: SVG 안에는 button 을 넣을 수 없다
                  <g
                    key={n.id}
                    role="button"
                    tabIndex={0}
                    className="cursor-pointer"
                    aria-label={`${axes[col].label} ${n.title}, GPU ${n.gpus}장, 상태 ${STATUS_LABEL[n.worst]}`}
                    aria-pressed={isSel}
                    onClick={() => activate(n)}
                    onKeyDown={(e) => onKeyDown(e, n)}
                  >
                    <title>{`${n.title} · GPU ${formatCount(n.gpus)}장 · 사용률 ${formatPct(n.util, 1)}`}</title>
                    <rect
                      x={n.x}
                      y={n.y}
                      width={boxW}
                      height={n.h}
                      rx="8"
                      fill="var(--card)"
                      stroke={
                        isSel ? 'var(--primary)' : hasFault ? 'var(--metric-fault)' : 'var(--border)'
                      }
                      strokeWidth={isSel ? 2 : hasFault ? 1.6 : 1}
                    />
                    {hasFault ? (
                      <text
                        x={n.x + boxW - 10}
                        y={n.y + 19}
                        fontSize="12"
                        textAnchor="end"
                        fill="var(--metric-fault)"
                      >
                        ⚠
                      </text>
                    ) : null}
                    <text
                      x={n.x + 12}
                      y={n.y + 20}
                      fontSize="13"
                      fontWeight="600"
                      fill="var(--foreground)"
                    >
                      {trunc(n.title, hasFault ? maxChars - 3 : maxChars)}
                    </text>
                    <text x={n.x + 12} y={n.y + 36} fontSize="10.5" fill="var(--muted-foreground)">
                      {trunc(n.line1, maxChars + 4)}
                    </text>
                    {n.line2 ? (
                      <text x={n.x + 12} y={n.y + 51} fontSize="10.5" fill="var(--muted-foreground)">
                        {n.line2}
                      </text>
                    ) : null}
                    {/* 워커 노드 GPU 칸 — device 순으로 한 줄에 상태색을 칠한다. */}
                    {n.cells?.slice(0, GRID_MAX).map((c, i) => (
                      <rect
                        key={c.uuid}
                        x={n.x + GRID_PAD_X + i * (cellW + GRID_GAP)}
                        y={n.y + GRID_TOP}
                        width={cellW}
                        height={cellH}
                        rx="2"
                        fill={statusOf ? STATUS_COLOR[statusOf(c)] : `var(--util-band-${c.band})`}
                      />
                    ))}
                  </g>
                );
              })}
              {graph.more[col] > 0 ? (
                <text
                  x={xs[col] + boxW / 2}
                  y={viewH - 12}
                  fontSize="10"
                  textAnchor="middle"
                  fill="var(--muted-foreground)"
                >
                  +{formatCount(graph.more[col])}개 더
                </text>
              ) : null}
            </g>
          ))}
        </svg>
      </div>
    </div>
  );
}
