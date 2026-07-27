// GpuDeviceCard — 노드 드릴다운 시트의 GPU 1장 카드. 실시간 지표와 자산
// 정보를 한 장에 합친다: 운영자는 "지금 몇 %"만이 아니라 "무슨 드라이버로,
// 어떤 MIG 로, 어느 파드가 쓰는 장비인가"를 같은 시선에서 본다.
//
// 상단 3px 라인 = util 대역색, util 대형 %(관제 규약 GPU 고정색 자홍),
// mem Progress(보라), temp(주황 · 80°C 초과 시 빨강 강조), power W.
// props 는 순수 값 객체 — 질의/훅 없음(부모 NodeDrillSheet 가 메트릭 join +
// 자산 조회를 담당). 실시간 값이 없으면 그 줄은 "—", 자산이 없으면 자산
// 블록만 생략하고 카드는 그대로 그린다.
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Separator } from '@/components/ui/separator';
import {
  bandForUtil,
  formatBytes,
  formatCelsius,
  formatPct,
  formatWatts,
} from '@/lib/gpuMetrics';
import { formatNvlink, formatPcie, type GpuAsset, NVLINK_OK } from '@/lib/nodeAsset';

/** 시트가 join 한 실시간 표본 — 결측 시리즈는 null/undefined 로 온다. */
export interface GpuLiveSample {
  util?: number | null;
  memUsed?: number | null;
  memTotal?: number | null;
  temp?: number | null;
  power?: number | null;
}

export interface GpuDeviceCardProps {
  device: string;
  live: GpuLiveSample;
  asset?: GpuAsset;
  /** 자산이 없을 때 쓰는 모델명(메트릭 라벨 유래). */
  fallbackModel?: string;
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

/** 자산 정의행 — 라벨 폭을 고정해 카드 2열 그리드에서도 열이 맞는다. */
function AssetRow({
  label,
  value,
  mono,
  alert,
}: {
  label: string;
  value: string;
  mono?: boolean;
  alert?: boolean;
}) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd
        className={[
          'truncate',
          mono ? 'font-mono' : '',
          alert ? 'font-medium text-metric-thermal' : 'text-foreground',
        ]
          .filter(Boolean)
          .join(' ')}
        title={value}
      >
        {value || '—'}
      </dd>
    </>
  );
}

export default function GpuDeviceCard({
  device,
  live,
  asset,
  fallbackModel,
}: GpuDeviceCardProps) {
  const util = live.util ?? null;
  const memUsed = live.memUsed ?? null;
  const memTotal = live.memTotal ?? null;
  const temp = live.temp ?? null;
  const power = live.power ?? null;

  const accent = util === null ? 'var(--muted)' : BAND_VARS[bandForUtil(util).index];
  const memPct = memUsed !== null && memTotal !== null && memTotal > 0 ? (memUsed / memTotal) * 100 : null;
  const tempDanger = temp !== null && temp > TEMP_DANGER_CELSIUS;
  const model = asset?.model || fallbackModel || '';
  const nvlinkDegraded = asset !== undefined && asset.nvlinkStatus !== NVLINK_OK;

  return (
    <Card className="gap-0 py-0" style={{ borderTop: `3px solid ${accent}` }}>
      <CardContent className="flex flex-col gap-2 p-3">
        <div className="flex items-baseline justify-between gap-2">
          <span className="font-medium text-sm">{device}</span>
          <span className="truncate text-muted-foreground text-xs" title={model}>
            {model || '—'}
          </span>
        </div>
        {asset ? (
          <div className="flex flex-wrap items-center gap-1">
            {asset.allocated ? (
              <Badge variant="outline" className="border-metric-pod text-[10px] text-metric-pod">
                할당됨
              </Badge>
            ) : (
              <Badge variant="outline" className="text-[10px]">
                미할당
              </Badge>
            )}
            {nvlinkDegraded ? (
              <Badge
                variant="outline"
                className="border-metric-thermal text-[10px] text-metric-thermal"
              >
                링크 저하
              </Badge>
            ) : null}
            {tempDanger ? (
              <Badge variant="outline" className="border-metric-fault text-[10px] text-metric-fault">
                온도 경보
              </Badge>
            ) : null}
          </div>
        ) : null}
        <span className="font-semibold text-3xl text-metric-gpu leading-none tabular-nums">
          {formatPct(util)}
        </span>
        <div className="flex flex-col gap-1">
          <div className="flex items-center justify-between text-muted-foreground text-xs">
            <span>메모리</span>
            <span className="tabular-nums">
              {formatBytes(memUsed)} / {formatBytes(memTotal)}
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
            온도 {formatCelsius(temp)}
          </span>
          <span className="text-muted-foreground">전력 {formatWatts(power)}</span>
        </div>
        {asset ? (
          <>
            <Separator />
            <dl className="grid grid-cols-[3.25rem_minmax(0,1fr)] gap-x-2 gap-y-0.5 text-[11px] leading-relaxed">
              <AssetRow label="드라이버" value={`${asset.driverVersion} · CUDA ${asset.cudaVersion}`} />
              <AssetRow label="VBIOS" value={asset.vbios} mono />
              <AssetRow label="MIG" value={asset.migProfile} />
              <AssetRow label="PCIe" value={formatPcie(asset.pcieGen, asset.pcieWidth)} />
              <AssetRow
                label="NVLink"
                value={formatNvlink(asset.nvlinkLinks, asset.nvlinkStatus)}
                alert={nvlinkDegraded}
              />
              <AssetRow label="시리얼" value={asset.serial} mono />
              <AssetRow
                label="풀"
                value={asset.tenant ? `${asset.pool} · ${asset.tenant}` : asset.pool}
              />
              {asset.pod ? <AssetRow label="파드" value={asset.pod} mono /> : null}
            </dl>
          </>
        ) : null}
      </CardContent>
    </Card>
  );
}
