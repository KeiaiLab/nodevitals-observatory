// GpuRemediation — Step 4 "승인 기반 격리" 자동복구 콘솔 (데모 전용 페이지).
// 복구 모드(관찰/승인/제한 자동) 서버 연동 선택(set-mode) + 승인 대기 카드(사전 검증 → Dialog 확인
// → approve-isolation 액션) + Cordon→Drain→Taint 스테퍼 + 감사 로그 +
// Maintenance Lock(폐루프 안전장치). 진행 상태는 GpuLayout 폴링(진행 장면 2s)이
// 자동 갱신하므로 승인 성공 후 별도 구독 없이 스테퍼가 살아 움직인다.
import { Activity, Check, FileClock, HeartPulse, ShieldCheck, Wrench } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router';
import KpiTile from '@/components/gpu/KpiTile';
import RemediationStepper from '@/components/gpu/RemediationStepper';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { type DemoContextValue, useDemo } from '@/hooks/demoContext';
import {
  type AuditEntry,
  type DemoState,
  type MaintenanceLock,
  PHASE_LABELS,
  type RemediationMode,
  type ScenarioPhase,
  type ScenarioState,
} from '@/lib/demoApi';

// 시나리오 단계 순서(로컬 사본) — draining 이후 여부 판정에 쓴다.
// 서버 phaseIndex 의 내부 의미에 의존하지 않는 자기완결 판정.
const PHASE_ORDER: readonly ScenarioPhase[] = [
  'normal',
  'degrading',
  'awaiting-approval',
  'draining',
  'replacing',
  'burnin-1',
  'burnin-failed',
  'burnin-2',
  'ready-to-return',
  'returned',
];

function phaseAtLeast(phase: ScenarioPhase, target: ScenarioPhase): boolean {
  return PHASE_ORDER.indexOf(phase) >= PHASE_ORDER.indexOf(target);
}

function scoreColor(score: number): string {
  if (score >= 90) return 'var(--metric-pod)';
  if (score >= 70) return 'var(--metric-thermal)';
  return 'var(--metric-fault)';
}

// ---- 복구 모드 셀렉터 (서버 연동 — scenario.mode 가 선택 상태의 진실) ----
// 항목은 서버 modeOptions 로 렌더하고, 선택 시 set-mode 액션을 보낸다. 낙관적
// 갱신은 하지 않는다 — 실패하면 다음 폴링의 서버 값으로 선택이 되돌아온다.

function ModeSelectorCard({
  scenario,
  act,
}: {
  scenario: ScenarioState;
  act: DemoContextValue['act'];
}) {
  const [pending, setPending] = useState<RemediationMode | null>(null);
  const [modeError, setModeError] = useState<string | null>(null);
  const mode = scenario.mode;
  const options = scenario.modeOptions;
  // 관찰 모드 + 승인 대기 = 자동 전이 정지(서버 autoAdvance 가 근거).
  const stalled =
    mode === 'observe' && scenario.phase === 'awaiting-approval' && scenario.autoAdvance === false;

  async function onSelect(next: string) {
    if (pending !== null || next === mode) return;
    const target = next as RemediationMode;
    setPending(target);
    setModeError(null);
    const outcome = await act('set-mode', { mode: target });
    setPending(null);
    if (outcome.ok) return;
    setModeError(
      outcome.status === 409
        ? `모드 전환 거부 — ${outcome.error}`
        : `모드 전환 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
    );
  }

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          복구 모드
          {pending ? (
            <span className="font-normal text-muted-foreground text-xs">전환 중…</span>
          ) : null}
        </CardTitle>
        <CardDescription>
          자동복구의 개입 수준을 선택한다 — 시나리오 자동 전이가 실제로 바뀐다.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {stalled ? (
          <Alert>
            <AlertTitle>관찰 모드 — 자동 조치 없음</AlertTitle>
            <AlertDescription>승인해야 진행된다.</AlertDescription>
          </Alert>
        ) : null}
        {modeError ? (
          <Alert variant="destructive">
            <AlertTitle>모드 전환 실패</AlertTitle>
            <AlertDescription>{modeError}</AlertDescription>
          </Alert>
        ) : null}
        {options.length === 0 ? (
          <p className="text-muted-foreground text-sm">모드 목록을 불러오는 중이다.</p>
        ) : (
          <RadioGroup
            value={mode}
            onValueChange={onSelect}
            disabled={pending !== null}
            className="flex flex-col gap-3"
          >
            {options.map((opt) => (
              <div key={opt.id} className="flex items-start gap-3">
                <RadioGroupItem value={opt.id} id={`mode-${opt.id}`} className="mt-0.5" />
                <div className="flex flex-col gap-0.5">
                  <Label htmlFor={`mode-${opt.id}`} className="flex items-center gap-1.5">
                    {opt.label}
                    {opt.id === 'approve' ? (
                      <Badge variant="secondary" className="text-[10px]">
                        권장
                      </Badge>
                    ) : null}
                  </Label>
                  <span className="text-muted-foreground text-xs">{opt.describe}</span>
                </div>
              </div>
            ))}
          </RadioGroup>
        )}
      </CardContent>
      <CardFooter className="flex-col items-start gap-2">
        <Separator />
        <p className="text-muted-foreground text-xs">
          초기 상용은 승인 기반 반자동 권장 — 오탐 비용이 큰 환경에서 사람의 최종 게이트를
          유지한다.
        </p>
      </CardFooter>
    </Card>
  );
}

// ---- 승인 대기 카드 (사전 검증 + Dialog 확인 + approve-isolation) ----

function PassCell({ text }: { text: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm">
      {text}
      <Check aria-label="통과" className="size-3.5" style={{ color: 'var(--metric-pod)' }} />
    </span>
  );
}

function ApprovalCard({
  scenario,
  act,
}: {
  scenario: ScenarioState;
  act: DemoContextValue['act'];
}) {
  const { victim, phase } = scenario;
  // 승인 가능 여부는 *서버가* 판정한다 — pendingAction 은 서버 Apply 가드와 같은
  // 단계 집합에서 나오는 값이다(state.go pendingActionFor ↔ scenario.go 의
  // approve-isolation 가드, 둘 다 degrading/awaiting-approval). 여기에 단계
  // 목록을 다시 쓰면 서버가 규칙을 바꾼 날 화면만 거짓말한다.
  //
  // 이 값으로 버튼을 *미리* 막는다: 막지 않으면 시연 시작 단계(normal)에서
  // 클라이맥스 버튼이 빨간 409 거부만 돌려준다. 눌러서 거부당하는 버튼보다
  // "왜 아직 못 누르는지 + 어떻게 열리는지" 가 시연에 쓸모 있다.
  const canApprove = scenario.pendingAction === 'approve-isolation';
  const hot = canApprove;
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [jumping, setJumping] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const score = Math.round(victim.health.score);

  // 시연 제어 — 서버 jump-phase 로 승인 게이트가 열린 단계까지 당긴다. 이게
  // 없으면 부팅 직후 normal(기본 4분)이 끝날 때까지 이 화면의 유일한 조치를
  // 누를 수 없다 — 시연에서 클라이맥스를 4분 기다릴 수는 없다.
  async function jumpToApproval() {
    setJumping(true);
    const outcome = await act('jump-phase', { phase: 'awaiting-approval' });
    setJumping(false);
    setActionError(outcome.ok ? null : `단계 이동 실패 (HTTP ${outcome.status}) — ${outcome.error}`);
  }

  async function approve() {
    setBusy(true);
    const outcome = await act('approve-isolation');
    setBusy(false);
    setConfirmOpen(false);
    if (outcome.ok) {
      setActionError(null);
      return;
    }
    setActionError(
      outcome.status === 409
        ? `현 단계에서는 승인 불가 — ${outcome.error}`
        : `승인 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
    );
  }

  return (
    <Card
      className="h-full"
      style={hot ? { borderTop: '4px solid var(--metric-fault)' } : undefined}
    >
      <CardHeader>
        <CardTitle>격리 승인 대기</CardTitle>
        <CardDescription>
          {canApprove
            ? '무음 열화가 확인된 GPU 다 — 사전 검증을 확인하고 격리를 승인한다.'
            : `현재 단계(${PHASE_LABELS[phaseOf(phase)]})에는 승인할 조치가 없다 — 열화가 확인되면 승인 게이트가 열린다.`}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline" className="font-mono">
            {victim.instance}
          </Badge>
          <Badge variant="outline" className="font-mono">
            {victim.device}
          </Badge>
          <Badge variant="secondary">{victim.model}</Badge>
          <Badge variant="secondary">{victim.pool}</Badge>
        </div>
        <div className="flex items-baseline gap-2">
          <span
            className="font-bold text-4xl tabular-nums leading-none"
            style={{ color: scoreColor(score) }}
          >
            {score}
          </span>
          <span className="text-muted-foreground text-sm">Health Score</span>
        </div>
        {victim.health.deductions.length > 0 ? (
          <ul className="flex flex-col gap-1.5">
            {victim.health.deductions.map((d) => (
              <li key={d.code} className="flex items-start gap-2 text-sm">
                <Badge variant="outline" className="shrink-0 font-mono text-[10px]">
                  {d.code}
                </Badge>
                <span className="flex-1 leading-snug">{d.message}</span>
                <span
                  className="shrink-0 font-medium tabular-nums"
                  style={{ color: 'var(--metric-fault)' }}
                >
                  -{d.penalty}
                </span>
              </li>
            ))}
          </ul>
        ) : null}
        <div className="flex flex-col gap-1.5">
          <span className="font-medium text-muted-foreground text-xs">사전 검증</span>
          <Table>
            <TableBody>
              <TableRow>
                <TableCell className="w-32 text-muted-foreground text-xs">대체 GPU 용량</TableCell>
                <TableCell>
                  <PassCell text={`동일 풀(${victim.pool}) 예비 12장 확보`} />
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell className="w-32 text-muted-foreground text-xs">PDB 영향</TableCell>
                <TableCell>
                  <PassCell text="최소 가용 파드 위반 없음" />
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
        {actionError ? (
          <Alert variant="destructive">
            <AlertTitle>승인 거부</AlertTitle>
            <AlertDescription>{actionError}</AlertDescription>
          </Alert>
        ) : null}
        <Button
          size="lg"
          className="w-full"
          disabled={busy || !canApprove}
          onClick={() => setConfirmOpen(true)}
        >
          격리 승인 (Approve)
        </Button>
        {canApprove ? null : (
          <Button variant="outline" disabled={jumping} onClick={jumpToApproval}>
            {jumping ? '이동 중…' : '승인 대기 단계로 이동 (시연 제어)'}
          </Button>
        )}
        <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>격리를 승인할까</DialogTitle>
              <DialogDescription>
                Cordon → Graceful Drain → Taint 를 실행합니다. in-flight 추론 요청은 보호됩니다.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" disabled={busy} onClick={() => setConfirmOpen(false)}>
                취소
              </Button>
              <Button disabled={busy} onClick={approve}>
                {busy ? '실행 중…' : '승인 실행'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}

// ---- draining 이후: 승인 카드 대체 안내 ----

function IsolationDoneCard() {
  return (
    <Card className="h-full" style={{ borderTop: '4px solid var(--metric-pod)' }}>
      <CardHeader>
        <CardTitle>격리 완료 — 검증·번인 단계로</CardTitle>
        <CardDescription>
          Cordon → Graceful Drain → Taint 파이프라인이 실행되었다. 교체 GPU 는 번인 검증을 통과한
          뒤에만 운영 풀로 복귀한다.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <span className="font-medium text-muted-foreground text-xs">이후 절차</span>
          {[
            ['교체 GPU 투입', '동일 풀 예비 자원으로 용량 회복'],
            ['번인 검증 1차', '드라이버·CUDA·NCCL 사전검증 후 부하 시험'],
            ['가용성 판정', '기준 미달이면 재시험 — 통과분만 승급'],
            ['운영 복귀', '판정 통과 후에만 서빙 풀로 재투입'],
          ].map(([title, desc], index) => (
            <div key={title} className="flex items-start gap-2.5">
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full border bg-muted font-semibold text-[11px] text-muted-foreground">
                {index + 1}
              </span>
              <span className="flex flex-col leading-snug">
                <span className="font-medium text-sm">{title}</span>
                <span className="text-[11px] text-muted-foreground">{desc}</span>
              </span>
            </div>
          ))}
        </div>
        <Button asChild className="w-fit">
          <Link to="/gpu/validation">검증 콘솔로 이동</Link>
        </Button>
      </CardContent>
    </Card>
  );
}

// ---- Maintenance Lock — CSP 관제 신호 상관의 최소 표면 ----
// 대상 노드는 서버가 실제 플릿에서 고른 개체다(하드코딩 예시 노드명은 화면
// 간 정합을 깨뜨린다 — 다른 목록에 없는 노드가 여기서만 등장하면 안 된다).

function MaintenanceLockCard({ locks }: { locks: MaintenanceLock[] }) {
  return (
    <Card className="h-full" style={{ borderTop: '4px solid var(--metric-cpu)' }}>
      <CardHeader>
        <CardTitle>Maintenance Lock</CardTitle>
        <CardDescription>CSP 관제 신호 상관 — 폐루프 안전장치</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm leading-relaxed">
          CSP 정비 예정 신호 수신 노드는 자동복구를 트리거하지 않는다 — 폐루프 안전장치. 정비
          창에서의 자체 격리는 CSP 교체 절차와 충돌해 이중 드레인을 일으킨다.
        </p>
        {locks.length === 0 ? (
          <p className="text-muted-foreground text-xs">현재 정비 예정 신호를 받은 노드가 없다.</p>
        ) : (
          locks.map((lock) => (
            <div
              key={lock.instance}
              className="flex flex-col gap-1 rounded-md border bg-muted/40 px-3 py-2"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs">{lock.instance}</span>
                <Badge
                  variant="outline"
                  style={{ borderColor: 'var(--metric-cpu)', color: 'var(--metric-cpu)' }}
                >
                  Maintenance Lock
                </Badge>
              </div>
              <span className="text-muted-foreground text-[11px]">
                {lock.csp} · {lock.window} · {lock.reason}
              </span>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// ---- 감사 로그 ----

function AuditCard({ audit }: { audit: AuditEntry[] }) {
  const rows = [...audit].sort((a, b) => b.at - a.at).slice(0, 20);
  return (
    <Card>
      <CardHeader>
        <CardTitle>감사 로그</CardTitle>
        <CardDescription>최근 {rows.length}건 — 모든 조치는 수행자와 함께 기록된다.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-28">시각</TableHead>
              <TableHead className="w-28">수행자</TableHead>
              <TableHead>액션</TableHead>
              <TableHead>대상</TableHead>
              <TableHead>결과</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  감사 기록이 없다.
                </TableCell>
              </TableRow>
            ) : (
              rows.map((entry, index) => (
                <TableRow key={`${entry.at}-${entry.action}-${index}`}>
                  <TableCell className="text-xs tabular-nums">
                    {new Date(entry.at).toLocaleTimeString('ko-KR')}
                  </TableCell>
                  <TableCell>
                    {entry.actor === 'auto' ? (
                      <Badge variant="secondary">자동</Badge>
                    ) : (
                      <span className="text-xs">{entry.actor}</span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{entry.action}</TableCell>
                  <TableCell className="font-mono text-xs">{entry.target ?? '—'}</TableCell>
                  <TableCell className="text-xs">{entry.result}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

// ---- KPI 타일 행 (목업 demo1/demo4 의 상단 6타일 어휘) ----

function RemediationKpis({ state }: { state: DemoState }) {
  const { scenario, audit } = state;
  const { drain, health } = scenario.victim;
  const score = Math.round(health.score);
  const modeLabel =
    scenario.modeOptions.find((o) => o.id === scenario.mode)?.label ?? scenario.mode;
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6" aria-label="자동복구 핵심 지표">
      <KpiTile
        label="복구 모드"
        icon={<ShieldCheck className="size-4" />}
        tone="info"
        value={modeLabel}
        hint={scenario.autoAdvance ? '자동 전이 진행' : '승인 대기 — 자동 전이 정지'}
      />
      <KpiTile
        label="승인 대기"
        icon={<Activity className="size-4" />}
        tone={scenario.pendingAction ? 'warn' : 'default'}
        value={scenario.pendingAction ? '1건' : '0건'}
        hint={`현재 단계 — ${PHASE_LABELS[phaseOf(scenario.phase)]}`}
        progress={((scenario.phaseIndex + 1) / PHASE_ORDER.length) * 100}
      />
      <KpiTile
        label="Health Score"
        icon={<HeartPulse className="size-4" />}
        tone={score >= 90 ? 'ok' : score >= 70 ? 'warn' : 'danger'}
        value={String(score)}
        sub="/ 100"
        hint={`감점 ${health.deductions.length}건`}
        progress={score}
      />
      <KpiTile
        label="격리 진행"
        icon={<Wrench className="size-4" />}
        tone="info"
        value={`${Math.round(drain.progress)}%`}
        hint={`파드 퇴거 ${drain.podsEvicted}/${drain.podsTotal}`}
        progress={drain.progress}
      />
      <KpiTile
        label="in-flight 보호"
        icon={<Check className="size-4" />}
        tone={drain.droppedRequests === 0 ? 'ok' : 'danger'}
        value={drain.droppedRequests === 0 ? '0건' : `${drain.droppedRequests}건`}
        hint="드롭된 추론 요청"
      />
      <KpiTile
        label="감사 기록"
        icon={<FileClock className="size-4" />}
        value={`${audit.length}건`}
        hint={`Maintenance Lock ${scenario.maintenanceLocks.length}건`}
      />
    </div>
  );
}

// PHASE_LABELS 키 방어 — 서버가 새 단계를 보내도 화면이 깨지지 않게 한다.
function phaseOf(phase: ScenarioPhase): ScenarioPhase {
  return PHASE_LABELS[phase] ? phase : 'normal';
}

// ---- 페이지 ----

function RemediationView({ state, act }: { state: DemoState; act: DemoContextValue['act'] }) {
  const { scenario, audit } = state;
  const phase = scenario.phase;
  const isolationDone = phaseAtLeast(phase, 'draining');
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="font-semibold text-lg">자동복구 콘솔 — 승인 기반 격리</h1>
        <Badge variant={phase === 'normal' ? 'secondary' : 'default'}>{PHASE_LABELS[phase]}</Badge>
      </div>
      <RemediationKpis state={state} />
      <div className="grid gap-4 lg:grid-cols-2">
        {isolationDone ? (
          <IsolationDoneCard />
        ) : (
          <ApprovalCard scenario={scenario} act={act} />
        )}
        <Card className="h-full">
          <CardHeader>
            <CardTitle>격리 파이프라인</CardTitle>
            <CardDescription>Cordon → Graceful Drain → Taint — in-flight 요청 보호</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <RemediationStepper drain={scenario.victim.drain} />
            <Separator />
            <div className="flex flex-col gap-2">
              <span className="font-medium text-muted-foreground text-xs">파이프라인 보장</span>
              {[
                'PDB(최소 가용 파드) 를 위반하지 않는다',
                'in-flight 추론 요청은 완료까지 대기한다 — 드롭 0건',
                '되돌리기는 uncordon 1회 — 격리는 가역 조치다',
              ].map((line) => (
                <span key={line} className="flex items-start gap-2 text-sm">
                  <Check
                    aria-hidden
                    className="mt-0.5 size-3.5 shrink-0"
                    style={{ color: 'var(--metric-pod)' }}
                  />
                  <span className="leading-snug">{line}</span>
                </span>
              ))}
            </div>
          </CardContent>
        </Card>
        <ModeSelectorCard scenario={scenario} act={act} />
        <MaintenanceLockCard locks={scenario.maintenanceLocks} />
        <div className="lg:col-span-2">
          <AuditCard audit={audit} />
        </div>
      </div>
    </div>
  );
}

function RemediationSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-8 w-72" />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {[0, 1, 2, 3, 4, 5].map((i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72" />
        <Skeleton className="h-72" />
      </div>
    </div>
  );
}

export default function GpuRemediation() {
  const { demoMode, state, act } = useDemo();
  if (demoMode === null) return <RemediationSkeleton />;
  if (demoMode === false) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Card className="max-w-md">
          <CardHeader>
            <CardTitle>자동복구 콘솔</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-sm leading-relaxed">
              자동복구 콘솔은 데모 인스턴스 전용입니다. 실서비스 격리 파이프라인이 연결되면 이
              화면에서 승인 기반 복구를 운영한다.
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }
  if (!state) return <RemediationSkeleton />;
  return <RemediationView state={state} act={act} />;
}
