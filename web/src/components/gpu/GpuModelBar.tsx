// GpuModelBar — demo1 「GPU 모델별 현황」 위젯: 모델별 GPU 수를 전체 대비 비율
// 가로 막대로 보인다(상위 5개). 색은 index.css 의 --metric-gpu 토큰.
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { formatCount, METRIC_COLORS } from '@/lib/gpuMetrics';

/** "NVIDIA H200" → "H200" — 목업의 짧은 표기(IdleDonut 과 동일 규칙). */
function shortModel(model: string): string {
  return model.replace(/^NVIDIA\s+/, '');
}

export default function GpuModelBar({ models }: { models: { model: string; count: number }[] }) {
  const total = models.reduce((s, m) => s + m.count, 0);
  const top = [...models].sort((a, b) => b.count - a.count).slice(0, 5);

  return (
    <Card className="gap-3 py-4">
      <CardHeader className="px-4">
        <CardTitle className="flex items-baseline justify-between text-sm">
          <span>GPU 모델별 현황</span>
          <span className="font-normal text-muted-foreground text-xs">사용률 Top 5</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="px-4">
        {top.length === 0 ? (
          <p className="py-4 text-center text-muted-foreground text-sm">모델 데이터가 없다.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {top.map((m) => {
              const pct = total > 0 ? (m.count / total) * 100 : 0;
              return (
                <li key={m.model} className="relative overflow-hidden rounded-md bg-muted/40">
                  <div
                    className="absolute inset-y-0 left-0 rounded-md opacity-25"
                    style={{ width: `${pct}%`, backgroundColor: METRIC_COLORS.gpu }}
                  />
                  <div className="relative flex items-center gap-2 px-2.5 py-1.5 text-sm">
                    <span className="min-w-0 flex-1 truncate font-medium" title={m.model}>
                      {shortModel(m.model)}
                    </span>
                    <span className="shrink-0 font-semibold tabular-nums">{formatCount(m.count)}</span>
                    <span className="w-11 shrink-0 text-right text-muted-foreground text-xs tabular-nums">
                      {Math.round(pct)}%
                    </span>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
