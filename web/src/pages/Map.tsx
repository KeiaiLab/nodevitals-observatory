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
import { METRIC_GPU_TEMP as GPU_TEMP_METRIC } from '@/lib/gpuMetrics';
import { cn } from '@/lib/utils';

/** Overview 와 동일 주기(m5-design.md §4.4, M4 POLL_INTERVAL 계승). */
const POLL_MS = 15_000;
const NO_DATA_SWATCH = 'bg-util-band-0';

/** 한 화면에 담는 노드 행 수 — 플릿 전체(수백 대)를 다 그리면 페이지가 수십
 *  화면 높이가 되어 히트맵이 "한눈에 보는" 물건이 아니게 된다. 나머지는 총계로만
 *  알린다. */
const MAX_ROWS = 24;

interface Band {
  shortLabel: string;
  rangeLabel: string;
  swatchClass: string;
  test: (celsius: number) => boolean;
}

// 오름차순 평가 — 배열 순서대로 첫 매치 채택. 색은 통합 상황판 히트맵과 같은
// --util-band-* 토큰을 쓴다(초록→호박→빨강) — 두 히트맵이 같은 제품으로 읽히려면
// 팔레트가 같아야 한다.
//
// 임계는 H100/H200 SXM 실동작 기준이다: 부하 중 75°C 이하는 정상 운전이고,
// 83°C 부터 관찰 대상, 88°C 부근이 thermal throttle 진입이다. 구 임계(50/65/75/85)
// 는 정상 운전 온도를 전부 "높음"으로 칠해 플릿 전체가 불난 것처럼 보였다.
const BANDS: Band[] = [
  { shortLabel: '정상', rangeLabel: '75°C 미만', swatchClass: 'bg-util-band-1', test: (c) => c < 75 },
  { shortLabel: '주의', rangeLabel: '75~82°C', swatchClass: 'bg-util-band-2', test: (c) => c < 83 },
  { shortLabel: '높음', rangeLabel: '83~87°C', swatchClass: 'bg-util-band-3', test: (c) => c < 88 },
  { shortLabel: '위험', rangeLabel: '88°C 이상 (스로틀)', swatchClass: 'bg-util-band-4', test: () => true },
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
            'size-7 rounded-[3px] transition-opacity hover:opacity-80 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring',
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

  const allInstances = useMemo(() => {
    const set = new Set<string>();
    for (const cell of cells?.values() ?? []) set.add(cell.instance);
    return Array.from(set).sort();
  }, [cells]);

  const instances = useMemo(() => allInstances.slice(0, MAX_ROWS), [allInstances]);

  const devices = useMemo(() => {
    const set = new Set<string>();
    for (const cell of cells?.values() ?? []) set.add(cell.device);
    return Array.from(set).sort();
  }, [cells]);

  return (
    <div className="flex flex-col gap-4">
      {/* 제목은 셸이 메뉴 라벨로 이미 그린다(중복 h1 금지 — Overview 와 동일). */}
      <p className="text-muted-foreground text-sm">
        노드별 GPU 온도를 히트맵으로 펼쳐 과열 노드를 한눈에 찾는다.
      </p>

      <Card>
      <CardHeader>
        <CardTitle>노드 × GPU 온도 히트맵</CardTitle>
        <CardDescription>
          행 = 노드, 열 = GPU 디바이스. 최근 1시간 관측된 GPU 를 펼치고 현재 온도(5분 평가창)로
          칠한다.
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
            표시할 GPU 온도 데이터가 없다 — 아직 수집되지 않았거나 대상 노드에 GPU 센서가 없다.
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
                className="grid w-fit gap-1"
                style={{
                  gridTemplateColumns: `auto repeat(${devices.length}, 1.75rem)`,
                }}
              >
                <div />
                {devices.map((device) => (
                  <div
                    key={`head-${device}`}
                    title={device}
                    className="text-center font-medium text-[10px] text-muted-foreground"
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
            {allInstances.length > instances.length &&
              `총 ${allInstances.length.toLocaleString('ko-KR')}대 중 상위 ${instances.length}대 · `}
            마지막 갱신 {updatedAt.toLocaleTimeString('ko-KR')} · {POLL_MS / 1000}초마다 자동 갱신
          </p>
        )}
      </CardContent>
      </Card>
    </div>
  );
}
