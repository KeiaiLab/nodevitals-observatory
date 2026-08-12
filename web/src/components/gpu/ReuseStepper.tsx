// ReuseStepper — 재사용 검증 4단계(사전검증 → 번인 테스트 → 가용성 판정 →
// 재투입) 번호 스테퍼. 번호 원 + 사이 연결선 + 아래 라벨/힌트 구성이다.
//
// 관문 다이어그램(GateDiagram)과 축이 다르다: 저쪽은 "통과 여부"(K8s Ready →
// 검증 관문 → 운영 풀)이고 이쪽은 "절차 진행"이다. 다만 상태 어휘는 같은 4종
// (GateStageStatus)을 그대로 쓴다 — 새 상태 이름을 만들지 않는다. 분류는 서버
// (ReuseStep.status)가 하고 이 컴포넌트는 렌더만 한다.
import type { CSSProperties } from 'react';
import type { GateStageStatus } from '@/components/gpu/GateDiagram';
import { cn } from '@/lib/utils';

export interface ReuseStepperStep {
  id: string;
  label: string;
  status: GateStageStatus;
  hint?: string;
}

export interface ReuseStepperProps {
  steps: ReuseStepperStep[];
}

const STEP_STYLE: Record<GateStageStatus, { circle: string; style?: CSSProperties }> = {
  done: {
    circle: 'text-white',
    style: { backgroundColor: 'var(--metric-pod)', borderColor: 'var(--metric-pod)' },
  },
  active: { circle: 'border-primary bg-primary text-primary-foreground' },
  blocked: {
    circle: '',
    style: { color: 'var(--metric-fault)', borderColor: 'var(--metric-fault)' },
  },
  idle: { circle: 'border-border text-muted-foreground' },
};

const LABEL_STYLE: Record<GateStageStatus, { className: string; style?: CSSProperties }> = {
  done: { className: '', style: { color: 'var(--metric-pod)' } },
  active: { className: 'text-primary' },
  blocked: { className: '', style: { color: 'var(--metric-fault)' } },
  idle: { className: 'text-muted-foreground' },
};

export default function ReuseStepper({ steps }: ReuseStepperProps) {
  return (
    <div className="flex items-start">
      {steps.map((step, index) => {
        const circle = STEP_STYLE[step.status];
        const label = LABEL_STYLE[step.status];
        return (
          <div key={step.id} className="contents">
            {index > 0 ? <div className="mt-4 h-px min-w-4 flex-1 bg-border" /> : null}
            <div className="flex w-24 flex-col items-center gap-1.5 sm:w-36">
              <span
                className={cn(
                  'flex size-8 items-center justify-center rounded-full border-2 font-semibold text-sm tabular-nums',
                  circle.circle,
                )}
                style={circle.style}
              >
                {index + 1}
              </span>
              <span
                className={cn('text-center font-medium text-xs', label.className)}
                style={label.style}
              >
                {step.label}
              </span>
              {step.hint ? (
                <span className="text-center text-[11px] text-muted-foreground">{step.hint}</span>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}
