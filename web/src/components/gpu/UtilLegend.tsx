// UtilLegend — 플릿 히트맵의 사용률 5단 대역 + 오버레이(장애/온도 이상/데이터
// 없음) 범례. 대역 정의는 gpuMetrics UTIL_BANDS(SSOT)를 그대로 그린다.
// Tailwind v4 스캐너는 정적 문자열만 발견하므로 `bg-util-band-${i}` 같은 동적
// 조합 대신 아래 상수 배열로 고정한다 — 히트맵 셀(FleetHeatmap)과 공유.
import { UTIL_BANDS } from '@/lib/gpuMetrics';

/** index.css --util-band-N 토큰의 정적 클래스 매핑 (UTIL_BANDS.index 순서). */
export const UTIL_BAND_CLASSES = [
  'bg-util-band-0',
  'bg-util-band-1',
  'bg-util-band-2',
  'bg-util-band-3',
  'bg-util-band-4',
] as const;

export default function UtilLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-muted-foreground text-xs">
      <span className="font-medium text-foreground">범례</span>
      {UTIL_BANDS.map((band) => (
        <span key={band.index} className="flex items-center gap-1.5">
          <span className={`size-3 rounded-[2px] ${UTIL_BAND_CLASSES[band.index]}`} />
          {band.label}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <span className="size-3 rounded-[2px] bg-util-band-2 ring-2 ring-metric-fault" />
        장애
      </span>
      <span className="flex items-center gap-1.5">
        <span className="relative size-3 rounded-[2px] bg-util-band-1">
          <span className="-top-0.5 -right-0.5 absolute size-1.5 rounded-full bg-metric-thermal" />
        </span>
        온도 이상
      </span>
      <span className="flex items-center gap-1.5">
        <span className="size-3 rounded-[2px] border border-muted-foreground/50 border-dashed bg-transparent" />
        데이터 없음
      </span>
    </div>
  );
}
