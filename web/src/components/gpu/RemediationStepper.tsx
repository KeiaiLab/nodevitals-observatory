// RemediationStepper — Cordon → Graceful Drain → Taint 가로 번호 스테퍼.
// 형태는 목업 demo5 의 4단 스테퍼(번호 원 + 연결선 + 하단 라벨·상태)를 그대로
// 따른다 — 같은 제품으로 보이려면 스테퍼 어휘가 화면마다 달라선 안 된다.
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

const STATUS_TEXT: Record<StepState['status'], string> = {
  done: '완료',
  running: '진행 중',
  pending: '대기',
};

// 번호 원 — done=초록 체크 / running=파랑 번호(펄스 링) / pending=회색 번호.
function StepCircle({ status, index }: { status: StepState['status']; index: number }) {
  if (status === 'done') {
    return (
      <span
        className="flex size-9 shrink-0 items-center justify-center rounded-full"
        style={{ backgroundColor: 'var(--metric-pod)' }}
      >
        <Check className="size-5 text-white" aria-label="완료" />
      </span>
    );
  }
  if (status === 'running') {
    return (
      <span
        className="flex size-9 shrink-0 animate-pulse items-center justify-center rounded-full font-semibold text-sm text-white"
        style={{
          backgroundColor: 'var(--metric-cpu)',
          boxShadow: '0 0 0 4px color-mix(in srgb, var(--metric-cpu) 18%, transparent)',
        }}
      >
        {index + 1}
      </span>
    );
  }
  return (
    <span className="flex size-9 shrink-0 items-center justify-center rounded-full border bg-muted font-semibold text-muted-foreground text-sm">
      {index + 1}
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
  const drainStep = steps.find((s) => s.key === 'drain');
  return (
    <div className="flex flex-col gap-5">
      <ol className="flex items-start">
        {steps.map((step, index) => {
          // "Cordon — 신규 워크로드 차단" → 제목/설명 분리(목업 라벨은 짧다).
          const [head, ...rest] = step.label.split(' — ');
          const desc = rest.join(' — ');
          const prevDone = index > 0 && steps[index - 1]?.status === 'done';
          return (
            <li key={step.key} className="flex flex-1 flex-col items-center gap-2">
              <div className="flex w-full items-center">
                <span
                  className="h-px flex-1"
                  style={{
                    backgroundColor:
                      index === 0
                        ? 'transparent'
                        : prevDone
                          ? 'var(--metric-pod)'
                          : 'var(--border)',
                  }}
                />
                <StepCircle status={step.status} index={index} />
                <span
                  className="h-px flex-1"
                  style={{
                    backgroundColor:
                      index === steps.length - 1
                        ? 'transparent'
                        : step.status === 'done'
                          ? 'var(--metric-pod)'
                          : 'var(--border)',
                  }}
                />
              </div>
              <span
                className={
                  step.status === 'pending'
                    ? 'text-center text-muted-foreground text-sm'
                    : 'text-center font-medium text-sm'
                }
              >
                {head}
              </span>
              {desc ? (
                <span className="text-center text-[11px] text-muted-foreground leading-snug">
                  {desc}
                </span>
              ) : null}
              <Badge
                variant={step.status === 'pending' ? 'outline' : 'secondary'}
                className="text-[10px]"
                style={
                  step.status === 'pending'
                    ? undefined
                    : {
                        color: step.status === 'done' ? 'var(--metric-pod)' : 'var(--metric-cpu)',
                      }
                }
              >
                {STATUS_TEXT[step.status]}
              </Badge>
            </li>
          );
        })}
      </ol>
      {drainStep && drainStep.status !== 'pending' ? <DrainDetail drain={drain} /> : null}
    </div>
  );
}
