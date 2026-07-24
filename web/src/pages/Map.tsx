// Map — 노드 × 디바이스 온도 히트맵 (m5-design.md §4.4 Map 행).
// 위상(행×열 존재 여부) = GET /api/v1/series(생략 시 최근 1h, server.go
// defaultSeriesWindow) 로 결정하고, 현재값 = GET /api/v1/query(5m 평가창,
// queryWindow) 로 채운다 — 위상엔 있으나 현재값이 없는 셀은 "데이터 없음"
// (muted) 으로 표시한다(디바이스 최근 무응답 신호. Overview 의 up/down 판정과
// 동일한 "넓은 창 관측 vs 좁은 창 최신값" 비대칭을 재사용하는 설계).
// 셀 배경 = index.css 의 --chart-1..5 토큰을 온도 임계 단계색으로 재사용
// (요청 색만 도입 금지 — 기존 chart 토큰 재사용, m5-design.md §4.4 지시).
// hover 상세 = HoverCard(셀: 라벨 전체 + 값 + 상태) / Tooltip(범례: 임계값 안내).
import { useEffect, useMemo, useState } from 'react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { api, UnauthorizedError } from '@/lib/api';
import { cn } from '@/lib/utils';

/** M4 app.js 계승 상수(m4-design.md Overview 행) — Overview KPI 와 동일 메트릭. */
const GPU_TEMP_METRIC = 'nodevitals_gpu_temperature_celsius';
/** Overview 와 동일 주기(m5-design.md §4.4, M4 POLL_INTERVAL 계승). */
const POLL_MS = 15_000;
const NO_DATA_SWATCH = 'bg-muted';

interface Band {
  shortLabel: string;
  rangeLabel: string;
  swatchClass: string;
  test: (celsius: number) => boolean;
}

// 오름차순 평가 — 배열 순서대로 첫 매치 채택. index.css 토큰을 hue 기준
// 저온(파랑 227°/--chart-3)→고온(적주황 41°/--chart-1) 순으로 배치해 신규
// 색 도입 없이 기존 chart 팔레트를 온도 그라데이션으로 재사용한다.
const BANDS: Band[] = [
  { shortLabel: '정상', rangeLabel: '50°C 미만', swatchClass: 'bg-chart-3', test: (c) => c < 50 },
  { shortLabel: '보통', rangeLabel: '50~64°C', swatchClass: 'bg-chart-2', test: (c) => c < 65 },
  { shortLabel: '따뜻함', rangeLabel: '65~74°C', swatchClass: 'bg-chart-4', test: (c) => c < 75 },
  { shortLabel: '높음', rangeLabel: '75~84°C', swatchClass: 'bg-chart-5', test: (c) => c < 85 },
  { shortLabel: '위험', rangeLabel: '85°C 이상', swatchClass: 'bg-chart-1', test: () => true },
];

function bandFor(celsius: number): Band {
  return BANDS.find((band) => band.test(celsius)) ?? BANDS[BANDS.length - 1];
}

interface Cell {
  instance: string;
  device: string;
  labels: Record<string, string>;
  celsius: number | null;
}

// "|" 구분자 — instance/device 라벨 값에 나타날 일이 없는 문자로 조인해
// 두 라벨을 하나의 그리드 셀 키로 합친다.
function cellKey(instance: string, device: string): string {
  return `${instance}|${device}`;
}

// 스크레이퍼가 모든 시리즈에 instance 라벨을 무조건 주입하므로(m2-design.md
// §4 라벨 매핑 계약) 행 키는 항상 존재 — node 는 exposition 원본 라벨 대비
// 방어적 대체일 뿐.
function labelInstance(labels: Record<string, string>): string {
  return labels.instance ?? labels.node ?? '알수없음';
}

// device 라벨 부재 시 단일 "default" 열로 수렴(§2 Simplicity — 불필요한
// 방어 분기 대신 한 갈래로 처리).
function labelDevice(labels: Record<string, string>): string {
  return labels.device ?? labels.gpu ?? 'default';
}

function GridCell({
  instance,
  device,
  cell,
}: {
  instance: string;
  device: string;
  cell: Cell | undefined;
}) {
  const celsius = cell?.celsius ?? null;
  const band = celsius === null ? null : bandFor(celsius);
  const swatchClass = band?.swatchClass ?? NO_DATA_SWATCH;
  const statusLabel = band ? `${band.shortLabel} (${band.rangeLabel})` : '데이터 없음';

  return (
    <HoverCard openDelay={100} closeDelay={100}>
      <HoverCardTrigger asChild>
        <button
          type="button"
          aria-label={`${instance} / ${device}: ${statusLabel}`}
          className={cn(
            'aspect-square w-full rounded-sm transition-opacity hover:opacity-80 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring',
            swatchClass,
          )}
        />
      </HoverCardTrigger>
      <HoverCardContent>
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <dt className="text-muted-foreground">instance</dt>
          <dd className="truncate font-medium">{instance}</dd>
          <dt className="text-muted-foreground">device</dt>
          <dd className="truncate font-medium">{device}</dd>
          <dt className="text-muted-foreground">값</dt>
          <dd className="font-medium">{celsius === null ? '—' : `${celsius.toFixed(1)}°C`}</dd>
          <dt className="text-muted-foreground">상태</dt>
          <dd>
            <Badge variant="outline">{statusLabel}</Badge>
          </dd>
        </dl>
        {cell && (
          <div className="mt-3 flex flex-wrap gap-1 border-t pt-2">
            {Object.entries(cell.labels).map(([k, v]) => (
              <Badge key={k} variant="secondary" className="font-normal">
                {k}={v}
              </Badge>
            ))}
          </div>
        )}
      </HoverCardContent>
    </HoverCard>
  );
}

export default function MapPage() {
  const [cells, setCells] = useState<Map<string, Cell> | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const [seriesRes, queryRes] = await Promise.all([
          api.series([GPU_TEMP_METRIC]),
          api.instantQuery(GPU_TEMP_METRIC),
        ]);
        if (cancelled) return;

        const next = new Map<string, Cell>();
        for (const labels of seriesRes.data) {
          const instance = labelInstance(labels);
          const device = labelDevice(labels);
          next.set(cellKey(instance, device), { instance, device, labels, celsius: null });
        }
        for (const sample of queryRes.data.result) {
          const instance = labelInstance(sample.metric);
          const device = labelDevice(sample.metric);
          const parsed = Number(sample.value[1]);
          const key = cellKey(instance, device);
          const prev = next.get(key);
          next.set(key, {
            instance,
            device,
            labels: prev?.labels ?? sample.metric,
            celsius: Number.isFinite(parsed) ? parsed : null,
          });
        }

        setCells(next);
        setError(null);
        setUpdatedAt(new Date());
      } catch (err) {
        // 401 은 api.ts 의 onUnauthorized 핸들러가 이미 AuthContext 를 anon 으로
        // 전환해 로그인 화면으로 보낸다 — 여기서 별도 에러 배너를 띄우지 않는다.
        if (cancelled || err instanceof UnauthorizedError) return;
        setError(err instanceof Error ? err.message : 'observatory: 알 수 없는 오류');
      }
    }

    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  const instances = useMemo(() => {
    const set = new Set<string>();
    for (const cell of cells?.values() ?? []) set.add(cell.instance);
    return Array.from(set).sort();
  }, [cells]);

  const devices = useMemo(() => {
    const set = new Set<string>();
    for (const cell of cells?.values() ?? []) set.add(cell.device);
    return Array.from(set).sort();
  }, [cells]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Map</CardTitle>
        <CardDescription>
          노드 × 디바이스 온도 히트맵 — {GPU_TEMP_METRIC} 위상(최근 1시간) + 현재값(5분 평가창).
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {!cells ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-5 w-64" />
            <Skeleton className="h-40 w-full" />
          </div>
        ) : instances.length === 0 || devices.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            표시할 {GPU_TEMP_METRIC} 시리즈가 없다 — 스크레이프 대상에 해당 센서가 없거나 아직
            수집되지 않았다.
          </p>
        ) : (
          <TooltipProvider>
            <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
              <span className="font-medium text-foreground">범례</span>
              {BANDS.map((band) => (
                <Tooltip key={band.shortLabel}>
                  <TooltipTrigger asChild>
                    <span className="flex cursor-default items-center gap-1.5">
                      <span className={cn('size-3 rounded-sm', band.swatchClass)} />
                      {band.shortLabel}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{band.rangeLabel}</TooltipContent>
                </Tooltip>
              ))}
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="flex cursor-default items-center gap-1.5">
                    <span className={cn('size-3 rounded-sm', NO_DATA_SWATCH)} />
                    데이터없음
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  최근 5분 평가창 안에 값을 받지 못했다(디바이스 무응답 가능성)
                </TooltipContent>
              </Tooltip>
            </div>

            <div className="overflow-x-auto">
              <div
                className="grid gap-1"
                style={{
                  gridTemplateColumns: `auto repeat(${devices.length}, minmax(2.5rem, 1fr))`,
                }}
              >
                <div />
                {devices.map((device) => (
                  <div
                    key={`head-${device}`}
                    title={device}
                    className="truncate px-1 text-center text-xs font-medium text-muted-foreground"
                  >
                    {device}
                  </div>
                ))}
                {instances.flatMap((instance) => [
                  <div
                    key={`row-${instance}`}
                    title={instance}
                    className="flex items-center truncate pr-2 text-xs font-medium"
                  >
                    {instance}
                  </div>,
                  ...devices.map((device) => (
                    <GridCell
                      key={`${instance}-${device}`}
                      instance={instance}
                      device={device}
                      cell={cells.get(cellKey(instance, device))}
                    />
                  )),
                ])}
              </div>
            </div>
          </TooltipProvider>
        )}

        {updatedAt && (
          <p className="text-xs text-muted-foreground">
            마지막 갱신 {updatedAt.toLocaleTimeString()} · {POLL_MS / 1000}초마다 자동 갱신
          </p>
        )}
      </CardContent>
    </Card>
  );
}
