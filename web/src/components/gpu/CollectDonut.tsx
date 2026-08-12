// CollectDonut — demo1 우하단 "수집 상태" 위젯. collect[] 를 tone 색 도넛으로 보이고
// 중앙에 총 노드수, 우측에 라벨·건수·비율 범례를 둔다. IdleDonut 도넛 패턴 계승.
import { Cell, Pie, PieChart, ResponsiveContainer } from 'recharts';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { StatusCount } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

// tone → 조각 색. 라벨 문자열이 아니라 tone 이 색을 정한다(StatusCount 계약).
const TONE_COLORS: Record<StatusCount['tone'], string> = {
  ok: 'var(--metric-pod)',
  info: 'var(--metric-cpu)',
  warn: 'var(--metric-thermal)',
  major: 'var(--metric-fault)',
  crit: 'var(--metric-fault)',
  muted: 'var(--muted-foreground)',
};

export default function CollectDonut({ collect }: { collect: StatusCount[] }) {
  const slices = collect.filter((c) => c.count > 0);
  const total = slices.reduce((s, x) => s + x.count, 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle>수집 상태</CardTitle>
      </CardHeader>
      <CardContent>
        {total === 0 ? (
          <p className="py-6 text-center text-muted-foreground text-sm">수집 대상이 없다.</p>
        ) : (
          <div className="flex items-center gap-4">
            <div className="relative h-28 w-28 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={slices}
                    dataKey="count"
                    nameKey="label"
                    innerRadius={34}
                    outerRadius={54}
                    paddingAngle={2}
                    isAnimationActive={false}
                    stroke="var(--card)"
                    strokeWidth={2}
                  >
                    {slices.map((s) => (
                      <Cell key={s.key} fill={TONE_COLORS[s.tone]} />
                    ))}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
              {/* 중앙 총 노드수 오버레이 */}
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                <span className="font-semibold text-lg tabular-nums leading-none">
                  {formatCount(total)}
                </span>
                <span className="text-muted-foreground text-xs">노드</span>
              </div>
            </div>
            <ul className="flex min-w-0 flex-1 flex-col gap-1.5">
              {slices.map((s) => (
                <li key={s.key} className="flex items-center gap-2 text-sm">
                  <span
                    className="size-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: TONE_COLORS[s.tone] }}
                  />
                  <span className="min-w-0 flex-1 truncate">{s.label}</span>
                  <span className="shrink-0 font-semibold tabular-nums">{formatCount(s.count)}</span>
                  <span className="shrink-0 text-muted-foreground text-xs tabular-nums">
                    {`${Math.round((s.count / total) * 100)}%`}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
