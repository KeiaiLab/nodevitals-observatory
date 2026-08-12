// GpuRoadmap — Step 3 "AI 분석 + 지능형 스케줄링". Phase 2·3 은 여전히 로드맵
// 범위지만, 화면은 정적 티저가 아니라 *눌러서 확인할 수 있는* 조회 화면이다.
// 답변·후보는 전부 useDemo() 스냅샷 + useFleet() 셀에서 클라이언트 계산한다 —
// 신규 API 호출·폴링 0(기존 훅이 이미 받아온 데이터만 재사용).
import { AlertTriangle, Boxes, Cpu, Layers, Thermometer } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import KpiTile from '@/components/gpu/KpiTile';
import ScoringFunnel from '@/components/gpu/ScoringFunnel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { useDemo } from '@/hooks/demoContext';
import { useFleet, type FleetCell } from '@/hooks/useFleet';
import { PHASE_LABELS, type DemoAction, type DemoState } from '@/lib/demoApi';
import { formatCount, formatPct } from '@/lib/gpuMetrics';
import { cn } from '@/lib/utils';

const MAX_LIST_ROWS = 5;
const RECENT_ALERTS = 10;
const TOP_N = 3;

// 액션 표기명 — Partial 로 두고 미등록 액션은 원문 키를 그대로 보여준다
// (demoApi 의 DemoAction 이 늘어도 이 화면이 깨지지 않게 한다).
const ACTION_LABELS: Partial<Record<DemoAction, string>> = {
  'approve-isolation': '격리 승인',
  'start-burnin': '번인 검증 시작',
  'return-to-service': '운영 복귀',
  reset: '시나리오 초기화',
  'register-idle-reason': '미사용 사유 등록',
  'report-false-positive': '오탐 피드백',
  'set-mode': '진행 모드 변경',
  'configure-burnin': '번인 설정 변경',
  'jump-phase': '단계 이동',
  'ack-alert': '알림 확인',
};

interface ChatRow {
  label: string;
  value: string;
}

interface ChatMessage {
  id: number;
  role: 'operator' | 'ai';
  text: string;
  rows?: ChatRow[];
  /** 답변 생성 시각(epoch ms) — 말풍선 하단 캡션의 근거. */
  at: number;
}

interface AnswerCtx {
  state: DemoState;
  cells: FleetCell[] | null;
  byUuid: Map<string, FleetCell>;
}

interface Answer {
  text: string;
  rows?: ChatRow[];
}

interface Question {
  id: string;
  /** 칩 문구 = 운영자 말풍선 본문. */
  label: string;
  answer: (ctx: AnswerCtx) => Answer;
}

/** uuid → 사람이 읽는 한 줄. 벽면 인벤토리에 없는 uuid(Agent Missing 노드 소속)는
 *  그대로 uuid 를 노출해 "모른다"를 숨기지 않는다. */
function describeCell(uuid: string, byUuid: Map<string, FleetCell>): ChatRow {
  const cell = byUuid.get(uuid);
  if (!cell) return { label: uuid, value: '인벤토리 조회 불가 (Agent Missing 추정)' };
  return {
    label: `${cell.instance} / ${cell.device}`,
    value: `${cell.model || '모델 미상'} · 풀 ${cell.pool || '미배정'} · ${formatPct(cell.util)}`,
  };
}

interface GroupAvg {
  key: string;
  avg: number;
  count: number;
}

/** 지정한 라벨로 묶어 평균 사용률을 낸다(빈 라벨은 제외). */
function averageUtilBy(cells: FleetCell[], pick: (c: FleetCell) => string): GroupAvg[] {
  const agg = new Map<string, { sum: number; count: number }>();
  for (const c of cells) {
    const key = pick(c);
    if (!key) continue;
    const cur = agg.get(key) ?? { sum: 0, count: 0 };
    cur.sum += c.util;
    cur.count += 1;
    agg.set(key, cur);
  }
  return [...agg.entries()].map(([key, v]) => ({ key, avg: v.sum / v.count, count: v.count }));
}

const NO_FLEET: Answer = { text: '플릿 인벤토리를 아직 불러오지 못했다 — 잠시 후 다시 묻는다.' };

// 질문 6종 — 답변은 전부 라이브 수치 인용(하드코딩 문장 없음).
const QUESTIONS: readonly Question[] = [
  {
    id: 'temp',
    // fleet.tempAlerts 는 *온도 경보가 발행된* GPU 수다(히트맵 주황 점과 같은 집합,
    // state.go FleetSummary 계약). 플릿에서 80°C 를 넘은 GPU 수가 아니다 — 그 수는
    // 매 틱 전수 주사값이고 룰셋 화면 THERMAL 조건 매칭으로 따로 나간다(7,000장 기준
    // 700~800장). 이 답변에 ">80°C 초과" 술어를 쓰면 두 화면이 같은 말로 두 자릿수
    // 다른 수를 주장하게 되므로, 질문·답변 모두 "경보" 어휘로 고정한다.
    label: '지금 온도 경보가 뜬 GPU 는?',
    answer: ({ state, byUuid }) => {
      const { tempAlerts, tempAlertUuids } = state.fleet;
      if (tempAlerts === 0) return { text: '현재 온도 경보가 발행된 GPU 는 없다.' };
      const shown = tempAlertUuids.slice(0, MAX_LIST_ROWS);
      const rest = tempAlerts - shown.length;
      const tail = rest > 0 ? ` 아래 ${shown.length}장 외 ${formatCount(rest)}장이 더 있다.` : '';
      return {
        text: `온도 경보가 발행된 GPU 는 ${formatCount(tempAlerts)}장이다.${tail}`,
        rows: shown.map((uuid) => describeCell(uuid, byUuid)),
      };
    },
  },
  {
    id: 'fault',
    label: '장애 GPU 와 영향 범위는?',
    answer: ({ state, byUuid }) => {
      const { faulted, faultUuids } = state.fleet;
      if (faulted === 0) return { text: '현재 장애로 분류된 GPU 는 없다.' };
      const shown = faultUuids.slice(0, MAX_LIST_ROWS);
      const pools = [
        ...new Set(faultUuids.map((u) => byUuid.get(u)?.pool).filter((p): p is string => !!p)),
      ];
      const rest = faulted - shown.length;
      const tail = rest > 0 ? ` 아래 ${shown.length}장 외 ${formatCount(rest)}장이 더 있다.` : '';
      const poolText = pools.length > 0 ? pools.join(', ') : '풀 미배정(대기 인벤토리)';
      return {
        text: `장애 GPU 는 ${formatCount(faulted)}장이다. 관련 서빙 풀: ${poolText}.${tail}`,
        rows: shown.map((uuid) => describeCell(uuid, byUuid)),
      };
    },
  },
  {
    id: 'low-pool',
    label: '사용률이 가장 낮은 서빙 풀은?',
    answer: ({ cells }) => {
      if (!cells) return NO_FLEET;
      const pools = averageUtilBy(cells, (c) => c.pool);
      if (pools.length === 0) return { text: '풀에 배정된 GPU 가 없다.' };
      const bottom = [...pools].sort((a, b) => a.avg - b.avg).slice(0, TOP_N);
      return {
        text: `서빙 풀 ${formatCount(pools.length)}개 중 평균 사용률 하위 ${bottom.length}개다.`,
        rows: bottom.map((p) => ({
          label: p.key,
          value: `평균 ${formatPct(p.avg)} · ${formatCount(p.count)}장`,
        })),
      };
    },
  },
  {
    id: 'scenario',
    label: '현재 진행 중인 조치는?',
    answer: ({ state }) => {
      const { scenario } = state;
      const victim = scenario.victim;
      const rows: ChatRow[] = [
        { label: '대상 GPU', value: `${victim.instance} / ${victim.device} (${victim.model})` },
        { label: 'Health', value: `${Math.round(victim.health.score)}점 / 100` },
      ];
      const top = victim.health.deductions[0];
      if (top) rows.push({ label: `근거 ${top.code}`, value: `${top.message} (−${top.penalty})` });
      if (victim.drain.active) {
        rows.push({
          label: 'Drain 진행',
          value: `${Math.round(victim.drain.progress)}% · 파드 ${victim.drain.podsEvicted}/${victim.drain.podsTotal}`,
        });
      }
      if (victim.burnin.active) {
        rows.push({
          label: '번인 진행',
          value: `${victim.burnin.attempt}차 · ${Math.round(victim.burnin.progress)}%`,
        });
      }
      if (scenario.pendingAction) {
        rows.push({
          label: '대기 중 승인',
          value: ACTION_LABELS[scenario.pendingAction] ?? scenario.pendingAction,
        });
      }
      return { text: `현재 시나리오 단계는 '${PHASE_LABELS[scenario.phase]}' 다.`, rows };
    },
  },
  {
    id: 'busy-cluster',
    label: '가장 바쁜 클러스터는?',
    answer: ({ cells }) => {
      if (!cells) return NO_FLEET;
      const clusters = averageUtilBy(cells, (c) => c.cluster);
      if (clusters.length === 0) return { text: '클러스터 라벨이 붙은 GPU 가 없다.' };
      const top = [...clusters].sort((a, b) => b.avg - a.avg).slice(0, TOP_N);
      return {
        text: `클러스터 ${formatCount(clusters.length)}개 중 평균 사용률 상위 ${top.length}개다.`,
        rows: top.map((c) => ({
          label: c.key,
          value: `평균 ${formatPct(c.avg)} · ${formatCount(c.count)}장`,
        })),
      };
    },
  },
  {
    id: 'alerts',
    label: '최근 알림 요약해줘',
    answer: ({ state }) => {
      const recent = [...state.alerts].sort((a, b) => b.at - a.at).slice(0, RECENT_ALERTS);
      if (recent.length === 0) return { text: '수신된 알림이 없다.' };
      const counts = { critical: 0, major: 0, warning: 0, info: 0 };
      for (const alert of recent) counts[alert.severity] += 1;
      return {
        text: `최근 알림 ${recent.length}건 — Critical ${counts.critical} · Major ${counts.major} · Warning ${counts.warning} · 정보 ${counts.info}. 대표 2건은 아래와 같다.`,
        rows: recent.slice(0, 2).map((alert) => ({
          label: `${new Date(alert.at).toLocaleTimeString('ko-KR')} · ${alert.code}`,
          value: `${alert.title}${alert.instance ? ` — ${alert.instance}${alert.device ? ` / ${alert.device}` : ''}` : ''}`,
        })),
      };
    },
  },
] as const;

function ChatBubble({ message }: { message: ChatMessage }) {
  const isOperator = message.role === 'operator';
  return (
    <div className={cn('flex', isOperator ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'flex max-w-[92%] flex-col gap-1.5 rounded-lg px-3 py-2 text-sm leading-relaxed',
          isOperator ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground',
        )}
      >
        <span>{message.text}</span>
        {message.rows && message.rows.length > 0 ? (
          <div className="flex flex-col gap-0.5 rounded-md bg-background/60 p-2">
            {message.rows.map((row, index) => (
              // 라벨/값 2열 고정 — flex-wrap 은 길이에 따라 어떤 줄만 붙고 어떤
              // 줄만 접혀 표가 들쭉날쭉해진다(목업의 키-값 표는 열이 맞는다).
              <div
                key={`${row.label}-${index}`}
                className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 border-border/60 border-b py-1 text-xs last:border-0"
              >
                <span className="whitespace-nowrap font-mono">{row.label}</span>
                <span className="truncate text-right text-muted-foreground tabular-nums">
                  {row.value}
                </span>
              </div>
            ))}
          </div>
        ) : null}
        {isOperator ? null : (
          <span className="text-[10px] text-muted-foreground">
            라이브 데이터 기준 · {new Date(message.at).toLocaleTimeString('ko-KR')}
          </span>
        )}
      </div>
    </div>
  );
}

export default function GpuRoadmap() {
  const { demoMode, state } = useDemo();
  const { cells } = useFleet();

  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const seq = useRef(0);

  const byUuid = useMemo(() => {
    const map = new Map<string, FleetCell>();
    for (const c of cells ?? []) map.set(c.uuid, c);
    return map;
  }, [cells]);

  // 모델 목록 — 데모 집계가 있으면 그것을, 없으면 벽면 셀에서 파생한다.
  const modelOptions = useMemo(() => {
    const fromDemo = (state?.fleet.models ?? []).map((m) => m.model).filter(Boolean);
    if (fromDemo.length > 0) return fromDemo;
    return [...new Set((cells ?? []).map((c) => c.model).filter(Boolean))].sort();
  }, [state?.fleet.models, cells]);

  // 장애 + 온도이상 uuid = 배치 후보에서 노드째 제외할 근거.
  const excludedUuids = useMemo(() => {
    const set = new Set<string>();
    for (const uuid of state?.fleet.faultUuids ?? []) set.add(uuid);
    for (const uuid of state?.fleet.tempAlertUuids ?? []) set.add(uuid);
    return set;
  }, [state?.fleet.faultUuids, state?.fleet.tempAlertUuids]);

  const chatReady = state !== null;

  function onAsk(question: Question) {
    if (!state) return;
    const answer = question.answer({ state, cells, byUuid });
    seq.current += 1;
    const operatorId = seq.current;
    seq.current += 1;
    const aiId = seq.current;
    const at = Date.now();
    setMessages((prev) => [
      ...prev,
      { id: operatorId, role: 'operator', text: question.label, at },
      { id: aiId, role: 'ai', text: answer.text, rows: answer.rows, at },
    ]);
  }

  // 첫 스냅샷 도착 시 대표 질문 1건을 자동 실행 — 시연자가 들어오자마자 빈
  // 상자가 아니라 "이미 답한 화면"을 본다(칩은 그대로 눌러 이어갈 수 있다).
  const autoAsked = useRef(false);
  // biome-ignore lint/correctness/useExhaustiveDependencies: 첫 스냅샷 1회만 — onAsk 재생성에 반응하면 중복 질의가 된다
  useEffect(() => {
    if (autoAsked.current || !state) return;
    const first = QUESTIONS[0];
    if (!first) return;
    autoAsked.current = true;
    onAsk(first);
  }, [state]);

  const phase1Body = state
    ? `통합관제 · 자동복구 · 번인 — 플릿 ${formatCount(state.fleet.gpuTotal)}장 관제 중`
    : '통합관제 · 자동복구 · 번인';

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="font-semibold text-xl">AI 분석 + 지능형 스케줄링</h1>
        <p className="text-muted-foreground text-sm">
          Phase 2·3 확장 방향을 라이브 데이터로 직접 눌러 확인한다 — 계산은 현재 플릿 기준이다.
        </p>
      </div>

      {/* 0. 질의 대상 라이브 지표 — 아래 질문·배치 계산이 읽는 그 수치다. */}
      <div
        className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5"
        aria-label="질의 대상 라이브 지표"
      >
        <KpiTile
          label="관제 중 GPU"
          icon={<Cpu className="size-4" />}
          value={state ? formatCount(state.fleet.gpuTotal) : null}
          hint={state ? `노드 ${formatCount(state.fleet.nodeTotal)}대` : undefined}
        />
        <KpiTile
          label="온도 경보"
          icon={<Thermometer className="size-4" />}
          tone="warn"
          value={state ? formatCount(state.fleet.tempAlerts) : null}
          hint="온도 경보 발행 GPU"
        />
        <KpiTile
          label="장애 GPU"
          icon={<AlertTriangle className="size-4" />}
          tone="danger"
          value={state ? formatCount(state.fleet.faulted) : null}
          hint="배치 후보에서 제외"
        />
        <KpiTile
          label="서빙 풀"
          icon={<Layers className="size-4" />}
          tone="info"
          value={state ? formatCount(state.fleet.pools.length) : null}
          hint="사용률 비교 축"
        />
        <KpiTile
          label="배치 후보 인벤토리"
          icon={<Boxes className="size-4" />}
          value={cells ? formatCount(cells.length) : null}
          hint={`제외 ${formatCount(excludedUuids.size)}장`}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 1. 대화형 AI 운영 에이전트 */}
        <Card>
          <CardHeader>
            <Badge variant="secondary" className="w-fit">
              Phase 2 · 의사결정 후 확장
            </Badge>
            <CardTitle>대화형 AI 운영 에이전트</CardTitle>
            <CardDescription>
              조회·요약 전용 읽기 에이전트 — 조치 권한 없음
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex max-h-80 min-h-40 flex-col gap-2 overflow-y-auto rounded-lg border bg-background/50 p-3">
              {messages.length === 0 ? (
                <p className="m-auto text-center text-muted-foreground text-sm">
                  {chatReady
                    ? '아래 질문을 눌러 라이브 데이터를 조회한다.'
                    : demoMode === false
                      ? '데모 인스턴스에서만 동작한다 — 실서비스 모드에서는 질문이 비활성이다.'
                      : '데모 상태를 불러오는 중이다.'}
                </p>
              ) : (
                messages.map((m) => <ChatBubble key={m.id} message={m} />)
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              {QUESTIONS.map((q) => (
                <Button
                  key={q.id}
                  variant="outline"
                  size="sm"
                  className="rounded-full"
                  disabled={!chatReady}
                  onClick={() => onAsk(q)}
                >
                  {q.label}
                </Button>
              ))}
            </div>

            <div className="flex items-center justify-between gap-2">
              <p className="text-muted-foreground text-xs">
                답변은 현재 스냅샷을 그때그때 계산한 값이다(사전 작성 문구 없음).
              </p>
              <Button
                variant="ghost"
                size="sm"
                disabled={messages.length === 0}
                onClick={() => setMessages([])}
              >
                대화 지우기
              </Button>
            </div>
          </CardContent>
        </Card>

        {/* 2. 지능형 워크로드 스케줄링 */}
        <ScoringFunnel cells={cells} models={modelOptions} excludedUuids={excludedUuids} />
      </div>

      {/* 3. 로드맵 스트립 */}
      <div className="grid gap-3 sm:grid-cols-3">
        <div
          className="flex flex-col gap-1.5 rounded-lg border bg-card p-4"
          style={{ borderTop: '3px solid var(--metric-pod)' }}
        >
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium text-sm">Phase 1 — 현재</span>
            <Badge variant="secondary" style={{ color: 'var(--metric-pod)' }}>
              적용 완료
            </Badge>
          </div>
          <span className="text-muted-foreground text-xs">{phase1Body}</span>
        </div>

        <div className="flex flex-col gap-1.5 rounded-lg border bg-card p-4">
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium text-sm">Phase 2</span>
            <Badge variant="outline">의사결정 후 확장</Badge>
          </div>
          <span className="text-muted-foreground text-xs">
            AI 운영 에이전트 — 대화형 조회·분석
          </span>
        </div>

        <div className="flex flex-col gap-1.5 rounded-lg border bg-card p-4">
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium text-sm">Phase 3</span>
            <Badge variant="outline">2027 비전</Badge>
          </div>
          <span className="text-muted-foreground text-xs">지능형 스케줄링 · 페일오버</span>
        </div>
      </div>
    </div>
  );
}
