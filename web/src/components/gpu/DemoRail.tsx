// DemoRail — 우하단 고정 "시나리오 리모컨". 발표자는 [다음] 버튼 하나로 6-Step
// 서사 화면을 순회한다. 액션(승인·번인 등)은 각 페이지의 대형 버튼이 담당 —
// 리모컨은 이동 전용(오조작 방지 역할 분리) — 예외는 초기화와 단계 점프로,
// 둘 다 시연 제어용 서버 액션(reset / jump-phase)이며 감사 로그에 남는다.
import { ChevronLeft, ChevronRight, Dices, RotateCcw } from 'lucide-react';
import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useDemo } from '@/hooks/demoContext';
import { DEMO_STEPS, PHASE_LABELS, type ScenarioPhase } from '@/lib/demoApi';

export default function DemoRail() {
  const { state, act, refresh } = useDemo();
  const location = useLocation();
  const navigate = useNavigate();
  const [jumping, setJumping] = useState(false);
  const [jumpError, setJumpError] = useState<string | null>(null);
  const [reseeding, setReseeding] = useState(false);
  const [reseedNote, setReseedNote] = useState<string | null>(null);

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

  const phaseOptions = state?.scenario.phases ?? [];

  // 단계 점프 — 성공 시 그 단계를 조명하는 화면으로 자동 이동(phase→route 역산,
  // 매칭되는 스텝이 없으면 이동하지 않는다).
  async function onJump(next: string) {
    if (jumping || next === phase) return;
    setJumping(true);
    setJumpError(null);
    const outcome = await act('jump-phase', { phase: next as ScenarioPhase });
    setJumping(false);
    if (!outcome.ok) {
      setJumpError(outcome.error);
      return;
    }
    const target = DEMO_STEPS.find((s) => s.phases.includes(next as ScenarioPhase));
    if (target) navigate(target.route);
  }

  // 재시딩 — 플릿 인벤토리·장애 개체를 새 시드로 통째로 다시 만든다. 시연을
  // 반복할 때 매번 같은 노드·같은 uuid 가 나오면 녹화 영상처럼 보인다.
  // 서버 백필이 도는 동안(reseeding) 상태 조회가 잠깐 멎으므로 이유를 표시한다.
  const busy = reseeding || state?.reseeding === true;
  async function onReseed() {
    if (busy) return;
    setReseeding(true);
    setReseedNote(null);
    const outcome = await act('reseed');
    setReseeding(false);
    setReseedNote(outcome.ok ? outcome.result.message : `재시딩 실패 — ${outcome.error}`);
    if (outcome.ok) {
      go(1);
    }
  }

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

        {phaseOptions.length > 0 ? (
          <div className="flex items-center gap-1.5">
            <span className="shrink-0 text-muted-foreground text-[10px]">단계 이동</span>
            <Select value={phase} onValueChange={onJump} disabled={jumping}>
              {/* 기본 h-8 을 리모컨 높이(h-7)로 낮춘다 — 같은 data-size 변형으로 덮어써야 적용된다. */}
              <SelectTrigger
                size="sm"
                className="h-7 flex-1 text-xs data-[size=sm]:h-7"
                aria-label="단계 이동"
              >
                <SelectValue placeholder="단계 선택" />
              </SelectTrigger>
              <SelectContent>
                {phaseOptions.map((p) => (
                  <SelectItem key={p.id} value={p.id} className="text-xs">
                    {p.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}

        <Button
          variant="outline"
          size="sm"
          className="h-7 w-full text-xs"
          disabled={busy}
          onClick={onReseed}
        >
          <Dices className="size-3.5" />
          {busy ? '새 목데이터 생성 중…' : '새 목데이터 생성'}
        </Button>

        {reseedNote ? (
          <p className="text-[10px] text-muted-foreground leading-snug">{reseedNote}</p>
        ) : null}
        {jumpError ? (
          <p className="text-[10px] text-destructive">단계 이동 실패 — {jumpError}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}
