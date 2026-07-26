// GpuRoadmap — Step 3 "AI 분석 + 지능형 스케줄링" 정적 티저. 폴링 0 —
// 데모 on/off 무관하게 항상 렌더 가능하다(Phase 2·3 비전 소개 전용).
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Separator } from '@/components/ui/separator';
import { cn } from '@/lib/utils';

// 챗 UI 재현용 정적 대화 — 운영자=오른쪽 primary, AI=왼쪽 muted 말풍선.
const CHAT: Array<{ role: 'operator' | 'ai'; text: string }> = [
  { role: 'operator', text: 'GPU 장애가 증가한 원인을 분석해줘.' },
  {
    role: 'ai',
    text: '최근 24시간 장애 경보가 35% 증가했습니다. 주요 원인: 노드 nhn-a-gpu-052 의 정정 ECC 6회 반복 증가, 노드 kakao-b-gpu-017 의 Agent Missing. 격리 검토를 권고합니다.',
  },
  { role: 'operator', text: '온도가 80도를 넘은 GPU 와 관련 워크로드는?' },
  {
    role: 'ai',
    text: "현재 3장입니다. 최고 87°C GPU 는 LLM 서빙 A 풀의 추론 파드가 사용 중이며, Thermal Throttle 5분 지속으로 '주의' 상태로 전환했습니다.",
  },
];

// Scoring 깔때기 — 위→아래 폭 감소로 후보 축소 과정을 표현.
const FUNNEL_STAGES = [
  { label: 'GPU 토폴로지', width: '100%' },
  { label: 'NVLink·NUMA 구조', width: '85%' },
  { label: '테넌트 Quota 정책', width: '70%' },
  { label: '장애·격리 노드 제외', width: '55%' },
];

// 하단 로드맵 스트립 3단.
const ROADMAP_PHASES = [
  {
    title: 'Phase 1 — 현재',
    body: '통합관제 · 자동복구 · 번인',
    badge: '적용 완료',
    accent: 'var(--metric-pod)' as string | undefined,
  },
  {
    title: 'Phase 2',
    body: 'AI 운영 에이전트 — 대화형 조회·분석',
    badge: '의사결정 후 확장',
    accent: undefined,
  },
  {
    title: 'Phase 3',
    body: '지능형 스케줄링 · 페일오버',
    badge: '2027 비전',
    accent: undefined,
  },
];

function ChatBubble({ role, text }: { role: 'operator' | 'ai'; text: string }) {
  const isOperator = role === 'operator';
  return (
    <div className={cn('flex', isOperator ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'max-w-[85%] rounded-lg px-3 py-2 text-sm leading-relaxed',
          isOperator ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground',
        )}
      >
        {text}
      </div>
    </div>
  );
}

export default function GpuRoadmap() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="font-semibold text-xl">AI 분석 + 지능형 스케줄링</h1>
        <p className="text-muted-foreground text-sm">
          로드맵 티저 — Phase 2·3 확장 방향 소개 (정적 화면).
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 1. 대화형 AI 운영 에이전트 */}
        <Card>
          <CardHeader>
            <Badge variant="secondary" className="w-fit">
              Phase 2 · 의사결정 후 확장
            </Badge>
            <CardTitle>대화형 AI 운영 에이전트</CardTitle>
            <CardDescription>관제 데이터를 대화로 조회·분석하는 운영 보조</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex flex-col gap-2 rounded-lg border bg-background/50 p-3">
              {CHAT.map((m) => (
                <ChatBubble key={`${m.role}-${m.text.slice(0, 12)}`} role={m.role} text={m.text} />
              ))}
            </div>
            <p className="text-muted-foreground text-xs">
              조회·요약 중심 읽기 전용 에이전트부터 단계 도입
            </p>
          </CardContent>
        </Card>

        {/* 2. 지능형 워크로드 스케줄링 */}
        <Card>
          <CardHeader>
            <Badge variant="outline" className="w-fit">
              Phase 3 · 2027 비전
            </Badge>
            <CardTitle>지능형 워크로드 스케줄링</CardTitle>
            <CardDescription>
              요청 예: <span className="font-mono">B200 8장 · 80GB · 추론</span>
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col items-center gap-1.5">
              {FUNNEL_STAGES.map((stage) => (
                <div
                  key={stage.label}
                  className="rounded-md border bg-muted/60 px-3 py-1.5 text-center text-xs"
                  style={{ width: stage.width }}
                >
                  {stage.label}
                </div>
              ))}
            </div>

            <Separator />

            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm">
                    Candidate A — 매칭률 <span className="font-semibold tabular-nums">98%</span>
                  </span>
                  <Badge>최적 배치안</Badge>
                </div>
                <Progress
                  value={98}
                  className="h-2 [&_[data-slot=progress-indicator]]:bg-[var(--metric-pod)]"
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm">
                    Candidate B — 매칭률 <span className="font-semibold tabular-nums">75%</span>
                  </span>
                  <Badge variant="outline">대체 추천</Badge>
                </div>
                <Progress
                  value={75}
                  className="h-2 [&_[data-slot=progress-indicator]]:bg-[var(--metric-thermal)]"
                />
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 3. 로드맵 스트립 */}
      <div className="grid gap-3 sm:grid-cols-3">
        {ROADMAP_PHASES.map((p) => (
          <div
            key={p.title}
            className="flex flex-col gap-1.5 rounded-lg border bg-card p-4"
            style={p.accent ? { borderTop: `3px solid ${p.accent}` } : undefined}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-medium text-sm">{p.title}</span>
              <Badge
                variant={p.accent ? 'secondary' : 'outline'}
                style={p.accent ? { color: p.accent } : undefined}
              >
                {p.badge}
              </Badge>
            </div>
            <span className="text-muted-foreground text-xs">{p.body}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
