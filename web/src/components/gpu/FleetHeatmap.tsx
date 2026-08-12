// FleetHeatmap — 7,000셀 플릿 벽면. 성능 계약(위반 시 시연 프레임 드랍):
//  · 셀 = 10px div + data-i(청크 내 인덱스)뿐 — 개별 컴포넌트/핸들러/포털 금지.
//  · 클러스터 청크 = React.memo, (chunk.signature + overlaySig) 동일 시 재렌더
//    skip — 30s 폴링마다 배열 참조가 갈려도 대역/오버레이 불변이면 DOM 불변.
//  · hover/click = 청크 컨테이너 1곳 이벤트 위임(closest('[data-i]')). 위임
//    핸들러는 identity 영구 고정 + 데이터 조회는 ref 경유 — memo-skip 된 구
//    클로저에서도 항상 최신 셀 값을 읽는다.
//  · 툴팁 = 루트 absolute div 1개를 ref 직접 DOM 갱신(textContent 만 — 마크업
//    주입 0, CSP default-src 'self' 방침). 리렌더 0.
import { memo, useCallback, useMemo, useRef } from 'react';
import type { MouseEvent as ReactMouseEvent } from 'react';
import { UTIL_BAND_CLASSES } from '@/components/gpu/UtilLegend';
import { Badge } from '@/components/ui/badge';
import type { ClusterChunk, FleetCell } from '@/hooks/useFleet';
import { formatCount, formatPct } from '@/lib/gpuMetrics';

export interface FleetHeatmapProps {
  chunks: ClusterChunk[];
  /** 장애 GPU uuid — 셀 ring 빨강 오버레이. */
  faultUuids: Set<string>;
  /** 온도 이상 GPU uuid — 셀 우상단 주황 점 오버레이. */
  tempAlertUuids: Set<string>;
  /** 에이전트 무응답 노드 instance — 셀 점선 빈칸(데이터 없음) 처리. */
  missingInstances: Set<string>;
  onSelectNode: (instance: string) => void;
}

type DelegatedHandler = (e: ReactMouseEvent<HTMLDivElement>) => void;

interface ChunkSectionProps {
  chunk: ClusterChunk;
  /** 이 청크의 오버레이 상태 시그니처 — memo 비교자 전용(본문 미사용). */
  overlaySig: string;
  faultUuids: Set<string>;
  tempAlertUuids: Set<string>;
  missingInstances: Set<string>;
  onMove: DelegatedHandler;
  onLeave: () => void;
  onCellClick: DelegatedHandler;
}

const ChunkSection = memo(
  function ChunkSection({
    chunk,
    faultUuids,
    tempAlertUuids,
    missingInstances,
    onMove,
    onLeave,
    onCellClick,
  }: ChunkSectionProps) {
    return (
      <section className="flex flex-col gap-1.5">
        <div className="flex items-center gap-2">
          <span className="font-medium text-sm">{chunk.key}</span>
          {chunk.csp ? (
            <Badge variant="outline" className="text-[10px]">
              {chunk.csp}
            </Badge>
          ) : null}
          <span className="text-muted-foreground text-xs">
            {formatCount(chunk.cells.length)}장
          </span>
        </div>
        {/* 셀 7,000개 개별 핸들러 금지 계약 — 컨테이너 1곳 위임이 구현이다. */}
        {/* biome-ignore lint/a11y/noStaticElementInteractions: 위임 컨테이너(성능 계약) */}
        <div
          className="flex flex-wrap gap-[2px]"
          data-chunk={chunk.key}
          aria-label={`${chunk.key} GPU ${chunk.cells.length}장 히트맵`}
          onMouseMove={onMove}
          onMouseLeave={onLeave}
          onClick={onCellClick}
        >
          {chunk.cells.map((cell, i) => {
            const missing = missingInstances.has(cell.instance);
            const fault = faultUuids.has(cell.uuid);
            const temp = tempAlertUuids.has(cell.uuid);
            let cls = 'size-2.5 cursor-pointer rounded-[2px] ';
            cls += missing
              ? 'border border-dashed border-muted-foreground/50 bg-transparent'
              : UTIL_BAND_CLASSES[cell.band] ?? UTIL_BAND_CLASSES[0];
            if (fault) cls += ' ring-2 ring-metric-fault';
            if (temp) cls += ' relative';
            return (
              // biome-ignore lint/suspicious/noArrayIndexKey: 셀 순서는 useFleet 정렬로 고정 — 인덱스가 안정 키
              <div key={i} data-i={i} className={cls}>
                {temp ? (
                  <span className="-top-0.5 -right-0.5 absolute size-1.5 rounded-full bg-metric-thermal" />
                ) : null}
              </div>
            );
          })}
        </div>
      </section>
    );
  },
  // 핸들러 prop 은 의도적으로 비교하지 않는다 — identity 고정(ref 경유) 계약.
  (prev, next) =>
    prev.chunk.key === next.chunk.key &&
    prev.chunk.signature === next.chunk.signature &&
    prev.overlaySig === next.overlaySig,
);

/** 툴팁 폭(max-w-60=240px) — 우측 이탈 클램프 기준. */
const TIP_WIDTH_PX = 240;

export default function FleetHeatmap({
  chunks,
  faultUuids,
  tempAlertUuids,
  missingInstances,
  onSelectNode,
}: FleetHeatmapProps) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const tipRef = useRef<HTMLDivElement | null>(null);

  // memo-skip 된 청크의 구 핸들러 클로저에서도 최신 데이터를 보도록 조회는
  // 전부 ref 경유 — 렌더마다 최신 값으로 갱신한다.
  const chunkMap = useMemo(() => new Map(chunks.map((c) => [c.key, c])), [chunks]);
  const chunkMapRef = useRef(chunkMap);
  chunkMapRef.current = chunkMap;
  const selectRef = useRef(onSelectNode);
  selectRef.current = onSelectNode;

  const cellFromEvent = useCallback((e: ReactMouseEvent<HTMLDivElement>): FleetCell | null => {
    const hit = (e.target as HTMLElement).closest('[data-i]');
    if (!(hit instanceof HTMLElement)) return null;
    const idx = Number(hit.dataset.i);
    const key = e.currentTarget.dataset.chunk ?? '';
    return chunkMapRef.current.get(key)?.cells[idx] ?? null;
  }, []);

  const hideTip = useCallback(() => {
    if (tipRef.current) tipRef.current.style.display = 'none';
  }, []);

  const handleMove = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      const tip = tipRef.current;
      const root = rootRef.current;
      if (!tip || !root) return;
      const cell = cellFromEvent(e);
      if (!cell) {
        tip.style.display = 'none';
        return;
      }
      const rect = root.getBoundingClientRect();
      const x = Math.min(e.clientX - rect.left + 12, Math.max(0, rect.width - TIP_WIDTH_PX));
      const y = e.clientY - rect.top + 14;
      tip.style.display = 'block';
      tip.style.transform = `translate(${x}px, ${y}px)`;
      // textContent + whitespace-pre — HTML 주입 경로 자체가 없다.
      tip.textContent = [
        cell.instance,
        `${cell.device}${cell.model ? ` · ${cell.model}` : ''}`,
        `사용률 ${formatPct(cell.util)} · 풀 ${cell.pool || '—'}`,
      ].join('\n');
    },
    [cellFromEvent],
  );

  const handleCellClick = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      const cell = cellFromEvent(e);
      if (cell) selectRef.current(cell.instance);
    },
    [cellFromEvent],
  );

  // 청크별 오버레이 시그니처 — Set 참조가 폴링마다 갈려도 내용이 같으면 같은
  // 문자열이 나와 memo 가 skip 한다(O(총 셀 수) 문자열 1회 — DOM diff 대비 염가).
  const overlaySigs = useMemo(() => {
    const sigs = new Map<string, string>();
    for (const chunk of chunks) {
      let sig = '';
      for (let i = 0; i < chunk.cells.length; i++) {
        const c = chunk.cells[i];
        const f = faultUuids.has(c.uuid);
        const t = tempAlertUuids.has(c.uuid);
        const m = missingInstances.has(c.instance);
        if (f || t || m) sig += `${i}${f ? 'f' : ''}${t ? 't' : ''}${m ? 'm' : ''};`;
      }
      sigs.set(chunk.key, sig);
    }
    return sigs;
  }, [chunks, faultUuids, tempAlertUuids, missingInstances]);

  const total = useMemo(() => chunks.reduce((sum, c) => sum + c.cells.length, 0), [chunks]);

  if (total === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        GPU 시리즈 없음 — 에이전트 배포 또는 데모 모드 확인
      </p>
    );
  }

  return (
    <div ref={rootRef} className="relative flex flex-col gap-4">
      {chunks.map((chunk) => (
        <ChunkSection
          key={chunk.key}
          chunk={chunk}
          overlaySig={overlaySigs.get(chunk.key) ?? ''}
          faultUuids={faultUuids}
          tempAlertUuids={tempAlertUuids}
          missingInstances={missingInstances}
          onMove={handleMove}
          onLeave={hideTip}
          onCellClick={handleCellClick}
        />
      ))}
      {/* 플로팅 툴팁 — 포털/Radix 금지 계약: 단순 div 1개, ref 직접 갱신 */}
      <div
        ref={tipRef}
        className="pointer-events-none absolute top-0 left-0 z-40 max-w-60 whitespace-pre rounded-md border bg-popover px-2.5 py-1.5 text-popover-foreground text-xs shadow-md"
        style={{ display: 'none' }}
      />
    </div>
  );
}
