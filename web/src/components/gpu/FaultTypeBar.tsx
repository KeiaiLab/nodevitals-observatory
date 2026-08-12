// FaultTypeBar — demo1 "장애 유형 분포" 위젯: 최근 장애 알림을 code 별로 집계해
// 건수 내림차순 세로 막대로 보인다. 막대 색은 그 code 의 최고 심각도로 정한다.
import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { AlertEvent } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

// 심각도 순위 — 한 code 그룹의 최댓값으로 막대 색을 고른다.
const SEV_RANK: Record<AlertEvent['severity'], number> = {
  info: 0,
  warning: 1,
  major: 2,
  critical: 3,
};

// critical → 장애(빨강), major/warning → 온도(주황), else → CPU(파랑).
function barColor(maxRank: number): string {
  if (maxRank >= 3) return 'var(--metric-fault)';
  if (maxRank >= 1) return 'var(--metric-thermal)';
  return 'var(--metric-cpu)';
}

interface FaultBucket {
  code: string;
  count: number;
  maxRank: number;
}

function aggregate(alerts: AlertEvent[]): FaultBucket[] {
  const byCode = new Map<string, FaultBucket>();
  for (const a of alerts) {
    const b = byCode.get(a.code) ?? { code: a.code, count: 0, maxRank: 0 };
    b.count += 1;
    b.maxRank = Math.max(b.maxRank, SEV_RANK[a.severity]);
    byCode.set(a.code, b);
  }
  return [...byCode.values()].sort((x, y) => y.count - x.count).slice(0, 6);
}

/** 긴 code 라벨 축약 — 축이 좁아 12자 넘으면 잘라 …로 표기. */
function shortCode(code: string): string {
  return code.length > 12 ? `${code.slice(0, 11)}…` : code;
}

export default function FaultTypeBar({ alerts }: { alerts: AlertEvent[] }) {
  const buckets = aggregate(alerts);

  return (
    <Card>
      <CardHeader>
        <CardTitle>장애 유형 분포</CardTitle>
      </CardHeader>
      <CardContent>
        {buckets.length === 0 ? (
          <p className="py-6 text-center text-muted-foreground text-sm">장애 없음</p>
        ) : (
          <div className="h-48 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={buckets} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
                <XAxis
                  dataKey="code"
                  tickFormatter={shortCode}
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  interval={0}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  allowDecimals={false}
                  width={32}
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  tickLine={false}
                  axisLine={false}
                />
                <Tooltip
                  cursor={{ fill: 'var(--muted)', opacity: 0.3 }}
                  formatter={(value) => [`${formatCount(Number(value))}건`, '건수']}
                  contentStyle={{
                    background: 'var(--card)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                />
                <Bar dataKey="count" radius={[3, 3, 0, 0]} isAnimationActive={false}>
                  {buckets.map((b) => (
                    <Cell key={b.code} fill={barColor(b.maxRank)} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
