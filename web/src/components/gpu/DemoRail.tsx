// DemoRail — 우하단 고정 "시나리오 리모컨". 발표자는 [다음] 버튼 하나로 6-Step
// 서사 화면을 순회한다. 액션(승인·번인 등)은 각 페이지의 대형 버튼이 담당 —
// 리모컨은 이동 전용(오조작 방지 역할 분리).
import { ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react';
import { useLocation, useNavigate } from 'react-router';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { useDemo } from '@/hooks/demoContext';
import { DEMO_STEPS, PHASE_LABELS } from '@/lib/demoApi';

export default function DemoRail() {
  const { state, act, refresh } = useDemo();
  const location = useLocation();
  const navigate = useNavigate();

  // 현재 스텝 = 경로 최장 매치 (예: /gpu/health → step 2, /gpu → step 1).
  const current = DEMO_STEPS.reduce<(typeof DEMO_STEPS)[number]>((best, s) => {
    const match =
      location.pathname === s.route ||
      (s.route !== '/gpu' && location.pathname.startsWith(s.route));
    if (!match) return best;
    return s.route.length > best.route.length ? s : best;
  }, DEMO_STEPS[0]);

  const phase = state?.scenario.phase;
  const liveStep = phase
    ? (DEMO_STEPS.find((s) => s.phases.includes(phase))?.step ?? null)
    : null;

  const go = (step: number) => {
    const target = DEMO_STEPS.find((s) => s.step === step);
    if (target) navigate(target.route);
  };

  return (
    <Card className="fixed right-4 bottom-4 z-50 w-80 border shadow-lg backdrop-blur supports-[backdrop-filter]:bg-card/90">
      <CardContent className="flex flex-col gap-2 p-3">
        <div className="flex items-center justify-between gap-2">
          <span className="text-muted-foreground text-xs font-medium">시연 시나리오</span>
          {phase ? (
            <Badge variant={phase === 'normal' ? 'secondary' : 'default'} className="text-[10px]">
              {PHASE_LABELS[phase]}
            </Badge>
          ) : null}
        </div>

        <div className="flex items-center justify-between gap-1">
          {DEMO_STEPS.map((s) => {
            const isCurrent = s.step === current.step;
            const isLive = s.step === liveStep;
            return (
              <button
                key={s.step}
                type="button"
                onClick={() => go(s.step)}
                className="group flex flex-1 flex-col items-center gap-1 rounded-md px-1 py-1.5 transition-colors hover:bg-accent"
                aria-label={`Step ${s.step} ${s.label}`}
                aria-current={isCurrent ? 'step' : undefined}
              >
                <span
                  className={`flex size-5 items-center justify-center rounded-full text-[10px] font-semibold transition-colors ${
                    isCurrent
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-muted text-muted-foreground group-hover:bg-accent-foreground/10'
                  } ${isLive && !isCurrent ? 'ring-2 ring-metric-fault/70' : ''}`}
                >
                  {s.step}
                </span>
                <span
                  className={`text-[10px] leading-none ${isCurrent ? 'font-semibold' : 'text-muted-foreground'}`}
                >
                  {s.label}
                </span>
              </button>
            );
          })}
        </div>

        <div className="flex items-center gap-1.5">
          <Button
            variant="outline"
            size="sm"
            className="h-7 px-2"
            disabled={current.step <= 1}
            onClick={() => go(current.step - 1)}
            aria-label="이전 단계"
          >
            <ChevronLeft className="size-3.5" />
          </Button>
          <Button
            size="sm"
            className="h-7 flex-1"
            disabled={current.step >= 6}
            onClick={() => go(current.step + 1)}
          >
            다음 단계
            <ChevronRight className="size-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2"
            aria-label="시나리오 초기화"
            onClick={async () => {
              await act('reset');
              refresh();
              go(1);
            }}
          >
            <RotateCcw className="size-3.5" />
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
