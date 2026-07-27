// Explorer — 시리즈 콤보박스 선택 + Recharts 시계열(/api/v1/query_range) + 라벨
// 브라우즈(/api/v1/labels) + 매치된 시리즈 테이블(/api/v1/series). m5-design.md §4.4.
//
// shadcn `chart`(ChartContainer/ChartStyle)는 시리즈 색 토큰을
// `<style dangerouslySetInnerHTML>`로 주입해 m5-constraints.md 의
// "dangerouslySetInnerHTML 금지" 와 충돌한다(m5-design.md 적대검토 F1) — 그래서
// 여기서는 Recharts 원시 컴포넌트를 그대로 쓰고, index.css 의 --chart-1..5 CSS
// 변수를 Line 의 stroke prop 문자열로 직접 참조한다(별도 style 주입 불필요).
import { useEffect, useMemo, useState } from 'react';
import { Check, ChevronsUpDown } from 'lucide-react';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { api, type MatrixData, type SeriesData } from '@/lib/api';
import { cn } from '@/lib/utils';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  METRIC_DEMO_FLEET_ALLOC_PCT,
  METRIC_DEMO_FLEET_FAULTED,
  METRIC_DEMO_FLEET_UTIL_AVG,
  METRIC_DEMO_VICTIM_HEALTH,
  METRIC_GPU_MEM_USED,
  METRIC_GPU_POWER,
  METRIC_GPU_TEMP,
  METRIC_GPU_UTIL,
} from '@/lib/gpuMetrics';

// 프리셋 — 노드 기본 1종 + GPU 하드웨어 4종 + 데모 집계 4종. 이름은 전부
// gpuMetrics.ts 상수를 참조한다(문자열 하드코딩 시 접두 drift 재발).
// "(데모)" 표기 항목은 데모 엔진이 emit 하므로 실서비스 인스턴스에서는 비어 있다.
const PRESET_METRICS = [
  { value: 'node_load1', label: 'node_load1', hint: '1분 평균 부하' },
  { value: METRIC_GPU_UTIL, label: METRIC_GPU_UTIL, hint: 'GPU 사용률' },
  { value: METRIC_GPU_TEMP, label: METRIC_GPU_TEMP, hint: 'GPU 온도' },
  { value: METRIC_GPU_POWER, label: METRIC_GPU_POWER, hint: 'GPU 전력' },
  { value: METRIC_GPU_MEM_USED, label: METRIC_GPU_MEM_USED, hint: 'GPU 메모리 사용' },
  {
    value: METRIC_DEMO_FLEET_UTIL_AVG,
    label: METRIC_DEMO_FLEET_UTIL_AVG,
    hint: '플릿 평균 사용률 (데모)',
  },
  {
    value: METRIC_DEMO_FLEET_ALLOC_PCT,
    label: METRIC_DEMO_FLEET_ALLOC_PCT,
    hint: '플릿 할당율 (데모)',
  },
  {
    value: METRIC_DEMO_FLEET_FAULTED,
    label: METRIC_DEMO_FLEET_FAULTED,
    hint: '장애 GPU 수 (데모)',
  },
  {
    value: METRIC_DEMO_VICTIM_HEALTH,
    label: METRIC_DEMO_VICTIM_HEALTH,
    hint: '대상 GPU Health 점수 (데모)',
  },
] as const;

type RangeKey = '1h' | '6h' | '24h' | '7d';

const RANGE_SECONDS: Record<RangeKey, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 24 * 3600,
};

const RANGE_LABELS: Record<RangeKey, string> = {
  '1h': '최근 1시간',
  '6h': '최근 6시간',
  '24h': '최근 24시간',
  '7d': '최근 7일',
};

const RANGE_KEYS = Object.keys(RANGE_LABELS) as RangeKey[];

// query_range 서버 상한(maxRangePoints=1500, internal/apiserver/server.go)에
// 여유를 둔 화면 해상도 목표(~500 포인트)로 step 을 정한다(m5-design.md §4.4 ②).
const MIN_STEP_SECONDS = 15;
const TARGET_POINTS = 500;

const CHART_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
];

/** `__name__` 을 제외한 나머지 라벨을 이름순 정렬해 `k="v", ...` 문자열로 합친다
 * (시리즈 범례명·매치 테이블이 공유하는 표시 전용 포맷). */
function formatLabelPart(rest: Record<string, string>): string {
  return Object.entries(rest)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}="${v}"`)
    .join(', ');
}

/** 라벨셋을 `metric_name{k="v", ...}` 형태로 포맷한다(표시 전용). */
function formatMetric(metric: Record<string, string>): string {
  const { __name__: name, ...rest } = metric;
  const labelPart = formatLabelPart(rest);
  if (!name) return labelPart ? `{${labelPart}}` : '(이름 없음)';
  return labelPart ? `${name}{${labelPart}}` : name;
}

function formatTick(unixSec: number, rangeKey: RangeKey): string {
  const date = new Date(unixSec * 1000);
  if (rangeKey === '24h' || rangeKey === '7d') {
    return date.toLocaleString('ko-KR', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  }
  return date.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit' });
}

// api.ts 의 request() 는 !ok 응답을 `observatory: HTTP <status> — <path>` 로만
// 던진다(본문의 error 필드는 포함하지 않음) — 상태 코드로만 400 을 구분해
// 셀렉터 문법 오류를 인라인 표기한다(m5-constraints.md "서버가 400 으로 거절하는
// 문법은 인라인 에러 표기").
function describeError(err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  if (message.includes('HTTP 400')) {
    return '셀렉터 또는 파라미터가 올바르지 않다 — metric_name 또는 metric_name{label="value"} 형식만 지원한다(정규식 매처 미지원).';
  }
  return '조회 중 오류가 발생했다.';
}

interface ChartRow {
  time: number;
  [seriesKey: string]: number | null;
}

interface SeriesMeta {
  key: string;
  name: string;
  color: string;
}

// matrix 응답은 시리즈별로 독립된 [t,v][] 를 낸다 — Recharts 가 그릴 수 있는
// 공통 시간축 행 배열로 병합한다. 시리즈에 없는 버킷은 null 로 명시해
// connectNulls=false 가 그 지점만 끊어 그리게 한다(m5-design.md §3.3).
function buildChartData(matrix: MatrixData | null): {
  rows: ChartRow[];
  seriesMeta: SeriesMeta[];
} {
  if (!matrix || matrix.result.length === 0) {
    return { rows: [], seriesMeta: [] };
  }

  const seriesMeta: SeriesMeta[] = matrix.result.map((s, index) => ({
    key: `s${index}`,
    name: formatMetric(s.metric),
    color: CHART_COLORS[index % CHART_COLORS.length],
  }));

  const times = new Set<number>();
  for (const s of matrix.result) {
    for (const [t] of s.values) {
      times.add(t);
    }
  }
  const sortedTimes = Array.from(times).sort((a, b) => a - b);

  const rows: ChartRow[] = [];
  const rowByTime = new Map<number, ChartRow>();
  for (const t of sortedTimes) {
    const row: ChartRow = { time: t };
    for (const meta of seriesMeta) {
      row[meta.key] = null;
    }
    rows.push(row);
    rowByTime.set(t, row);
  }

  matrix.result.forEach((s, index) => {
    const key = seriesMeta[index].key;
    for (const [t, v] of s.values) {
      const row = rowByTime.get(t);
      if (row) row[key] = Number(v);
    }
  });

  return { rows, seriesMeta };
}

export default function Explorer() {
  const [selector, setSelector] = useState<string>(PRESET_METRICS[0].value);
  const [rangeKey, setRangeKey] = useState<RangeKey>('1h');

  const [comboOpen, setComboOpen] = useState(false);
  const [comboInput, setComboInput] = useState('');

  const [labelNames, setLabelNames] = useState<string[]>([]);
  const [labelsError, setLabelsError] = useState<string | null>(null);

  const [matrix, setMatrix] = useState<MatrixData | null>(null);
  const [seriesList, setSeriesList] = useState<SeriesData>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // 라벨 이름 브라우즈 — 마운트 시 1회 조회(m5-constraints.md "라벨 브라우즈").
  useEffect(() => {
    let cancelled = false;
    api
      .labels()
      .then((res) => {
        if (!cancelled) setLabelNames(res.data);
      })
      .catch(() => {
        if (!cancelled) setLabelsError('라벨 목록을 불러오지 못했다.');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // 시리즈 나열 + 시계열 — selector/rangeKey 변경 시마다 재조회.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    const rangeSeconds = RANGE_SECONDS[rangeKey];
    const endSec = Math.floor(Date.now() / 1000);
    const startSec = endSec - rangeSeconds;
    const step = Math.max(MIN_STEP_SECONDS, Math.floor(rangeSeconds / TARGET_POINTS));

    Promise.all([api.rangeQuery(selector, startSec, endSec, step), api.series([selector])])
      .then(([rangeRes, seriesRes]) => {
        if (cancelled) return;
        setMatrix(rangeRes.data);
        setSeriesList(seriesRes.data);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setMatrix(null);
        setSeriesList([]);
        setError(describeError(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [selector, rangeKey]);

  const { rows, seriesMeta } = useMemo(() => buildChartData(matrix), [matrix]);

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>Explorer</CardTitle>
          <CardDescription>
            시리즈를 선택하고 기간을 지정해 시계열을 살펴본다. 셀렉터 문법은{' '}
            <code className="rounded bg-muted px-1 py-0.5">metric_name</code> 또는{' '}
            <code className="rounded bg-muted px-1 py-0.5">
              metric_name{'{'}label="value"{'}'}
            </code>{' '}
            만 지원한다(정규식 매처 미지원, M3-lite 한계).
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <Popover
              open={comboOpen}
              onOpenChange={(open) => {
                setComboOpen(open);
                if (open) setComboInput(selector);
              }}
            >
              <PopoverTrigger asChild>
                <Button
                  variant="outline"
                  role="combobox"
                  aria-expanded={comboOpen}
                  className="w-full justify-between font-mono text-sm sm:w-96"
                >
                  <span className="truncate">{selector}</span>
                  <ChevronsUpDown className="ml-2 size-4 shrink-0 opacity-50" />
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-96 p-0" align="start">
                <Command>
                  <CommandInput
                    placeholder="메트릭 이름 또는 셀렉터 입력…"
                    value={comboInput}
                    onValueChange={setComboInput}
                  />
                  <CommandList>
                    <CommandGroup heading="프리셋">
                      {PRESET_METRICS.map((m) => (
                        <CommandItem
                          key={m.value}
                          value={m.value}
                          keywords={[m.hint]}
                          onSelect={(value) => {
                            setSelector(value);
                            setComboOpen(false);
                          }}
                        >
                          <Check
                            className={cn(
                              'size-4',
                              selector === m.value ? 'opacity-100' : 'opacity-0'
                            )}
                          />
                          <div className="flex flex-col">
                            <span className="font-mono">{m.label}</span>
                            <span className="text-xs text-muted-foreground">{m.hint}</span>
                          </div>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                    {comboInput.trim() !== '' && (
                      <CommandGroup heading="자유 입력" forceMount>
                        <CommandItem
                          value={`__custom__${comboInput}`}
                          forceMount
                          onSelect={() => {
                            setSelector(comboInput.trim());
                            setComboOpen(false);
                          }}
                        >
                          <span className="truncate font-mono">{comboInput}</span>
                          를 셀렉터로 사용
                        </CommandItem>
                      </CommandGroup>
                    )}
                    <CommandEmpty>검색어를 입력하거나 프리셋을 선택한다.</CommandEmpty>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>

            <Select value={rangeKey} onValueChange={(v) => setRangeKey(v as RangeKey)}>
              <SelectTrigger className="w-36" aria-label="조회 기간">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RANGE_KEYS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {RANGE_LABELS[k]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground">
              라벨 이름 브라우즈(값 아님) — 메트릭 전체 목록은 제공하지 않는다(M3-lite 한계).
            </p>
            {labelsError ? (
              <p className="text-xs text-destructive">{labelsError}</p>
            ) : labelNames.length === 0 ? (
              <Skeleton className="h-5 w-48" />
            ) : (
              <div className="flex flex-wrap gap-1">
                {labelNames.map((name) => (
                  <Badge key={name} variant="outline" className="font-mono text-xs">
                    {name}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>시계열</CardTitle>
          <CardDescription>
            {seriesMeta.length > 0
              ? `${seriesMeta.length}개 시리즈 · ${RANGE_LABELS[rangeKey]}`
              : '매치된 시리즈가 없다.'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && rows.length === 0 ? (
            <Skeleton className="h-80 w-full" />
          ) : rows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              표시할 데이터가 없다.
            </p>
          ) : (
            <ResponsiveContainer width="100%" height={320}>
              <LineChart data={rows} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  dataKey="time"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={(t: number) => formatTick(t, rangeKey)}
                  tick={{ fontSize: 12 }}
                  className="fill-muted-foreground"
                />
                <YAxis
                  domain={['auto', 'auto']}
                  width={56}
                  tick={{ fontSize: 12 }}
                  className="fill-muted-foreground"
                />
                <Tooltip
                  labelFormatter={(t) => formatTick(Number(t), rangeKey)}
                  contentStyle={{
                    backgroundColor: 'var(--popover)',
                    borderColor: 'var(--border)',
                    color: 'var(--popover-foreground)',
                    fontSize: 12,
                  }}
                />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                {seriesMeta.map((meta) => (
                  <Line
                    key={meta.key}
                    type="monotone"
                    dataKey={meta.key}
                    name={meta.name}
                    stroke={meta.color}
                    strokeWidth={2}
                    dot={false}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>매치된 시리즈</CardTitle>
          <CardDescription>{seriesList.length}건</CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>메트릭</TableHead>
                <TableHead>레이블</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {seriesList.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={2} className="text-center text-muted-foreground">
                    {loading ? '불러오는 중…' : '매치된 시리즈가 없다.'}
                  </TableCell>
                </TableRow>
              ) : (
                seriesList.map((lset) => {
                  const { __name__: name, ...rest } = lset;
                  const labelPart = formatLabelPart(rest);
                  return (
                    <TableRow key={`${name ?? ''}-${labelPart}`}>
                      <TableCell className="font-mono text-xs">{name ?? '—'}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {labelPart || '—'}
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
