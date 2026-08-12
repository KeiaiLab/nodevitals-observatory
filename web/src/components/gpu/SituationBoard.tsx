// SituationBoard — 통합 상황판의 뼈대. 숫자를 늘어놓는 대신 세 가지 질문에
// 답하는 순서로 배치한다(사용자 지시 2026-07-27):
//
//   ① 지금 문제가 무엇인가  ② 어디까지 영향을 미치는가  ③ 지금 무엇을 해야 하는가
//
// 관제 화면이 실패하는 전형은 "지표는 다 있는데 무엇을 해야 할지 모르겠다"이다.
// 그래서 헤드라인·영향 범위·다음 조치를 한 줄에 두고, 세부 분류(8영역)는 그
// 아래에 조밀하게 깐다.
import { ArrowRight, Radio } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import type { DemoAction, DemoState, ScenarioPhase, StatusCount } from '@/lib/demoApi';
import { formatCount, formatMs, formatPct } from '@/lib/gpuMetrics';

/** tone → 색 클래스. 라벨 문자열로 색을 추론하면 라벨이 바뀔 때 색이 깨진다. */
const TONE_TEXT: Record<string, string> = {
  ok: 'text-metric-pod',
  info: 'text-metric-cpu',
  warn: 'text-metric-thermal',
  major: 'text-metric-thermal',
  crit: 'text-metric-fault',
  muted: 'text-muted-foreground',
};

const TONE_DOT: Record<string, string> = {
  ok: 'bg-metric-pod',
  info: 'bg-metric-cpu',
  warn: 'bg-metric-thermal',
  major: 'bg-metric-thermal',
  crit: 'bg-metric-fault',
  muted: 'bg-muted-foreground',
};

/** 단계별 (문제 / 조치 / 조치 액션). 조치 액션이 없으면 "대기 중"이라 버튼을
 *  띄우지 않는다 — 누를 수 없는 버튼은 관제 화면에서 소음이다. */
const PLAYBOOK: Record<
  ScenarioPhase,
  { problem: string; detail: string; todo: string; action?: DemoAction; actionLabel?: string }
> = {
  normal: {
    problem: '진행 중인 무음 장애 없음',
    detail: '상시 이상 신호만 관찰 중 — 자동 감지가 계속 돈다.',
    todo: '조치 필요 없음',
  },
  degrading: {
    problem: '무음 장애 진행 중',
    detail: 'K8s 는 Ready 인데 GPU 가 열화 중이다 — 스케줄러는 계속 이 노드에 배치한다.',
    todo: '감지 완료까지 대기 — 곧 승인 요청이 올라온다',
  },
  'awaiting-approval': {
    problem: '격리 승인 대기',
    detail: '자동복구가 근거를 모았고 운영자 승인만 남았다.',
    todo: '격리를 승인하면 Cordon → Drain → Taint 가 순서대로 실행된다',
    action: 'approve-isolation',
    actionLabel: '격리 승인',
  },
  draining: {
    problem: '격리 진행 중 (Graceful Drain)',
    detail: '기존 요청을 흘려보내며 파드를 퇴거시키는 중 — 드롭 0건이 목표다.',
    todo: '드레인 완료 대기',
  },
  replacing: {
    problem: '노드 교체 중',
    detail: '격리된 노드에서 불량 GPU 를 교체하는 중이다.',
    todo: '교체 완료 후 번인 검증으로 넘어간다',
  },
  'burnin-1': {
    problem: '번인 검증 1차 진행 중',
    detail: '교체 GPU 를 부하로 달구며 재발 여부를 본다.',
    todo: '검증 결과 대기',
  },
  'burnin-failed': {
    problem: '번인 1차 실패 — 재검증 필요',
    detail: 'Health Score 미달로 복귀가 보류됐다. 이대로 Uncordon 하면 장애가 재발한다.',
    todo: '번인을 다시 실행해 재검증한다',
    action: 'start-burnin',
    actionLabel: '재번인 시작',
  },
  'burnin-2': {
    problem: '번인 검증 2차 진행 중',
    detail: '1차 실패 후 재검증 — 이번 결과로 복귀 여부가 갈린다.',
    todo: '검증 결과 대기',
  },
  'ready-to-return': {
    problem: '검증 통과 — 복귀 대기',
    detail: 'Health Score 가 기준을 넘었다. 운영 풀 복귀만 남았다.',
    todo: '운영 풀로 복귀시킨다',
    action: 'return-to-service',
    actionLabel: '운영 복귀',
  },
  returned: {
    problem: '복구 완료',
    detail: '노드가 운영 풀로 돌아갔다 — 다음 사이클을 관찰한다.',
    todo: '조치 필요 없음',
  },
};

function StatusStrip({ title, items }: { title: string; items: StatusCount[] }) {
  const total = items.reduce((n, i) => n + i.count, 0);
  return (
    <Card className="gap-0 py-0">
      <CardContent className="flex flex-col gap-2 p-3">
        <div className="flex items-baseline justify-between gap-2">
          <span className="text-xs font-medium text-muted-foreground">{title}</span>
          <span className="text-[10px] tabular-nums text-muted-foreground">
            합 {formatCount(total)}
          </span>
        </div>
        <div className="flex flex-wrap gap-x-3 gap-y-1.5">
          {items.map((i) => (
            <div key={i.key} className="flex min-w-[4.5rem] items-center gap-1.5">
              <span className={`size-1.5 shrink-0 rounded-full ${TONE_DOT[i.tone] ?? ''}`} />
              <span className="truncate text-[11px] text-muted-foreground">{i.label}</span>
              <span className={`ml-auto text-sm font-semibold tabular-nums ${TONE_TEXT[i.tone] ?? ''}`}>
                {formatCount(i.count)}
              </span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

export interface SituationBoardProps {
  state: DemoState;
  onAct: (action: DemoAction) => void;
  acting: boolean;
}

export default function SituationBoard({ state, onAct, acting }: SituationBoardProps) {
  const { dashboard, scenario, fleet, serving } = state;
  const play = PLAYBOOK[scenario.phase];
  const victim = scenario.victim;

  // 영향 범위 — 정상이 아닌 것들의 합. 상황판 분류가 상호배타라 그냥 더하면 된다.
  const impactedGpus = dashboard.gpus
    .filter((g) => g.key !== 'normal')
    .reduce((n, g) => n + g.count, 0);
  const impactedNodes = dashboard.nodes
    .filter((n) => n.key !== 'ready')
    .reduce((n, x) => n + x.count, 0);

  // 워크로드 영향 — victim 이 속한 서빙 풀의 현재 지연. "GPU 1장 문제"가
  // "어느 서비스의 응답이 느려지는가"로 번역되는 지점이다.
  const victimPool = serving.find((s) => s.pool === victim.pool) ?? null;
  const region = fleet.regions.find((r) => r.csp === victim.csp);

  const critical = dashboard.alerts.find((a) => a.key === 'critical')?.count ?? 0;
  const acute = scenario.phase !== 'normal' && scenario.phase !== 'returned';

  return (
    <div className="flex flex-col gap-3">
      {/* ①②③ — 한 줄에 문제·영향·조치 */}
      <Card className={acute ? 'gap-0 border-metric-fault/40 py-0' : 'gap-0 py-0'}>
        <CardContent className="grid gap-4 p-4 lg:grid-cols-[1.1fr_1.2fr_0.9fr]">
          {/* ① 지금 문제 */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-1.5">
              <Radio
                className={`size-3.5 ${acute ? 'animate-pulse text-metric-fault' : 'text-metric-pod'}`}
              />
              <span className="text-[11px] font-medium tracking-wide text-muted-foreground">
                지금 문제
              </span>
            </div>
            <p className="text-lg font-semibold leading-tight">{play.problem}</p>
            <p className="text-xs leading-relaxed text-muted-foreground">{play.detail}</p>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {dashboard.alerts.map((a) => (
                <Badge
                  key={a.key}
                  variant={a.count > 0 && a.key === 'critical' ? 'destructive' : 'outline'}
                  className="text-[10px] tabular-nums"
                >
                  {a.label} {a.count}
                </Badge>
              ))}
            </div>
          </div>

          {/* ② 어디까지 영향 */}
          <div className="flex flex-col gap-1.5 lg:border-l lg:pl-4">
            <span className="text-[11px] font-medium tracking-wide text-muted-foreground">
              영향 범위
            </span>
            <div className="flex flex-wrap items-center gap-1 text-xs">
              {[
                victim.csp,
                region?.display ?? '',
                victim.cluster,
                victim.instance,
                `${victim.device} (${victim.model})`,
              ]
                .filter(Boolean)
                .map((part, idx, arr) => (
                  <span key={part} className="flex items-center gap-1">
                    <span
                      className={
                        idx === arr.length - 1 ? 'font-semibold text-metric-fault' : 'text-muted-foreground'
                      }
                    >
                      {part}
                    </span>
                    {idx < arr.length - 1 ? (
                      <ArrowRight className="size-3 text-muted-foreground/50" />
                    ) : null}
                  </span>
                ))}
            </div>
            <dl className="mt-1 grid grid-cols-3 gap-x-3 gap-y-1 text-xs">
              <div className="flex flex-col">
                <dt className="text-[10px] text-muted-foreground">영향 GPU</dt>
                <dd className="font-semibold tabular-nums">{formatCount(impactedGpus)}장</dd>
              </div>
              <div className="flex flex-col">
                <dt className="text-[10px] text-muted-foreground">영향 노드</dt>
                <dd className="font-semibold tabular-nums">{formatCount(impactedNodes)}대</dd>
              </div>
              <div className="flex flex-col">
                <dt className="text-[10px] text-muted-foreground">테넌트</dt>
                <dd className="truncate font-semibold">{victim.pool || '미할당'}</dd>
              </div>
            </dl>
            {victimPool ? (
              <p className="text-[11px] text-muted-foreground">
                워크로드 영향 — <span className="font-medium">{victimPool.display}</span> p95{' '}
                <span
                  className={`font-semibold tabular-nums ${victimPool.saturated ? 'text-metric-fault' : ''}`}
                >
                  {formatMs(victimPool.p95Ms)}
                </span>
                {victimPool.errorRatePct > 0.05 ? (
                  <> · 에러율 {formatPct(victimPool.errorRatePct)}</>
                ) : null}
              </p>
            ) : null}
          </div>

          {/* ③ 지금 무엇을 */}
          <div className="flex flex-col gap-1.5 lg:border-l lg:pl-4">
            <span className="text-[11px] font-medium tracking-wide text-muted-foreground">
              지금 할 일
            </span>
            <p className="text-sm leading-relaxed">{play.todo}</p>
            {play.action && play.actionLabel ? (
              <Button
                size="sm"
                className="mt-1 w-full"
                disabled={acting}
                onClick={() => onAct(play.action as DemoAction)}
              >
                {acting ? '처리 중…' : play.actionLabel}
                <ArrowRight className="size-3.5" />
              </Button>
            ) : (
              <Badge variant="secondary" className="w-fit text-[10px]">
                {critical > 0 ? '미확인 Critical 알림 확인 필요' : '자동 진행 중 — 개입 불필요'}
              </Badge>
            )}
            <p className="mt-auto text-[10px] text-muted-foreground">
              복구 모드 {scenario.mode === 'observe' ? '관찰' : scenario.mode === 'approve' ? '운영자 승인' : '제한 자동'}
              {scenario.autoAdvance ? ' · 자동 전이 켜짐' : ' · 자동 전이 멈춤'}
            </p>
          </div>
        </CardContent>
      </Card>

      {/* 8영역 중 분류 4종 — 조밀한 스트립 */}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <StatusStrip title="GPU 상태" items={dashboard.gpus} />
        <StatusStrip title="노드 상태" items={dashboard.nodes} />
        <StatusStrip title="자동복구" items={dashboard.repair} />
        <StatusStrip title="수집 상태" items={dashboard.collect} />
      </div>
    </div>
  );
}
