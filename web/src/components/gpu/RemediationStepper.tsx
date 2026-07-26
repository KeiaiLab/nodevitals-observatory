// RemediationStepper — Cordon → Graceful Drain → Taint 세로 3단 스테퍼.
// 단계 상태는 서버 스냅샷(DrainState.steps)이 SSOT — GpuLayout 폴링(진행 장면
// 2s)이 갱신하면 그대로 다시 그린다. drain 단계가 pending 이 아니면 파드 퇴거
// Progress + in-flight 대형 카운터 + "드롭된 요청 0건" 배지를 함께 보인다.
import { Check } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import type { DrainState, StepState } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

export interface RemediationStepperProps {
  drain: DrainState;
}

// 승인 전(steps 미생성 — Go nil 이 [] 로 정규화됨)에도 파이프라인 형태를
// 미리 보여주기 위한 로컬 기본 3단.
const FALLBACK_STEPS: StepState[] = [
  { key: 'cordon', label: 'Cordon — 신규 스케줄 차단', status: 'pending' },
  { key: 'drain', label: 'Graceful Drain — 파드 안전 퇴거', status: 'pending' },
  { key: 'taint', label: 'Taint — 재스케줄 방지', status: 'pending' },
];

function StepIcon({ status }: { status: StepState['status'] }) {
  if (status === 'done') {
    return (
      <span
        className="flex size-6 shrink-0 items-center justify-center rounded-full"
        style={{ backgroundColor: 'var(--metric-pod)' }}
      >
        <Check className="size-3.5 text-white" aria-label="완료" />
      </span>
    );
  }
  if (status === 'running') {
    return (
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full border border-border">
        <span
          aria-label="진행 중"
          className="size-2.5 animate-pulse rounded-full"
          style={{ backgroundColor: 'var(--metric-cpu)' }}
        />
      </span>
    );
  }
  return (
    <span className="flex size-6 shrink-0 items-center justify-center rounded-full border border-border bg-muted">
      <span aria-label="대기" className="size-2 rounded-full bg-muted-foreground/40" />
    </span>
  );
}

function DrainDetail({ drain }: { drain: DrainState }) {
  const evictedPct = drain.podsTotal > 0 ? (drain.podsEvicted / drain.podsTotal) * 100 : 0;
  return (
    <div className="flex flex-col gap-3 rounded-md border bg-muted/40 p-3">
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between text-muted-foreground text-xs">
          <span>파드 퇴거</span>
          <span className="tabular-nums">
            {formatCount(drain.podsEvicted)}/{formatCount(drain.podsTotal)}
          </span>
        </div>
        <Progress value={evictedPct} />
      </div>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <span className="text-muted-foreground text-xs">in-flight 추론 요청</span>
          <span className="font-bold text-3xl leading-none tabular-nums">
            {formatCount(drain.inflight)}
          </span>
          <span className="text-[11px] text-muted-foreground">
            시작 {formatCount(drain.inflightTotal)}건 → 0 으로 감소
          </span>
        </div>
        {drain.droppedRequests === 0 ? (
          <Badge className="text-white" style={{ backgroundColor: 'var(--metric-pod)' }}>
            드롭된 요청 0건
          </Badge>
        ) : (
          <Badge variant="destructive">드롭된 요청 {formatCount(drain.droppedRequests)}건</Badge>
        )}
      </div>
    </div>
  );
}

export default function RemediationStepper({ drain }: RemediationStepperProps) {
  const steps = drain.steps.length > 0 ? drain.steps : FALLBACK_STEPS;
  return (
    <ol className="flex flex-col">
      {steps.map((step, index) => (
        <li key={step.key} className="flex gap-3">
          <div className="flex flex-col items-center">
            <StepIcon status={step.status} />
            {index < steps.length - 1 ? <span className="w-px flex-1 bg-border" /> : null}
          </div>
          <div className="flex min-h-10 flex-1 flex-col gap-2 pb-4 last:pb-0">
            <div className="flex flex-wrap items-center gap-2 pt-0.5">
              <span
                className={
                  step.status === 'pending'
                    ? 'text-muted-foreground text-sm'
                    : 'font-medium text-sm'
                }
              >
                {step.label}
              </span>
              {step.status === 'running' ? (
                <Badge variant="secondary" className="text-[10px]">
                  진행 중
                </Badge>
              ) : null}
              {step.status === 'done' ? (
                <Badge variant="outline" className="text-[10px]">
                  완료
                </Badge>
              ) : null}
            </div>
            {step.key === 'drain' && step.status !== 'pending' ? (
              <DrainDetail drain={drain} />
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}
