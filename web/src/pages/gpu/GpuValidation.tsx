// GpuValidation — Step 5 "사전 검증·번인" (데모 전용 — demo off 는 안내 카드만).
// 서사: 노드 교체 후 번인 1차 실패(Health 75, "재검증 필요 (Uncordon 보류)") →
// 재번인 → 96 통과 → 재투입. "통과시키지 않는 장면"(관문 차단)이 핵심.
// 차트는 Explorer 의 CSP-안전 Recharts 셋업을 그대로 미러한다(isAnimationActive
// false / contentStyle var(--popover) / step=max(15, floor(range/500))).
import { useMemo, useState } from 'react';
import { Circle, CircleCheck, TriangleAlert } from 'lucide-react';
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import GateDiagram, { type GateStage } from '@/components/gpu/GateDiagram';
import HealthGauge from '@/components/gpu/HealthGauge';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Slider } from '@/components/ui/slider';
import { useDemo } from '@/hooks/demoContext';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api } from '@/lib/api';
import { type CheckItem, PHASE_LABELS, type ScenarioPhase } from '@/lib/demoApi';
import { METRIC_GPU_UTIL, selector } from '@/lib/gpuMetrics';

// 번인 프로파일 옵션 — B200 은 실장비 부재로 「시뮬레이션」 표기.
const BURNIN_PROFILES = [
  { value: 'High-Intensity-CUDA-v1', label: 'High-Intensity-CUDA-v1', simulated: false },
  { value: 'B200-Blackwell-v1', label: 'B200-Blackwell-v1', simulated: true },
] as const;

// 번인 부하 곡선 — 최근 30분, 15s 폴링 (Explorer step 규약 미러).
const CURVE_RANGE_SECONDS = 30 * 60;
const CURVE_POLL_MS = 15_000;
const MIN_STEP_SECONDS = 15;
const TARGET_POINTS = 500;

/** phase → 관문 다이어그램 3단 상태 매핑.
 *  burnin-1/2·burnin-failed = 관문 active + 운영풀 blocked / ready-to-return =
 *  관문 done / returned = 전부 done. 명세 외 단계는 보수적 가정: 교체 진행
 *  (draining·replacing)은 K8s Ready 대기 + 풀 차단, 그 이전 단계는 전부 대기. */
function gateStages(phase: ScenarioPhase): GateStage[] {
  const make = (
    k8s: GateStage['status'],
    gate: GateStage['status'],
    pool: GateStage['status'],
  ): GateStage[] => [
    { id: 'k8s-ready', label: 'Kubernetes Ready', status: k8s },
    { id: 'gate', label: '사전 검증 관문', status: gate },
    { id: 'pool', label: '운영 풀', status: pool },
  ];
  switch (phase) {
    case 'burnin-1':
    case 'burnin-failed':
    case 'burnin-2':
      return make('done', 'active', 'blocked');
    case 'ready-to-return':
      return make('done', 'done', 'idle');
    case 'returned':
      return make('done', 'done', 'done');
    case 'draining':
    case 'replacing':
      return make('active', 'idle', 'blocked');
    default:
      return make('idle', 'idle', 'idle');
  }
}

function StatusIcon({ status }: { status: CheckItem['status'] }) {
  if (status === 'pass') {
    return <CircleCheck className="size-4 shrink-0" style={{ color: 'var(--metric-pod)' }} />;
  }
  if (status === 'warn') {
    return <TriangleAlert className="size-4 shrink-0" style={{ color: 'var(--metric-thermal)' }} />;
  }
  if (status === 'running') {
    return (
      <span className="flex size-4 shrink-0 items-center justify-center">
        <span className="size-2.5 animate-pulse rounded-full bg-primary" />
      </span>
    );
  }
  return <Circle className="size-4 shrink-0 text-muted-foreground/50" />;
}

const CHECK_STATUS_TEXT: Record<CheckItem['status'], string> = {
  pass: '통과',
  warn: '경고',
  running: '진행 중',
  pending: '대기',
};

function formatTimeTick(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
  });
}

export default function GpuValidation() {
  const { demoMode, state, act } = useDemo();

  // 폼 로컬 상태 — 지속시간·목표 사용률은 시연용 표시 전용(서버 계약에 없음).
  const [profileChoice, setProfileChoice] = useState<string | null>(null);
  const [durationMin, setDurationMin] = useState(120);
  const [targetUtil, setTargetUtil] = useState(95);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  // victim util 곡선 — uuid 미확정(판별 중/demo off)이면 질의 0.
  const victimUuid = state?.scenario.victim.uuid ?? '';
  const curveQuery = usePolledQuery(async () => {
    if (!victimUuid) return null;
    const endSec = Math.floor(Date.now() / 1000);
    const startSec = endSec - CURVE_RANGE_SECONDS;
    const step = Math.max(MIN_STEP_SECONDS, Math.floor(CURVE_RANGE_SECONDS / TARGET_POINTS));
    const res = await api.rangeQuery(
      selector(METRIC_GPU_UTIL, { gpu_uuid: victimUuid }),
      startSec,
      endSec,
      step,
    );
    return res.data;
  }, CURVE_POLL_MS, [victimUuid]);

  const curveRows = useMemo(() => {
    const series = curveQuery.data?.result[0];
    if (!series) return [];
    return series.values.map(([t, v]) => ({ time: t, util: Number(v) }));
  }, [curveQuery.data]);

  if (demoMode === null) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!demoMode) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>사전 검증·번인</CardTitle>
          <CardDescription>데모 시나리오 전용 화면</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-sm">
            이 화면은 데모 모드에서만 동작한다. 노드 교체 후 번인 검증 시나리오
            (1차 실패 → 재번인 → 통과 → 운영 풀 재투입)를 시연하며, 실서비스
            모드에서는 제공되지 않는다.
          </p>
        </CardContent>
      </Card>
    );
  }

  if (!state) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const phase = state.scenario.phase;
  const victim = state.scenario.victim;
  const burnin = victim.burnin;

  // 서버가 알려준 프로파일이 옵션 밖이면 옵션에 합류시킨다(표시 누락 방지).
  const profileOptions = BURNIN_PROFILES.some((p) => p.value === burnin.profile)
    ? [...BURNIN_PROFILES]
    : burnin.profile
      ? [{ value: burnin.profile, label: burnin.profile, simulated: false }, ...BURNIN_PROFILES]
      : [...BURNIN_PROFILES];
  const effectiveProfile = profileChoice ?? burnin.profile ?? profileOptions[0].value;

  async function onStartBurnin() {
    setBusy(true);
    setActionError(null);
    const outcome = await act('start-burnin');
    setBusy(false);
    if (!outcome.ok) {
      setActionError(
        outcome.status === 409
          ? `재번인은 1차 실패 후 가능 — 현재 단계: ${PHASE_LABELS[phase]}`
          : outcome.error,
      );
    }
  }

  async function onReturnToService() {
    setBusy(true);
    setActionError(null);
    const outcome = await act('return-to-service');
    setBusy(false);
    if (!outcome.ok) {
      setActionError(
        outcome.status === 409
          ? `재투입은 검증 통과 후 가능 — 현재 단계: ${PHASE_LABELS[phase]}`
          : outcome.error,
      );
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="font-semibold text-xl">사전 검증·번인</h1>
        <Badge variant="outline">{PHASE_LABELS[phase]}</Badge>
      </div>

      {actionError ? (
        <Alert variant="destructive">
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      {/* 1. 검증 관문 다이어그램 */}
      <Card>
        <CardHeader>
          <CardTitle>검증 관문</CardTitle>
          <CardDescription>노드 교체 후 재투입 전 관문 상태</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <GateDiagram stages={gateStages(phase)} />
          <p className="text-muted-foreground text-xs">
            검증 전 운영 투입 원천 차단 — K8s Ready 를 맹신하지 않는다
          </p>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 2. 번인 프로파일 폼 */}
        <Card>
          <CardHeader>
            <CardTitle>번인 프로파일</CardTitle>
            <CardDescription>고부하 스트레스 검증 실행 설정</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <span className="text-muted-foreground text-xs">프로파일</span>
              <Select
                value={effectiveProfile}
                onValueChange={setProfileChoice}
                disabled={burnin.active}
              >
                <SelectTrigger className="w-full" aria-label="번인 프로파일">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {profileOptions.map((p) => (
                    <SelectItem key={p.value} value={p.value}>
                      <span className="flex items-center gap-2">
                        {p.label}
                        {p.simulated ? <Badge variant="secondary">시뮬레이션</Badge> : null}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="text-muted-foreground text-xs">
                지속시간 — <span className="font-medium text-foreground">{durationMin}분</span>
              </span>
              <Slider
                min={30}
                max={180}
                step={5}
                value={[durationMin]}
                onValueChange={(v) => setDurationMin(v[0] ?? 120)}
                disabled={burnin.active}
                aria-label="번인 지속시간(분)"
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="text-muted-foreground text-xs">
                목표 사용률 — <span className="font-medium text-foreground">{targetUtil}%</span>
              </span>
              <Slider
                min={80}
                max={100}
                step={1}
                value={[targetUtil]}
                onValueChange={(v) => setTargetUtil(v[0] ?? 95)}
                disabled={burnin.active}
                aria-label="번인 목표 사용률(%)"
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="text-muted-foreground text-xs">대상 (교체 노드 고정)</span>
              <div className="rounded-md border bg-muted/40 px-3 py-2 font-mono text-sm">
                {victim.instance} · {victim.device} · {victim.model}
              </div>
            </div>

            {burnin.active ? (
              <div className="flex flex-col gap-1.5">
                <span className="text-muted-foreground text-xs">
                  번인 진행 중 — {Math.round(burnin.progress)}%
                </span>
                <Progress value={burnin.progress} />
              </div>
            ) : null}

            <Button onClick={onStartBurnin} disabled={busy || burnin.active}>
              번인 시작
            </Button>
          </CardContent>
        </Card>

        {/* 3. 체크리스트 7종 */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              번인 체크리스트
              {burnin.attempt > 0 ? (
                <Badge variant="outline">{burnin.attempt}차 시도</Badge>
              ) : null}
            </CardTitle>
            <CardDescription>스트레스 항목별 판정</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {burnin.checklist.length === 0 ? (
              <p className="py-6 text-center text-muted-foreground text-sm">
                번인 시작 전 — 체크 항목이 준비되면 표시된다.
              </p>
            ) : (
              burnin.checklist.map((item) => (
                <div key={item.id} className="flex items-center gap-2 rounded-md border px-3 py-2">
                  <StatusIcon status={item.status} />
                  <span className="text-sm">{item.label}</span>
                  <span className="ml-auto text-muted-foreground text-xs">
                    {CHECK_STATUS_TEXT[item.status]}
                  </span>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* 4. Health 게이지 + 판정 근거 */}
        <Card>
          <CardHeader>
            <CardTitle>Health 판정</CardTitle>
            <CardDescription>100점 만점 — 85점 이상 통과</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center justify-center gap-6">
            <HealthGauge score={victim.health.score} verdict={burnin.verdict} />
            <div className="flex min-w-52 flex-1 flex-col gap-2">
              <span className="text-muted-foreground text-xs">판정 근거 (감점 항목)</span>
              {victim.health.deductions.length === 0 ? (
                <p className="text-muted-foreground text-sm">감점 항목 없음</p>
              ) : (
                victim.health.deductions.map((d) => (
                  <div
                    key={d.code}
                    className="flex items-start justify-between gap-2 rounded-md border px-3 py-2"
                  >
                    <div className="flex flex-col">
                      <span className="text-sm">{d.message}</span>
                      <span className="font-mono text-muted-foreground text-xs">{d.code}</span>
                    </div>
                    <span
                      className="font-semibold text-sm tabular-nums"
                      style={{ color: 'var(--metric-fault)' }}
                    >
                      -{d.penalty}
                    </span>
                  </div>
                ))
              )}
            </div>
          </CardContent>
        </Card>

        {/* 5. 번인 부하 곡선 */}
        <Card>
          <CardHeader>
            <CardTitle>번인 부하 곡선</CardTitle>
            <CardDescription>
              {victim.instance} · {victim.device} 사용률 — 최근 30분 · 15초 갱신
            </CardDescription>
          </CardHeader>
          <CardContent>
            {curveQuery.data === null && curveQuery.error === null ? (
              <Skeleton className="h-60 w-full" />
            ) : curveRows.length === 0 ? (
              <p className="py-8 text-center text-muted-foreground text-sm">
                표시할 데이터가 없다.
              </p>
            ) : (
              <ResponsiveContainer width="100%" height={240}>
                <LineChart data={curveRows} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                  <XAxis
                    dataKey="time"
                    type="number"
                    domain={['dataMin', 'dataMax']}
                    tickFormatter={formatTimeTick}
                    tick={{ fontSize: 12 }}
                    className="fill-muted-foreground"
                  />
                  <YAxis
                    domain={[0, 100]}
                    width={40}
                    tick={{ fontSize: 12 }}
                    className="fill-muted-foreground"
                  />
                  <Tooltip
                    labelFormatter={(t) => formatTimeTick(Number(t))}
                    formatter={(value) => [`${Number(value).toFixed(1)}%`, '사용률']}
                    contentStyle={{
                      backgroundColor: 'var(--popover)',
                      borderColor: 'var(--border)',
                      color: 'var(--popover-foreground)',
                      fontSize: 12,
                    }}
                  />
                  <ReferenceLine
                    y={95}
                    stroke="var(--metric-thermal)"
                    strokeDasharray="4 4"
                    label={{
                      value: '목표 95%',
                      position: 'insideTopRight',
                      fill: 'var(--muted-foreground)',
                      fontSize: 11,
                    }}
                  />
                  <Line
                    type="monotone"
                    dataKey="util"
                    name="사용률"
                    stroke="var(--metric-gpu)"
                    strokeWidth={2}
                    dot={false}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      {/* 6. 재투입 CTA (ready-to-return) / 복귀 완료 안내 (returned) */}
      {phase === 'ready-to-return' ? (
        <Card style={{ borderColor: 'var(--metric-pod)' }}>
          <CardContent className="flex flex-col items-center gap-3 py-6">
            <p className="text-muted-foreground text-sm">
              Health {Math.round(victim.health.score)}점 — 검증 관문 통과. 운영 풀 재투입이
              가능하다.
            </p>
            <Button
              size="lg"
              className="h-12 w-full max-w-md text-base"
              onClick={onReturnToService}
              disabled={busy}
            >
              운영 풀 재투입
            </Button>
          </CardContent>
        </Card>
      ) : null}
      {phase === 'returned' ? (
        <Alert>
          <CircleCheck className="size-4" style={{ color: 'var(--metric-pod)' }} />
          <AlertTitle>운영 풀 복귀 완료</AlertTitle>
          <AlertDescription>
            검증을 통과한 노드가 운영 풀에 재투입됐다 — Uncordon 완료.
          </AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}
