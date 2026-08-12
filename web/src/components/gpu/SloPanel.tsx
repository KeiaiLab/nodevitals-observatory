// SloPanel — 가용 GPU 비율 SLO 카드(NFR-04). 순간값만 있으면 모니터고,
// "목표 대비 잔여 오차 예산"이 있어야 운영 콘솔이 된다. 상단 4px 라인 =
// 위반 여부(관제 규약: 상단 라인 + 배지), 예산 바 = 30일 창 소진 상태.
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import type { SLOState } from '@/lib/demoApi';

export interface SloPanelProps {
  slo: SLOState;
}

// 예산 바 색 — 잔여 30% 미만 주황, 10% 미만 빨강. Tailwind JIT 는 정적 클래스
// 문자열만 수집하므로 조합이 아닌 완성형으로 둔다(GpuDeviceCard 관례 미러).
function budgetBarClass(remainingPct: number): string {
  if (remainingPct < 10) return 'h-2 [&>[data-slot=progress-indicator]]:bg-metric-fault';
  if (remainingPct < 30) return 'h-2 [&>[data-slot=progress-indicator]]:bg-metric-thermal';
  return 'h-2 [&>[data-slot=progress-indicator]]:bg-metric-pod';
}

function SubMetric({ label, value, unit }: { label: string; value: string; unit: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground text-xs">{label}</span>
      <span className="font-semibold text-lg tabular-nums">
        {value}
        <span className="ml-0.5 font-normal text-muted-foreground text-xs">{unit}</span>
      </span>
    </div>
  );
}

export default function SloPanel({ slo }: SloPanelProps) {
  const accent = slo.breaching ? 'var(--metric-fault)' : 'var(--metric-pod)';
  const delta = slo.availabilityPct - slo.targetPct;
  const budget = Math.max(0, Math.min(100, slo.errorBudgetRemainingPct));

  return (
    <Card className="gap-0 py-0" style={{ borderTop: `4px solid ${accent}` }}>
      <CardContent className="flex flex-col gap-4 p-4">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="flex flex-col gap-1">
            <span className="text-muted-foreground text-xs">가용 GPU 비율 (SLO)</span>
            <div className="flex items-end gap-2">
              <span
                className="font-semibold text-4xl leading-none tabular-nums"
                style={{ color: accent }}
              >
                {slo.availabilityPct.toFixed(2)}%
              </span>
              <span className="text-muted-foreground text-xs">
                목표 {slo.targetPct.toFixed(1)}% · {delta >= 0 ? '+' : ''}
                {delta.toFixed(2)}pp
              </span>
            </div>
          </div>
          {slo.breaching ? (
            <Badge variant="destructive">SLO 위반</Badge>
          ) : (
            <Badge variant="outline" className="text-metric-pod">
              정상
            </Badge>
          )}
        </div>

        <div className="flex flex-col gap-1.5">
          <div className="flex items-baseline justify-between">
            <span className="text-muted-foreground text-xs">오차 예산 잔여</span>
            <span className="font-medium text-sm tabular-nums">{budget.toFixed(1)}%</span>
          </div>
          <Progress value={budget} className={budgetBarClass(budget)} />
          <span className="text-muted-foreground text-[11px]">
            {slo.windowDays}일 창 — 예산을 다 쓰면 신규 변경을 멈추고 안정화에 들어간다
          </span>
        </div>

        <div className="grid grid-cols-3 gap-3">
          <SubMetric label="MTBF (평균 무장애 간격)" value={slo.mtbfHours.toFixed(1)} unit="시간" />
          <SubMetric label="MTTR (평균 복구 시간)" value={slo.mttrMinutes.toFixed(0)} unit="분" />
          <SubMetric
            label="30일 인시던트"
            value={slo.incidents30d.toLocaleString('ko-KR')}
            unit="건"
          />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
          <span className="text-muted-foreground text-xs">
            현재 서비스 불가 GPU{' '}
            <span className="font-semibold text-foreground tabular-nums">
              {slo.unavailableGpus.toLocaleString('ko-KR')}장
            </span>
          </span>
          <span className="text-muted-foreground text-[11px]">
            가용 GPU 비율 = (전체 − 장애·격리·수집중단) / 전체
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
