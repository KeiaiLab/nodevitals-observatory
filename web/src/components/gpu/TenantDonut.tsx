// TenantDonut — demo1 "테넌트별 사용량" 위젯. pools 를 tenant 로 allocated 합산해
// 도넛 + 우측 범례(테넌트명 + %)로 보인다. 색은 index.css 의 --chart-* 토큰.
import { Cell, Pie, PieChart, ResponsiveContainer } from 'recharts';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { formatCount } from '@/lib/gpuMetrics';

// 도넛 조각 색 — 테넌트 수만큼 순환. index.css --chart-1..5.
const SLICE_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
];

interface TenantSlice {
  tenant: string;
  name: string;
  allocated: number;
}

function aggregate(
  pools: { tenant: string; allocated: number }[],
  tenants: { id: string; display: string }[],
): TenantSlice[] {
  const nameById = new Map(tenants.map((t) => [t.id, t.display]));
  const byTenant = new Map<string, number>();
  for (const p of pools) {
    byTenant.set(p.tenant, (byTenant.get(p.tenant) ?? 0) + p.allocated);
  }
  return [...byTenant.entries()]
    .map(([tenant, allocated]) => ({ tenant, name: nameById.get(tenant) ?? tenant, allocated }))
    .sort((a, b) => b.allocated - a.allocated);
}

export default function TenantDonut({
  pools,
  tenants,
}: {
  pools: { tenant: string; allocated: number }[];
  tenants: { id: string; display: string }[];
}) {
  const slices = aggregate(pools, tenants);
  const total = slices.reduce((s, x) => s + x.allocated, 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle>테넌트별 사용량</CardTitle>
      </CardHeader>
      <CardContent>
        {total === 0 ? (
          <p className="py-6 text-center text-muted-foreground text-sm">할당된 GPU 가 없다.</p>
        ) : (
          <div className="flex items-center gap-4">
            <div className="relative h-28 w-28 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={slices}
                    dataKey="allocated"
                    nameKey="name"
                    innerRadius={34}
                    outerRadius={54}
                    paddingAngle={2}
                    isAnimationActive={false}
                    stroke="var(--card)"
                    strokeWidth={2}
                  >
                    {slices.map((s, i) => (
                      <Cell key={s.tenant} fill={SLICE_COLORS[i % SLICE_COLORS.length]} />
                    ))}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
              {/* 중앙 총합 — 전체 할당 GPU 수 */}
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                <span className="font-semibold text-lg tabular-nums leading-none">
                  {formatCount(total)}
                </span>
                <span className="text-[10px] text-muted-foreground">전체 GPU</span>
              </div>
            </div>
            <ul className="flex min-w-0 flex-1 flex-col gap-1.5">
              {slices.map((s, i) => (
                <li key={s.tenant} className="flex items-center gap-2 text-sm">
                  <span
                    className="size-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: SLICE_COLORS[i % SLICE_COLORS.length] }}
                  />
                  <span className="min-w-0 flex-1 truncate">{s.name}</span>
                  <span className="shrink-0 font-semibold tabular-nums">
                    {formatCount(s.allocated)}
                  </span>
                  <span className="shrink-0 text-muted-foreground text-xs tabular-nums">
                    {`${Math.round((s.allocated / total) * 100)}%`}
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
