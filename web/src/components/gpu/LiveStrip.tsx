// LiveStrip — 상황판 최상단 실시간 지표 띠.
//
// 큰 숫자 + 30분 추세선 + 변화량을 한 칸에 압축해 6개를 가로로 깐다. 관제
// 콘솔에서 숫자만 있으면 "지금 얼마"는 알아도 "오르는 중인지"를 모른다 —
// 추세선이 그 절반을 채운다.
//
// 값은 전부 엔진이 미리 집계한 라이브 시리즈다(합성이지만 실제 시계열로
// 저장·질의된다 — 화면이 임의로 그리는 장식이 아니다).
import { Minus, TrendingDown, TrendingUp } from 'lucide-react';
import Sparkline from '@/components/gpu/Sparkline';
import { Card, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import type { LiveMetricKey, LiveSeriesMap } from '@/hooks/useLiveSeries';
import { formatCount, formatMs, formatPct } from '@/lib/gpuMetrics';

interface MetricDef {
  key: LiveMetricKey;
  label: string;
  color: string;
  format: (v: number) => string;
  /** 값이 오르는 게 나쁜 지표(지연·에러율) — 추세 배지 색을 뒤집는다. */
  higherIsWorse?: boolean;
  formatDelta?: (v: number) => string;
  /** 이 미만은 "보합" — 없으면 반올림돼 0 이 된 변화가 "+0" 으로 찍혀
   *  화면이 고장난 것처럼 보인다(라이브 검증에서 실제로 났다). */
  epsilon: number;
}

const METRICS: readonly MetricDef[] = [
  {
    key: 'util',
    epsilon: 0.05,
    label: 'GPU 사용률',
    color: 'var(--metric-gpu)',
    format: (v) => formatPct(v),
    formatDelta: (v) => `${v >= 0 ? '+' : ''}${v.toFixed(1)}pp`,
  },
  {
    key: 'rps',
    epsilon: 1,
    label: '추론 요청',
    color: 'var(--metric-cpu)',
    format: (v) => `${formatCount(Math.round(v))} rps`,
  },
  {
    key: 'p95',
    epsilon: 1,
    label: '응답 p95',
    color: 'var(--metric-thermal)',
    format: (v) => formatMs(v),
    higherIsWorse: true,
    formatDelta: (v) => `${v >= 0 ? '+' : ''}${v.toFixed(0)}ms`,
  },
  {
    key: 'tokens',
    epsilon: 1000,
    label: '토큰 처리',
    color: 'var(--metric-pod)',
    format: (v) => `${formatCount(Math.round(v))}/s`,
  },
  {
    key: 'errPct',
    epsilon: 0.005,
    label: '에러율',
    color: 'var(--metric-fault)',
    format: (v) => formatPct(v),
    higherIsWorse: true,
    formatDelta: (v) => `${v >= 0 ? '+' : ''}${v.toFixed(2)}pp`,
  },
  {
    key: 'powerKw',
    epsilon: 1,
    label: '전력',
    color: 'var(--metric-mem)',
    format: (v) => `${formatCount(Math.round(v))} kW`,
  },
] as const;

function DeltaBadge({ delta, def }: { delta: number | null; def: MetricDef }) {
  if (delta === null || Math.abs(delta) < def.epsilon) {
    return (
      <span className="flex items-center gap-0.5 text-[10px] text-muted-foreground">
        <Minus className="size-3" />
        보합
      </span>
    );
  }
  const up = delta > 0;
  // 좋고 나쁨은 지표마다 다르다 — 지연이 오르면 빨강, 사용률이 오르면 초록.
  const bad = def.higherIsWorse ? up : !up;
  const Icon = up ? TrendingUp : TrendingDown;
  const text = def.formatDelta
    ? def.formatDelta(delta)
    : `${up ? '+' : ''}${formatCount(Math.round(delta))}`;
  return (
    <span
      className={`flex items-center gap-0.5 text-[10px] tabular-nums ${
        bad ? 'text-metric-fault' : 'text-metric-pod'
      }`}
    >
      <Icon className="size-3" />
      {text}
    </span>
  );
}

export interface LiveStripProps {
  series: LiveSeriesMap | null;
}

export default function LiveStrip({ series }: LiveStripProps) {
  if (!series) {
    return (
      <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-6" aria-busy="true">
        {METRICS.map((m) => (
          <Skeleton key={m.key} className="h-[5.25rem] w-full" />
        ))}
      </div>
    );
  }

  return (
    <div
      className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-6"
      aria-label="실시간 플릿 지표"
    >
      {METRICS.map((def) => {
        const s = series[def.key];
        return (
          <Card key={def.key} className="gap-0 overflow-hidden py-0">
            <CardContent className="flex flex-col gap-0.5 p-2.5 pb-0">
              <div className="flex items-baseline justify-between gap-1">
                <span className="truncate text-[10px] text-muted-foreground">{def.label}</span>
                <DeltaBadge delta={s.delta} def={def} />
              </div>
              <span className="truncate font-semibold text-lg tabular-nums leading-tight">
                {s.last === null ? '—' : def.format(s.last)}
              </span>
            </CardContent>
            <Sparkline values={s.values} color={def.color} height={26} />
          </Card>
        );
      })}
    </div>
  );
}
