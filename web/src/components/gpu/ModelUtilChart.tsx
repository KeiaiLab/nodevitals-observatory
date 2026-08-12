// ModelUtilChart — demo1 「GPU 사용률 (실시간)」 다중 라인차트.
// 최근 30분 창에서 GPU 모델별(gpu_model 라벨) 추이를 한 축에 겹쳐 그린다.
// 탭 4종(Utilization / Memory / Temperature / Power)은 질의 셀렉터만 바꾸고
// 폴링(15s)은 그대로 유지한다. 색은 --chart-1..5 순환, 전체 평균은
// --muted-foreground 점선. Recharts 는 GpuServing 의 CSP-안전 셋업 미러
// (원시 컴포넌트 + CSS 변수 문자열 직접 참조 + isAnimationActive={false}).
import { useState } from 'react';
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type RangeSeries } from '@/lib/api';
import { formatCelsius, formatWatts } from '@/lib/gpuMetrics';

/** 전체 평균 라인(Utilization 탭 전용) — 기존 플릿 롤업 시리즈 재사용. */
const METRIC_FLEET_UTIL = 'nodevitals_demo_fleet_util_avg_pct';

const TABS = [
  {
    key: 'util',
    label: 'Utilization',
    metric: 'nodevitals_demo_model_util_avg_pct',
    unit: 'pct',
    max: 100,
  },
  {
    key: 'mem',
    label: 'Memory',
    metric: 'nodevitals_demo_model_mem_used_pct',
    unit: 'pct',
    max: 100,
  },
  {
    key: 'temp',
    label: 'Temperature',
    metric: 'nodevitals_demo_model_temp_avg_celsius',
    unit: 'celsius',
    max: null,
  },
  {
    key: 'power',
    label: 'Power',
    metric: 'nodevitals_demo_model_power_avg_watts',
    unit: 'watts',
    max: null,
  },
] as const;

type TabKey = (typeof TABS)[number]['key'];
type Unit = (typeof TABS)[number]['unit'];

const WINDOW_SECONDS = 30 * 60;
const STEP_SECONDS = 15;
const POLL_MS = 15_000;
/** 모델 선 색 — index.css 의 차트 팔레트를 모델 수만큼 순환한다. */
const SERIES_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
] as const;
const AVG_COLOR = 'var(--muted-foreground)';

const CHART_MARGIN = { top: 8, right: 12, left: 0, bottom: 4 } as const;

const TOOLTIP_STYLE = {
  backgroundColor: 'var(--popover)',
  borderColor: 'var(--border)',
  color: 'var(--popover-foreground)',
  fontSize: 12,
} as const;

function formatValue(unit: Unit, v: number | null): string {
  if (v == null || Number.isNaN(v)) return '—';
  if (unit === 'celsius') return formatCelsius(v);
  if (unit === 'watts') return formatWatts(v);
  return `${Math.round(v)}%`;
}

function formatClock(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

/** "NVIDIA H200" → "H200" — 목업의 짧은 표기(GpuModelBar 와 동일 규칙). */
function shortModel(model: string): string {
  return model.replace(/^NVIDIA\s+/, '');
}

function lastValue(values: RangeSeries['values']): number | null {
  for (let i = values.length - 1; i >= 0; i--) {
    const v = Number(values[i][1]);
    if (Number.isFinite(v)) return v;
  }
  return null;
}

interface SeriesMeta {
  /** dataKey — 모델명에 공백/기호가 섞여도 안전하도록 인덱스 키를 쓴다. */
  key: string;
  name: string;
  color: string;
  dashed: boolean;
  current: number | null;
}

interface ChartRow {
  time: number;
  [seriesKey: string]: number | null;
}

/** 시리즈들을 공통 시간축 행으로 병합 — 빠진 버킷은 null(connectNulls=false). */
function toRows(parts: readonly { key: string; values: RangeSeries['values'] }[]): ChartRow[] {
  const byTime = new Map<number, ChartRow>();
  for (const { key, values } of parts) {
    for (const [t, v] of values) {
      let row = byTime.get(t);
      if (!row) {
        row = { time: t };
        for (const p of parts) row[p.key] = null;
        byTime.set(t, row);
      }
      row[key] = Number(v);
    }
  }
  return [...byTime.values()].sort((a, b) => a.time - b.time);
}

export interface ModelUtilChartProps {
  /** null = 데모 모드 판별 중, false = 실서비스 → 질의 skip. */
  demoMode?: boolean | null;
}

export default function ModelUtilChart({ demoMode }: ModelUtilChartProps) {
  const [tab, setTab] = useState<TabKey>('util');
  const active = TABS.find((t) => t.key === tab) ?? TABS[0];
  const enabled = demoMode !== false;

  const trend = usePolledQuery(
    async () => {
      if (!enabled) return null;
      const endSec = Math.floor(Date.now() / 1000);
      const startSec = endSec - WINDOW_SECONDS;
      const [byModel, fleet] = await Promise.all([
        api.rangeQuery(active.metric, startSec, endSec, STEP_SECONDS),
        active.key === 'util'
          ? api.rangeQuery(METRIC_FLEET_UTIL, startSec, endSec, STEP_SECONDS)
          : null,
      ]);

      // 모델명 정렬 — 폴링마다 색이 튀지 않도록 순서를 고정한다.
      const models = [...byModel.data.result].sort((a, b) =>
        (a.metric.gpu_model ?? '').localeCompare(b.metric.gpu_model ?? ''),
      );
      const series: SeriesMeta[] = models.map((s, i) => ({
        key: `m${i}`,
        name: shortModel(s.metric.gpu_model ?? '(unknown)'),
        color: SERIES_COLORS[i % SERIES_COLORS.length],
        dashed: false,
        current: lastValue(s.values),
      }));
      const parts = models.map((s, i) => ({ key: `m${i}`, values: s.values }));

      const avg = fleet?.data.result[0];
      if (avg) {
        series.push({
          key: 'avg',
          name: '전체 평균',
          color: AVG_COLOR,
          dashed: true,
          current: lastValue(avg.values),
        });
        parts.push({ key: 'avg', values: avg.values });
      }

      return { series, rows: toRows(parts) };
    },
    POLL_MS,
    [enabled, tab],
  );

  const rows = trend.data?.rows ?? [];
  const series = trend.data?.series ?? [];
  const nowSec = rows.length > 0 ? rows[rows.length - 1].time : null;
  const loading = enabled && trend.data === null && trend.error === null;

  return (
    <Card className="gap-3 py-4">
      <CardHeader className="px-4">
        <CardTitle className="text-sm">GPU 사용률 (실시간)</CardTitle>
        <CardAction>
          <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)}>
            <TabsList className="h-7">
              {TABS.map((t) => (
                <TabsTrigger key={t.key} value={t.key} className="px-2 text-xs">
                  {t.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </CardAction>
      </CardHeader>
      <CardContent className="px-4">
        {/* 범례 — 모델별 현재값(마지막 포인트) */}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pb-2 text-xs">
          {series.map((s) => (
            <span key={s.key} className="flex items-center gap-1.5">
              <span
                className={`inline-block w-3 border-t-2 ${s.dashed ? 'border-dashed' : ''}`}
                style={{ borderColor: s.color }}
              />
              <span className="text-muted-foreground">{s.name}</span>
              <span className="font-semibold tabular-nums">
                {formatValue(active.unit, s.current)}
              </span>
            </span>
          ))}
        </div>

        <div className="h-[220px]">
          {loading ? (
            <Skeleton className="h-full w-full" />
          ) : rows.length < 2 ? (
            <div className="flex h-full items-center justify-center text-muted-foreground text-sm">
              {enabled ? '데이터 수집 중…' : '데모 인스턴스 전용 지표다.'}
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={rows} margin={CHART_MARGIN}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  dataKey="time"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={formatClock}
                  tick={{ fontSize: 11 }}
                  className="fill-muted-foreground"
                />
                <YAxis
                  domain={active.max === null ? ['auto', 'auto'] : [0, active.max]}
                  ticks={active.max === null ? undefined : [0, 25, 50, 75, 100]}
                  tickFormatter={(v) => formatValue(active.unit, Number(v))}
                  width={48}
                  tick={{ fontSize: 11 }}
                  className="fill-muted-foreground"
                />
                <Tooltip
                  labelFormatter={(t) => formatClock(Number(t))}
                  formatter={(value) => formatValue(active.unit, Number(value))}
                  contentStyle={TOOLTIP_STYLE}
                />
                {/* 현재 시각 기준선 */}
                {nowSec === null ? null : (
                  <ReferenceLine x={nowSec} stroke={AVG_COLOR} strokeDasharray="4 4" />
                )}
                {series.map((s) => (
                  <Line
                    key={s.key}
                    type="monotone"
                    dataKey={s.key}
                    name={s.name}
                    stroke={s.color}
                    strokeWidth={2}
                    strokeDasharray={s.dashed ? '4 3' : undefined}
                    dot={false}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
