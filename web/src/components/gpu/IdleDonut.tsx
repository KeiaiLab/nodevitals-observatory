// IdleDonut — 유휴 GPU 의 모델별 분포 도넛(demo3 GPU Assistant "유휴 장비" 카드).
// idle[] 를 모델로 집계해 도넛 + 범례로 보인다. 색은 index.css 의 --chart-*
// 토큰(모드별 hue 정의). 코드베이스 첫 PieChart 사용처.
import { Cell, Pie, PieChart, ResponsiveContainer } from 'recharts';
import type { IdleGPU } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

// 도넛 조각 색 — 모델 수만큼 순환. index.css --chart-1..5.
const SLICE_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
];

interface ModelSlice {
  model: string;
  count: number;
}

function aggregate(idle: IdleGPU[]): ModelSlice[] {
  const byModel = new Map<string, number>();
  for (const g of idle) {
    byModel.set(g.model, (byModel.get(g.model) ?? 0) + 1);
  }
  return [...byModel.entries()]
    .map(([model, count]) => ({ model, count }))
    .sort((a, b) => b.count - a.count);
}

/** "NVIDIA H200" → "H200" — 범례를 목업의 짧은 표기에 맞춘다. */
function shortModel(model: string): string {
  return model.replace(/^NVIDIA\s+/, '');
}

export default function IdleDonut({ idle }: { idle: IdleGPU[] }) {
  const slices = aggregate(idle);
  const total = slices.reduce((s, x) => s + x.count, 0);

  if (total === 0) {
    return <p className="py-6 text-center text-muted-foreground text-sm">유휴 GPU 가 없다.</p>;
  }

  return (
    <div className="flex items-center gap-4">
      <div className="h-28 w-28 shrink-0">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={slices}
              dataKey="count"
              nameKey="model"
              innerRadius={34}
              outerRadius={54}
              paddingAngle={2}
              isAnimationActive={false}
              stroke="var(--card)"
              strokeWidth={2}
            >
              {slices.map((s, i) => (
                <Cell key={s.model} fill={SLICE_COLORS[i % SLICE_COLORS.length]} />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>
      </div>
      <ul className="flex min-w-0 flex-1 flex-col gap-1.5">
        {slices.map((s, i) => (
          <li key={s.model} className="flex items-center gap-2 text-sm">
            <span
              className="size-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: SLICE_COLORS[i % SLICE_COLORS.length] }}
            />
            <span className="min-w-0 flex-1 truncate">{shortModel(s.model)}</span>
            <span className="shrink-0 font-semibold tabular-nums">{formatCount(s.count)}</span>
            <span className="shrink-0 text-muted-foreground text-xs tabular-nums">
              {total > 0 ? `${Math.round((s.count / total) * 100)}%` : '—'}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
