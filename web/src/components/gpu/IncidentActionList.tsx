// IncidentActionList — 「권장 조치 / 다음 액션」 6단계 체크리스트(목업 demo2).
//
// 시각 문법은 RemediationStepper(Cordon→Drain→Taint)를 그대로 따른다 — 같은
// 제품 안에서 "단계 진행"이 두 가지 모양으로 보이면 안 된다. 다른 점은 두 가지:
// 순번 원(목업이 번호를 매긴다)과 완료 시각(우측 tabular-nums).
//
// 상태의 진실은 서버(incident.actions)다. 이 목록은 시나리오 단계의 투영이라
// 파이프라인과 따로 놀 수 없고, GpuLayout 폴링이 갱신하면 살아 움직인다.
import { CheckCircle2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import type { IncidentAction } from '@/lib/demoApi';

export interface IncidentActionListProps {
  actions: IncidentAction[];
}

function StepMark({ action }: { action: IncidentAction }) {
  if (action.status === 'done') {
    return (
      <CheckCircle2
        className="size-6 shrink-0"
        style={{ color: 'var(--metric-pod)' }}
        aria-label="완료"
      />
    );
  }
  if (action.status === 'running') {
    return (
      <span
        aria-label="진행 중"
        className="flex size-6 shrink-0 animate-pulse items-center justify-center rounded-full font-semibold text-[11px] text-white tabular-nums"
        style={{ backgroundColor: 'var(--metric-cpu)' }}
      >
        {action.seq}
      </span>
    );
  }
  return (
    <span
      aria-label="대기"
      className="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted font-semibold text-[11px] text-muted-foreground tabular-nums"
    >
      {action.seq}
    </span>
  );
}

export default function IncidentActionList({ actions }: IncidentActionListProps) {
  if (actions.length === 0) {
    return <p className="py-6 text-center text-muted-foreground text-sm">권장 조치가 없다.</p>;
  }
  return (
    <ol className="flex flex-col">
      {actions.map((action, index) => (
        <li key={action.key} className="flex gap-3">
          <div className="flex flex-col items-center">
            <StepMark action={action} />
            {index < actions.length - 1 ? <span className="w-px flex-1 bg-border" /> : null}
          </div>
          <div
            className={`mb-1 flex min-w-0 flex-1 flex-col gap-0.5 pb-3 last:pb-0 ${
              action.status === 'running' ? '-mx-2 rounded-md border border-primary bg-primary/5 px-2 py-1' : ''
            }`}
          >
            <div className="flex flex-wrap items-baseline gap-2">
              <span className="font-semibold text-muted-foreground text-xs tabular-nums">
                {action.seq}
              </span>
              <span
                className={
                  action.status === 'pending'
                    ? 'text-muted-foreground text-sm'
                    : 'font-medium text-sm'
                }
              >
                {action.label}
              </span>
              {action.status === 'done' && action.at ? (
                <span className="ml-auto text-muted-foreground text-[11px] tabular-nums">
                  {new Date(action.at).toLocaleTimeString('ko-KR')}
                </span>
              ) : (
                <Badge
                  variant={action.status === 'running' ? 'default' : 'outline'}
                  className="ml-auto text-[10px]"
                >
                  {action.status === 'running' ? '진행 중' : '대기'}
                </Badge>
              )}
            </div>
            <p className="text-muted-foreground text-xs leading-snug">{action.detail}</p>
          </div>
        </li>
      ))}
    </ol>
  );
}
