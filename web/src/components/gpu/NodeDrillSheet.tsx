// NodeDrillSheet — 히트맵 셀 클릭 시 우측 노드 드릴다운 Sheet. 열릴 때 해당
// 노드의 4메트릭(util/temp/power/mem used+total)을 instance 등호 매처로 instant
// 질의(15s 폴링)한다. instance=null 이면 fetcher 가 질의를 skip 하고 null 을
// 반환 — usePolledQuery 의 enabled 대체 패턴(재구독 신호는 deps=[instance]).
import GpuDeviceCard from '@/components/gpu/GpuDeviceCard';
import { Badge } from '@/components/ui/badge';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
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

  // 노드 전환 직후 이전 노드 결과가 새 헤더 아래 보이는 혼선 방지 — 불일치는 로딩 취급.
  const data = query.data && query.data.instance === instance ? query.data : null;

  return (
    <Sheet
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent side="right" className="w-full sm:max-w-xl">
        <SheetHeader>
          <SheetTitle className="break-all pr-8">{instance ?? ''}</SheetTitle>
          <SheetDescription className="flex flex-wrap items-center gap-1.5">
            {data ? (
              <>
                {data.cluster ? <Badge variant="outline">{data.cluster}</Badge> : null}
                {data.csp ? <Badge variant="outline">{data.csp}</Badge> : null}
                <span>GPU {formatCount(data.devices.length)}장 · 15초마다 자동 갱신</span>
              </>
            ) : (
              <span>노드 상세 조회 중…</span>
            )}
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-y-auto px-4 pb-4">
          {data === null ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Skeleton className="h-36 w-full" />
              <Skeleton className="h-36 w-full" />
              <Skeleton className="h-36 w-full" />
              <Skeleton className="h-36 w-full" />
            </div>
          ) : data.devices.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              이 노드의 GPU 시리즈가 없다 — 최근 평가창 안에 수집된 값이 없다.
            </p>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {data.devices.map((row) => (
                <GpuDeviceCard
                  key={row.device}
                  device={row.device}
                  model={row.model}
                  util={row.util}
                  memUsedBytes={row.memUsed}
                  memTotalBytes={row.memTotal}
                  tempCelsius={row.temp}
                  powerWatts={row.power}
                />
              ))}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
