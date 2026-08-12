// GateDiagram — 「Kubernetes Ready → 사전 검증 관문 → 운영 풀」 흐름 상태
// 다이어그램. 가로 박스 + 화살표(flex)로 "검증 전 운영 투입 원천 차단" 서사를
// 시각화한다. active 는 ring pulse, blocked 는 빨강 라인.
import type { CSSProperties } from 'react';
import { ChevronRight, Circle, CircleCheck, CircleX } from 'lucide-react';
import { cn } from '@/lib/utils';

export type GateStageStatus = 'done' | 'active' | 'blocked' | 'idle';

export interface GateStage {
  id: string;
  label: string;
  status: GateStageStatus;
}

export interface GateDiagramProps {
  stages: GateStage[];
}

const STATUS_META: Record<
  GateStageStatus,
  { box: string; style?: CSSProperties; caption: string; captionStyle?: CSSProperties }
> = {
  done: {
    box: 'border-2',
    style: { borderColor: 'var(--metric-pod)' },
    caption: '통과',
    captionStyle: { color: 'var(--metric-pod)' },
  },
  active: {
    box: 'animate-pulse border-2 border-primary ring-2 ring-primary/40',
    caption: '검증 중',
  },
  blocked: {
    box: 'border-2',
    style: { borderColor: 'var(--metric-fault)' },
    caption: '투입 차단',
    captionStyle: { color: 'var(--metric-fault)' },
  },
  idle: {
    box: 'border border-border',
    caption: '대기',
    captionStyle: { color: 'var(--muted-foreground)' },
  },
};

function StageIcon({ status }: { status: GateStageStatus }) {
  if (status === 'done') {
    return <CircleCheck className="size-4 shrink-0" style={{ color: 'var(--metric-pod)' }} />;
  }
  if (status === 'blocked') {
    return <CircleX className="size-4 shrink-0" style={{ color: 'var(--metric-fault)' }} />;
  }
  if (status === 'active') {
    return (
      <span className="flex size-4 shrink-0 items-center justify-center">
        <span className="size-2.5 animate-pulse rounded-full bg-primary" />
      </span>
    );
  }
  return <Circle className="size-4 shrink-0 text-muted-foreground/50" />;
}

export default function GateDiagram({ stages }: GateDiagramProps) {
  return (
    <div className="flex items-stretch gap-1 sm:gap-2">
      {stages.map((stage, index) => {
        const meta = STATUS_META[stage.status];
        return (
          <div key={stage.id} className="contents">
            {index > 0 ? (
              <div className="flex items-center">
                <ChevronRight className="size-5 shrink-0 text-muted-foreground" />
              </div>
            ) : null}
            <div
              className={cn(
                'flex min-h-16 flex-1 flex-col items-center justify-center gap-1 rounded-lg bg-card px-2 py-3 text-center',
                meta.box,
              )}
              style={meta.style}
            >
              <div className="flex items-center gap-1.5">
                <StageIcon status={stage.status} />
                <span className="font-medium text-sm">{stage.label}</span>
              </div>
              <span className="text-xs" style={meta.captionStyle}>
                {meta.caption}
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}
