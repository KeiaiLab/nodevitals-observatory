// GpuDeviceCard — 노드 드릴다운 시트의 GPU 1장 카드. 상단 3px 라인 = util
// 대역색, util 대형 %(관제 규약 GPU 고정색 자홍), mem Progress(보라),
// temp(주황 · 80°C 초과 시 빨강 강조), power W. props 는 순수 값 객체 —
// 질의/훅 없음(부모 NodeDrillSheet 가 5메트릭 join 을 담당).
import { Card, CardContent } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import {
  bandForUtil,
  formatBytes,
  formatCelsius,
  formatPct,
  formatWatts,
} from '@/lib/gpuMetrics';

export interface GpuDeviceCardProps {
  device: string;
  model: string;
  util: number | null;
  memUsedBytes: number | null;
  memTotalBytes: number | null;
  tempCelsius: number | null;
  powerWatts: number | null;
}

/** util 대역 → --util-band-N 토큰 참조 (동적 var 이름 조합 금지 — 정적 배열). */
const BAND_VARS = [
  'var(--util-band-0)',
  'var(--util-band-1)',
  'var(--util-band-2)',
  'var(--util-band-3)',
  'var(--util-band-4)',
] as const;

/** 온도 강조 임계 — 이 값을 초과하면 주황 대신 빨강(장애색)으로 표시. */
const TEMP_DANGER_CELSIUS = 80;

export default function GpuDeviceCard({
  device,
  model,
  util,
  memUsedBytes,
  memTotalBytes,
  tempCelsius,
  powerWatts,
}: GpuDeviceCardProps) {
  const accent = util === null ? 'var(--muted)' : BAND_VARS[bandForUtil(util).index];
  const memPct =
    memUsedBytes !== null && memTotalBytes !== null && memTotalBytes > 0
      ? (memUsedBytes / memTotalBytes) * 100
      : null;
  const tempDanger = tempCelsius !== null && tempCelsius > TEMP_DANGER_CELSIUS;

  return (
    <Card className="gap-0 py-0" style={{ borderTop: `3px solid ${accent}` }}>
      <CardContent className="flex flex-col gap-2 p-3">
        <div className="flex items-baseline justify-between gap-2">
          <span className="font-medium text-sm">{device}</span>
          <span className="truncate text-muted-foreground text-xs" title={model}>
            {model || '—'}
          </span>
        </div>
        <span className="font-semibold text-3xl text-metric-gpu leading-none tabular-nums">
          {formatPct(util)}
        </span>
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between text-muted-foreground text-xs">
            <span>메모리</span>
            <span className="tabular-nums">
              {formatBytes(memUsedBytes)} / {formatBytes(memTotalBytes)}
            </span>
          </div>
          {/* Progress indicator 는 기본 bg-primary — 관제 규약 메모리색(보라)으로 재지정 */}
          <Progress
            value={memPct ?? 0}
            className="h-1.5 [&>[data-slot=progress-indicator]]:bg-metric-mem"
          />
        </div>
        <div className="flex items-center gap-3 text-xs tabular-nums">
          <span className={tempDanger ? 'font-semibold text-metric-fault' : 'text-metric-thermal'}>
            온도 {formatCelsius(tempCelsius)}
          </span>
          <span className="text-muted-foreground">전력 {formatWatts(powerWatts)}</span>
        </div>
      </CardContent>
    </Card>
  );
}
