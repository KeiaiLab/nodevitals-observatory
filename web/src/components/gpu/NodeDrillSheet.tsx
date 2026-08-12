// NodeDrillSheet — 히트맵 셀 클릭 시 우측 노드 드릴다운 Sheet.
//
// 사용률 숫자만 보이는 드릴다운은 관제 화면이 아니라 그래프 위젯이다. 운영자가
// 노드를 열면 보는 것은 자산 대장이다 — 드라이버·펌웨어·시리얼·MIG·PCIe·
// NVLink·물리 위치(랙/PDU)·보증·K8s 라벨/테인트. 그래야 "이 노드를 교체
// 요청한다"는 판단이 화면 안에서 성립한다. 그래서 3탭 구성:
//   실시간 — GPU별 카드(실시간 지표 + 자산 요약), 15초 폴링
//   자산   — 노드 하드웨어/소프트웨어 대장 + GPU 자산 표
//   K8s    — 라벨/테인트
//
// 질의는 두 갈래다. 실시간 4메트릭(util/temp/power/mem used+total)은 instance
// 등호 매처 instant 질의로 15초 폴링하고, 자산은 변하지 않으므로
// `GET /api/v1/demo/node/{instance}` 를 instance 당 1회만 부른다(폴링 금지).
// instance=null 이면 두 질의 모두 skip — usePolledQuery 의 enabled 대체 패턴
// (재구독 신호는 deps=[instance]).
import { useEffect, useState } from 'react';
import GpuDeviceCard from '@/components/gpu/GpuDeviceCard';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type InstantSample } from '@/lib/api';
import {
  formatCount,
  METRIC_GPU_MEM_TOTAL,
  METRIC_GPU_MEM_USED,
  METRIC_GPU_POWER,
  METRIC_GPU_TEMP,
  METRIC_GPU_UTIL,
  selector,
} from '@/lib/gpuMetrics';
import {
  fetchNodeAsset,
  formatNvlink,
  formatPcie,
  type GpuAsset,
  type NodeAsset,
  nodeAssetMessage,
  NVLINK_OK,
  parseLabel,
  WARRANTY_WARN_DAYS,
  warrantyDaysLeft,
} from '@/lib/nodeAsset';

export interface NodeDrillSheetProps {
  instance: string | null;
  onClose: () => void;
}

interface DeviceRow {
  device: string;
  model: string;
  util: number | null;
  memUsed: number | null;
  memTotal: number | null;
  temp: number | null;
  power: number | null;
}

interface DrillData {
  /** 어느 노드의 결과인지 — 노드 전환 직후 이전 노드 데이터 오표시 방지. */
  instance: string;
  csp: string;
  cluster: string;
  devices: DeviceRow[];
}

/** 실시간 표본과 자산을 device 라벨로 합친 카드 1장분. 둘 중 하나만 있어도 그린다
 *  (자산만 = 수집 중단 노드 / 실시간만 = 자산 API 미도달). */
interface MergedDevice {
  device: string;
  live: DeviceRow | null;
  asset: GpuAsset | null;
}

const POLL_MS = 15_000;

function sampleValue(sample: InstantSample): number | null {
  const v = Number(sample.value[1]);
  return Number.isFinite(v) ? v : null;
}

/** device 라벨의 숫자 접미로 정렬(gpu0, gpu1, … gpu10) — useFleet 정렬과 동일 기준. */
function deviceOrder(device: string): number {
  const n = Number.parseInt(device.replace(/^\D+/, ''), 10);
  return Number.isNaN(n) ? 0 : n;
}

function mergeDevices(live: DeviceRow[], gpus: GpuAsset[]): MergedDevice[] {
  const rows = new Map<string, MergedDevice>();
  const at = (device: string): MergedDevice => {
    let row = rows.get(device);
    if (!row) {
      row = { device, live: null, asset: null };
      rows.set(device, row);
    }
    return row;
  };
  for (const row of live) at(row.device).live = row;
  for (const gpu of gpus) at(gpu.device).asset = gpu;
  return [...rows.values()].sort((a, b) => deviceOrder(a.device) - deviceOrder(b.device));
}

/** 노드 자산 대장의 정의행(라벨/값 2열). */
function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-sm' : 'break-all text-sm'}>
        {value || '—'}
      </dd>
    </>
  );
}

/** 자산 미도달 상태(로딩/오류)의 공통 자리 표시. */
function AssetFallback({ error }: { error: string | null }) {
  if (error !== null) {
    return <p className="text-muted-foreground text-sm">{error}</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      <Skeleton className="h-5 w-40" />
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-5 w-32" />
      <Skeleton className="h-24 w-full" />
    </div>
  );
}

export default function NodeDrillSheet({ instance, onClose }: NodeDrillSheetProps) {
  const query = usePolledQuery<DrillData | null>(
    async () => {
      if (!instance) return null;
      const [utilRes, tempRes, powerRes, memUsedRes, memTotalRes] = await Promise.all([
        api.instantQuery(selector(METRIC_GPU_UTIL, { instance })),
        api.instantQuery(selector(METRIC_GPU_TEMP, { instance })),
        api.instantQuery(selector(METRIC_GPU_POWER, { instance })),
        api.instantQuery(selector(METRIC_GPU_MEM_USED, { instance })),
        api.instantQuery(selector(METRIC_GPU_MEM_TOTAL, { instance })),
      ]);

      // device 라벨 기준 5메트릭 join — 시리즈 일부 결측은 null 로 남긴다.
      const rows = new Map<string, DeviceRow>();
      const rowOf = (device: string): DeviceRow => {
        let row = rows.get(device);
        if (!row) {
          row = { device, model: '', util: null, memUsed: null, memTotal: null, temp: null, power: null };
          rows.set(device, row);
        }
        return row;
      };
      const fill = (
        samples: InstantSample[],
        assign: (row: DeviceRow, v: number | null) => void,
      ) => {
        for (const s of samples) {
          const row = rowOf(s.metric.device ?? 'default');
          if (!row.model && s.metric.gpu_model) row.model = s.metric.gpu_model;
          assign(row, sampleValue(s));
        }
      };
      fill(utilRes.data.result, (row, v) => {
        row.util = v;
      });
      fill(tempRes.data.result, (row, v) => {
        row.temp = v;
      });
      fill(powerRes.data.result, (row, v) => {
        row.power = v;
      });
      fill(memUsedRes.data.result, (row, v) => {
        row.memUsed = v;
      });
      fill(memTotalRes.data.result, (row, v) => {
        row.memTotal = v;
      });

      const head = utilRes.data.result[0]?.metric ?? {};
      return {
        instance,
        csp: head.csp ?? '',
        cluster: head.cluster ?? '',
        devices: [...rows.values()].sort((a, b) => deviceOrder(a.device) - deviceOrder(b.device)),
      };
    },
    POLL_MS,
    [instance],
  );

  // 자산은 노드 수명 동안 불변 — instance 가 바뀔 때 1회만 부른다(폴링 금지).
  const [asset, setAsset] = useState<NodeAsset | null>(null);
  const [assetError, setAssetError] = useState<string | null>(null);

  useEffect(() => {
    setAsset(null);
    setAssetError(null);
    if (!instance) return;
    let cancelled = false;
    fetchNodeAsset(instance)
      .then((next) => {
        if (!cancelled) setAsset(next);
      })
      .catch((err: unknown) => {
        // 401 은 AuthContext 가 로그인 화면 전환으로 처리한다(문구 null).
        if (!cancelled) setAssetError(nodeAssetMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [instance]);

  // 노드 전환 직후 이전 노드 결과가 새 헤더 아래 보이는 혼선 방지 — 불일치는 로딩 취급.
  const live = query.data && query.data.instance === instance ? query.data : null;
  const nodeAsset = asset && asset.instance === instance ? asset : null;
  const devices = mergeDevices(live?.devices ?? [], nodeAsset?.gpus ?? []);

  const warrantyLeft = nodeAsset ? warrantyDaysLeft(nodeAsset.warrantyEnds) : null;
  const warrantyNear = warrantyLeft !== null && warrantyLeft <= WARRANTY_WARN_DAYS;

  // 헤더 한 줄 — 자산이 오면 랙/PDU 까지, 오기 전에는 메트릭 라벨로 버틴다.
  const metaParts: string[] = [];
  const csp = nodeAsset?.csp || live?.csp || '';
  const cluster = nodeAsset?.cluster || live?.cluster || '';
  if (csp) metaParts.push(csp);
  if (cluster) metaParts.push(cluster);
  if (nodeAsset) {
    metaParts.push(`랙 ${nodeAsset.rack}`, `PDU ${nodeAsset.pdu}`, nodeAsset.model);
  }
  const gpuCount = nodeAsset ? nodeAsset.gpuCount : devices.length;
  if (gpuCount > 0) metaParts.push(`GPU ${formatCount(gpuCount)}장`);

  return (
    <Sheet
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent side="right" className="w-full sm:max-w-2xl">
        <SheetHeader className="gap-2 pb-2">
          <SheetTitle className="break-all pr-8 font-mono text-base">{instance ?? ''}</SheetTitle>
          <div className="flex flex-wrap items-center gap-1.5">
            {nodeAsset ? (
              <>
                {nodeAsset.ready ? (
                  <Badge variant="outline" className="border-metric-pod text-metric-pod">
                    Ready
                  </Badge>
                ) : (
                  <Badge variant="destructive">NotReady</Badge>
                )}
                {nodeAsset.cordoned ? (
                  <Badge variant="outline" className="border-metric-thermal text-metric-thermal">
                    Cordon
                  </Badge>
                ) : null}
                {nodeAsset.agentMissing ? <Badge variant="destructive">수집 중단</Badge> : null}
              </>
            ) : (
              <Badge variant="outline" className="text-muted-foreground">
                자산 조회 중
              </Badge>
            )}
          </div>
          <SheetDescription>
            {metaParts.length > 0 ? metaParts.join(' · ') : '노드 상세 조회 중…'}
          </SheetDescription>
        </SheetHeader>

        <Tabs defaultValue="live" className="flex min-h-0 flex-1 flex-col px-4 pb-4">
          <TabsList>
            <TabsTrigger value="live">실시간</TabsTrigger>
            <TabsTrigger value="asset">자산</TabsTrigger>
            <TabsTrigger value="k8s">K8s</TabsTrigger>
          </TabsList>

          {/* ---- 실시간 ---- */}
          <TabsContent value="live" className="min-h-0 overflow-y-auto">
            {devices.length === 0 ? (
              live === null ? (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Skeleton className="h-56 w-full" />
                  <Skeleton className="h-56 w-full" />
                  <Skeleton className="h-56 w-full" />
                  <Skeleton className="h-56 w-full" />
                </div>
              ) : (
                <p className="text-muted-foreground text-sm">
                  이 노드의 GPU 시리즈가 없다 — 최근 평가창 안에 수집된 값이 없다.
                </p>
              )
            ) : (
              <>
                <p className="mb-2 text-muted-foreground text-xs">
                  실시간 지표는 15초마다 자동 갱신 · 자산 정보는 노드 진입 시 1회 조회
                </p>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {devices.map((row) => (
                    <GpuDeviceCard
                      key={row.device}
                      device={row.device}
                      live={{
                        util: row.live?.util ?? null,
                        memUsed: row.live?.memUsed ?? null,
                        memTotal: row.live?.memTotal ?? null,
                        temp: row.live?.temp ?? null,
                        power: row.live?.power ?? null,
                      }}
                      asset={row.asset ?? undefined}
                      fallbackModel={row.live?.model}
                    />
                  ))}
                </div>
              </>
            )}
          </TabsContent>

          {/* ---- 자산 ---- */}
          <TabsContent value="asset" className="min-h-0 overflow-y-auto">
            {nodeAsset === null ? (
              <AssetFallback error={assetError} />
            ) : (
              <div className="flex flex-col gap-4">
                <section>
                  <h3 className="mb-2 font-medium text-sm">하드웨어</h3>
                  <dl className="grid grid-cols-[6.5rem_minmax(0,1fr)] gap-x-3 gap-y-1">
                    <InfoRow label="시리얼" value={nodeAsset.serialNumber} mono />
                    <InfoRow label="모델" value={nodeAsset.model} />
                    <InfoRow label="GPU 수" value={`${formatCount(nodeAsset.gpuCount)}장`} />
                    <InfoRow label="랙" value={nodeAsset.rack} mono />
                    <InfoRow label="PDU" value={nodeAsset.pdu} />
                    <InfoRow label="설치일" value={nodeAsset.installedAt} />
                    <InfoRow label="사용 기간" value={`${formatCount(nodeAsset.ageDays)}일`} />
                    <dt className="text-muted-foreground text-sm">보증 만료</dt>
                    <dd className="flex flex-wrap items-center gap-2 text-sm">
                      <span>{nodeAsset.warrantyEnds || '—'}</span>
                      {warrantyLeft !== null ? (
                        <span className="text-muted-foreground text-xs">
                          {warrantyLeft >= 0
                            ? `${formatCount(warrantyLeft)}일 남음`
                            : `${formatCount(-warrantyLeft)}일 경과`}
                        </span>
                      ) : null}
                      {warrantyNear ? (
                        <Badge
                          variant="outline"
                          className="border-metric-thermal text-metric-thermal"
                        >
                          {warrantyLeft !== null && warrantyLeft < 0
                            ? '보증 만료'
                            : '보증 만료 임박'}
                        </Badge>
                      ) : null}
                    </dd>
                  </dl>
                </section>

                <Separator />

                <section>
                  <h3 className="mb-2 font-medium text-sm">소프트웨어</h3>
                  <dl className="grid grid-cols-[6.5rem_minmax(0,1fr)] gap-x-3 gap-y-1">
                    <InfoRow label="kubelet" value={nodeAsset.kubeletVersion} mono />
                    <InfoRow label="containerd" value={nodeAsset.containerRuntime} mono />
                    <InfoRow label="OS 이미지" value={nodeAsset.osImage} />
                    <InfoRow label="커널" value={nodeAsset.kernelVersion} mono />
                  </dl>
                </section>

                <Separator />

                <section>
                  <h3 className="mb-2 font-medium text-sm">
                    GPU 자산 ({formatCount(nodeAsset.gpus.length)}장)
                  </h3>
                  <Table className="text-xs">
                    <TableHeader>
                      <TableRow>
                        <TableHead className="whitespace-nowrap">device</TableHead>
                        <TableHead className="whitespace-nowrap">시리얼</TableHead>
                        <TableHead className="whitespace-nowrap">VBIOS</TableHead>
                        <TableHead className="whitespace-nowrap">드라이버</TableHead>
                        <TableHead className="whitespace-nowrap">CUDA</TableHead>
                        <TableHead className="whitespace-nowrap">MIG</TableHead>
                        <TableHead className="whitespace-nowrap">PCIe</TableHead>
                        <TableHead className="whitespace-nowrap">NVLink</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {nodeAsset.gpus.map((gpu) => (
                        <TableRow key={gpu.uuid || gpu.device}>
                          <TableCell className="whitespace-nowrap font-medium">
                            {gpu.device}
                          </TableCell>
                          <TableCell className="whitespace-nowrap font-mono">{gpu.serial}</TableCell>
                          <TableCell className="whitespace-nowrap font-mono">{gpu.vbios}</TableCell>
                          <TableCell className="whitespace-nowrap font-mono">
                            {gpu.driverVersion}
                          </TableCell>
                          <TableCell className="whitespace-nowrap">{gpu.cudaVersion}</TableCell>
                          <TableCell className="whitespace-nowrap">{gpu.migProfile}</TableCell>
                          <TableCell className="whitespace-nowrap">
                            {formatPcie(gpu.pcieGen, gpu.pcieWidth)}
                          </TableCell>
                          <TableCell
                            className={
                              gpu.nvlinkStatus === NVLINK_OK
                                ? 'whitespace-nowrap'
                                : 'whitespace-nowrap font-medium text-metric-thermal'
                            }
                          >
                            {formatNvlink(gpu.nvlinkLinks, gpu.nvlinkStatus)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </section>
              </div>
            )}
          </TabsContent>

          {/* ---- K8s ---- */}
          <TabsContent value="k8s" className="min-h-0 overflow-y-auto">
            {nodeAsset === null ? (
              <AssetFallback error={assetError} />
            ) : (
              <div className="flex flex-col gap-4">
                <section>
                  <h3 className="mb-2 font-medium text-sm">
                    라벨 ({formatCount(nodeAsset.labels.length)})
                  </h3>
                  {nodeAsset.labels.length === 0 ? (
                    <p className="text-muted-foreground text-sm">라벨 없음</p>
                  ) : (
                    <Table className="text-xs">
                      <TableHeader>
                        <TableRow>
                          <TableHead>키</TableHead>
                          <TableHead>값</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {nodeAsset.labels.map((raw) => {
                          const { key, value } = parseLabel(raw);
                          return (
                            <TableRow key={raw}>
                              <TableCell className="break-all font-mono">{key}</TableCell>
                              <TableCell className="break-all font-mono">{value || '—'}</TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  )}
                </section>

                <Separator />

                <section>
                  <h3 className="mb-2 font-medium text-sm">
                    테인트 ({formatCount(nodeAsset.taints.length)})
                  </h3>
                  {nodeAsset.taints.length === 0 ? (
                    <p className="text-muted-foreground text-sm">테인트 없음</p>
                  ) : (
                    <div className="flex flex-col items-start gap-1.5">
                      {nodeAsset.taints.map((taint) => (
                        <Badge
                          key={taint}
                          variant="outline"
                          className="max-w-full whitespace-normal break-all border-metric-fault font-mono text-metric-fault"
                        >
                          {taint}
                        </Badge>
                      ))}
                    </div>
                  )}
                  <p className="mt-2 text-muted-foreground text-xs">
                    테인트는 스케줄러가 이 노드를 회피하는 이유다.
                  </p>
                </section>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  );
}
