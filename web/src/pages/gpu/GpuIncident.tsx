// GpuIncident — 장애 상세 / 자동격리 대응 (목업 demo2).
//
// 시나리오 3 [장애 파악 및 조치] 의 화면. 한 장에 세 가지 질문의 답을 놓는다:
//   ① 무엇이 났나 — 상단 장애 헤더(심각도·발생 시각·지속 시간·진단 요약)
//   ② 어디까지 번졌나 — 「원인 및 영향 경로」 가로 7-노드 + 「영향 범위 요약」
//   ③ 무엇부터 하나 — 우측 「권장 조치 / 다음 액션」 6단계 + 실행 버튼
//
// 조치 목록은 새 상태가 아니라 *시나리오 단계의 투영*이고(서버 incident.actions),
// 버튼은 기존 액션(approve-isolation / start-burnin / return-to-service)을 그대로
// 쏜다 — 화면 전용 상태를 만들면 파이프라인과 따로 놀기 시작한다.
import {
  AlertTriangle,
  Box,
  Boxes,
  CheckCircle2,
  Cpu,
  ExternalLink,
  RefreshCw,
  ShieldCheck,
  Users,
} from 'lucide-react';
import { type ReactNode, useEffect, useState } from 'react';
import { Link } from 'react-router';
import AlertTimeline from '@/components/gpu/AlertTimeline';
import IncidentActionList from '@/components/gpu/IncidentActionList';
import IncidentFlow from '@/components/gpu/IncidentFlow';
import VictimMetricChart from '@/components/gpu/VictimMetricChart';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemo } from '@/hooks/demoContext';
import { useVictimSeries } from '@/hooks/useVictimSeries';
import type { DemoAction, DemoState, IncidentChain, IncidentImpact } from '@/lib/demoApi';
import { formatCelsius, formatCount, formatPct, METRIC_COLORS } from '@/lib/gpuMetrics';

/** epoch ms 이후 경과를 "17분 53초"로 — 목업 지속 시간 표기. 0(장애 없음)이면 '—'. */
function elapsedText(fromMs: number, nowMs: number): string {
  if (fromMs <= 0) return '—';
  const total = Math.max(0, Math.floor((nowMs - fromMs) / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}시간 ${m}분 ${s}초`;
  return `${m}분 ${s}초`;
}

/** epoch ms → "2026-07-29 16:18:13" — 목업의 발생 시각 표기(24시간제, 대시 구분).
 *  ko-KR 기본 포맷("2026. 7. 29. 오후 4:18:13")은 좁은 칸에서 두 줄로 접힌다. */
function timeText(ms: number): string {
  if (ms <= 0) return '—';
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** epoch ms → "14:32:18" — 목업 우상단 「마지막 업데이트」 표기(24시간제). */
function clockText(ms: number): string {
  return ms > 0 ? new Date(ms).toLocaleTimeString('ko-KR', { hour12: false }) : '—';
}

const SEVERITY_LABEL: Record<string, string> = {
  critical: 'Critical',
  major: 'Major',
  warning: 'Warning',
  info: '정보',
};

const SEVERITY_COLOR: Record<string, string> = {
  critical: METRIC_COLORS.fault,
  major: METRIC_COLORS.thermal,
  warning: METRIC_COLORS.mem,
  info: METRIC_COLORS.cpu,
};

// 주 CTA — 단계에 따라 라벨과 액션이 바뀐다. 버튼 여러 개가 *같은* 액션을 쏘면
// 거짓 표면이 되므로(엔진에서 Cordon→Drain→Taint 는 한 액션이다) 실행 버튼은
// 하나로 두고, 나머지는 해당 콘솔로 가는 링크다.
const CTA: Record<string, { label: string; action: DemoAction }> = {
  'approve-isolation': { label: '노드 격리 실행', action: 'approve-isolation' },
  'start-burnin': { label: '재번인 시작', action: 'start-burnin' },
  'return-to-service': { label: '운영 풀 재투입', action: 'return-to-service' },
};

// ---- 헤더 ----

function IncidentHeader({ incident, now }: { incident: IncidentChain; now: number }) {
  const color = SEVERITY_COLOR[incident.severity] ?? METRIC_COLORS.cpu;
  return (
    <Card className={incident.active ? 'gap-0 border-metric-fault/40 py-0' : 'gap-0 py-0'}>
      <CardContent className="flex flex-col gap-3 p-4">
        <div className="flex flex-wrap items-center gap-3">
          <Badge
            variant="outline"
            style={{
              color,
              borderColor: color,
              backgroundColor: `color-mix(in srgb, ${color} 12%, transparent)`,
            }}
          >
            {SEVERITY_LABEL[incident.severity] ?? incident.severity}
          </Badge>
          <p className="min-w-0 flex-1 font-semibold text-lg leading-tight">{incident.title}</p>
          <Badge variant="outline" className="font-mono text-[10px]">
            {incident.code}
          </Badge>
        </div>

        <dl className="flex flex-wrap items-center gap-x-8 gap-y-2 text-sm">
          <div className="flex items-center gap-2">
            <dt className="text-muted-foreground text-xs">발생 시각</dt>
            <dd className="tabular-nums">{timeText(incident.firstAt || incident.at)}</dd>
          </div>
          <div className="flex items-center gap-2">
            <dt className="text-muted-foreground text-xs">상태</dt>
            <dd className="flex items-center gap-1.5">
              {incident.active ? (
                <>
                  <AlertTriangle className="size-3.5" style={{ color: METRIC_COLORS.fault }} />
                  <span style={{ color: METRIC_COLORS.fault }}>진행 중</span>
                </>
              ) : (
                <>
                  <CheckCircle2 className="size-3.5" style={{ color: METRIC_COLORS.pod }} />
                  <span style={{ color: METRIC_COLORS.pod }}>해소</span>
                </>
              )}
            </dd>
          </div>
          <div className="flex items-center gap-2">
            <dt className="text-muted-foreground text-xs">심각도</dt>
            <dd style={{ color }}>{SEVERITY_LABEL[incident.severity] ?? incident.severity}</dd>
          </div>
          <div className="flex items-center gap-2">
            <dt className="text-muted-foreground text-xs">지속 시간</dt>
            <dd className="tabular-nums">{elapsedText(incident.firstAt, now)}</dd>
          </div>
        </dl>

        <Separator />
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          <span className="text-muted-foreground text-xs">진단 요약</span>
          <p className="min-w-0 flex-1 text-sm leading-snug">
            {incident.cause || '진행 중인 장애가 없다 — 아래 경로는 마지막 대상 개체의 위치다.'}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

// ---- 원인 분석 (좌측 표 + 미니차트 3종) ----

function CauseRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="w-[5.5rem] shrink-0 text-muted-foreground text-xs">{label}</dt>
      <dd className={`min-w-0 flex-1 text-[13px] leading-snug ${mono ? 'font-mono' : 'tabular-nums'}`}>
        {value}
      </dd>
    </div>
  );
}

function CauseAnalysis({ state, now }: { state: DemoState; now: number }) {
  const { demoMode } = useDemo();
  const { incident, scenario } = state;
  const series = useVictimSeries(scenario.victim.uuid, demoMode);
  const ecc = series.data?.ecc;
  const temp = series.data?.temp;
  const util = series.data?.util;
  // 목업의 x축은 시각 눈금이고 장애 시점에 점선 마커가 선다 — 표본 시각과
  // 최초 발생 시각(초)을 함께 넘기면 차트가 그 둘을 그린다.
  const markerAt = incident.firstAt > 0 ? incident.firstAt / 1000 : undefined;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">원인 분석</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 xl:grid-cols-[15rem_minmax(0,1fr)]">
        <dl className="flex flex-col gap-2">
          <CauseRow label="장애 코드" value={incident.code} mono />
          <CauseRow label="최초 발생 시각" value={timeText(incident.firstAt)} />
          <CauseRow label="최근 발생 시각" value={timeText(incident.at)} />
          <CauseRow label="관련 이벤트 수" value={formatCount(incident.eventCount)} />
          <CauseRow label="지속 시간" value={elapsedText(incident.firstAt, now)} />
          <CauseRow label="추정 원인" value={incident.cause || '—'} />
        </dl>
        <div className="grid gap-3 sm:grid-cols-3">
          <VictimMetricChart
            label="ECC DBE Count"
            valueText={ecc?.last == null ? null : `${formatCount(ecc.last)} count`}
            color={METRIC_COLORS.fault}
            values={ecc?.values ?? []}
            times={ecc?.times}
            markerAt={markerAt}
          />
          <VictimMetricChart
            label={`Temperature (${scenario.victim.device})`}
            valueText={temp?.last == null ? null : formatCelsius(temp.last)}
            color={METRIC_COLORS.thermal}
            values={temp?.values ?? []}
            times={temp?.times}
            markerAt={markerAt}
          />
          <VictimMetricChart
            label="GPU Utilization"
            valueText={util?.last == null ? null : formatPct(util.last, 0)}
            color={METRIC_COLORS.gpu}
            values={util?.values ?? []}
            times={util?.times}
            markerAt={markerAt}
            domainMax={100}
          />
        </div>
      </CardContent>
    </Card>
  );
}

// ---- 영향 범위 요약 (한 카드 안 4 카운터) ----

function ImpactSummary({ impact }: { impact: IncidentImpact }) {
  const cells: Array<{ icon: ReactNode; label: string; value: string; unit: string }> = [
    { icon: <Cpu />, label: '영향 GPU', value: formatCount(impact.gpus), unit: '대' },
    { icon: <Box />, label: '영향 Pod', value: formatCount(impact.pods), unit: '개' },
    { icon: <Users />, label: '영향 Tenant', value: formatCount(impact.tenants), unit: '개' },
    { icon: <Boxes />, label: '영향 Service', value: formatCount(impact.services), unit: '개' },
  ];
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">영향 범위 요약</CardTitle>
      </CardHeader>
      <CardContent className="grid grid-cols-2 divide-x md:grid-cols-4">
        {cells.map((c) => (
          <div key={c.label} className="flex min-w-0 items-center gap-3 px-3 py-1">
            <span className="[&>svg]:size-7 shrink-0 text-muted-foreground/70">{c.icon}</span>
            <div className="min-w-0">
              <p className="truncate text-muted-foreground text-xs">{c.label}</p>
              {/* 목업의 "2 대" — 숫자는 크게, 단위는 작게 붙여 쓴다. */}
              <p className="flex items-baseline gap-1">
                <span className="font-semibold text-2xl tabular-nums leading-tight">{c.value}</span>
                <span className="text-muted-foreground text-xs">{c.unit}</span>
              </p>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

// ---- 우측: 권장 조치 + CTA ----

function ActionPanel({ state }: { state: DemoState }) {
  const { act, refresh } = useDemo();
  const { incident, scenario } = state;
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cta = scenario.pendingAction ? CTA[scenario.pendingAction] : undefined;

  async function run(action: DemoAction, body?: Record<string, string>) {
    if (pending) return;
    setPending(true);
    setError(null);
    const outcome = await act(action, body);
    setPending(false);
    if (outcome.ok) {
      refresh();
      return;
    }
    setError(
      outcome.status === 409
        ? `조치 거부 — ${outcome.error}`
        : `조치 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
    );
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">권장 조치 / 다음 액션</CardTitle>
        <CardDescription className="text-xs">
          자동복구가 모은 근거 기반 — 현 단계에서 허용된 조치만 실행된다.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <IncidentActionList actions={incident.actions} />

        <Button className="w-full" disabled={!cta || pending} onClick={() => cta && run(cta.action)}>
          <ShieldCheck />
          {cta ? cta.label : '실행 가능한 조치 없음'}
        </Button>
        <div className="grid grid-cols-2 gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link to="/gpu/remediation">
              <ExternalLink />
              격리 파이프라인
            </Link>
          </Button>
          <Button variant="outline" size="sm" asChild>
            <Link to="/gpu/validation">
              <ExternalLink />
              검증·번인 콘솔
            </Link>
          </Button>
        </div>
        <Button
          variant="ghost"
          size="sm"
          disabled={pending}
          onClick={() => run('report-false-positive', { uuid: scenario.victim.uuid })}
        >
          오탐 신고 — 룰 보정 요청
        </Button>

        {error ? (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

export default function GpuIncident() {
  const { demoMode, state, refresh } = useDemo();
  // 지속 시간을 라이브로 갱신 — 헤더의 카운터가 1초마다 흐른다.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  if (demoMode === null || (demoMode === true && !state)) {
    return (
      <div className="flex flex-col gap-3" aria-busy="true">
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }

  if (demoMode !== true || !state) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>장애 상세</CardTitle>
          <CardDescription>
            장애 체인은 데모 모드의 시나리오 데이터로 구성된다 — 실서비스 인스턴스에서는
            표시할 진행 중 장애가 없다.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const { incident, alerts, scenario } = state;
  const impact = incident.impact;
  // 「관련 이벤트」의 모집단은 서버가 정한다 — victim 개체(노드 단위 알림은 개체가
  // 빈다) × *이번 사이클*(firstAt 이후). 노드만 보면 (a) 같은 노드 형제 GPU 의 일상
  // 알림과 (b) 지난 사이클의 CORDON·REPLACE·RETURNED 가 섞여, 바로 옆 「관련 이벤트
  // 수」(서버 eventCount)와 다른 집합을 세게 된다 — 실측(2사이클째 degrading): 수=0
  // 인데 타임라인은 지난 사이클 3건(그중 "운영 풀 재투입 완료")을 이 장애의 관련
  // 이벤트로 세웠다. 앰비언트(일상) 알림은 victim 개체를 비켜 가므로(fireAmbient)
  // 개체 규칙이 곧 앰비언트 배제다 — 코드 목록을 프론트에 복제하면 코드가 늘 때 두
  // 곳이 어긋난다. 두 술어의 등가는 Go 회귀가 지킨다(internal/demo/incident_events_test.go
  // 관련이벤트수는_화면_타임라인과_같은_모집단). 진행 중인 장애가 없으면(firstAt=0)
  // 관련 이벤트도 없다 — 서버 eventCount 도 그때 0 이다.
  const victim = scenario.victim;
  const related =
    incident.firstAt > 0
      ? alerts.filter(
          (a) =>
            a.instance === victim.instance &&
            (!a.device || a.device === victim.device) &&
            a.at >= incident.firstAt,
        )
      : [];

  return (
    <div className="flex flex-col gap-3">
      {/* 목업의 페이지 부제 + 우상단 「마지막 업데이트 · 새로고침」. 시각은 서버가
          스냅샷을 만든 시각(generatedAt)이고, 버튼은 폴링과 같은 재조회를 쏜다 —
          화면이 자기 시계로 지어내면 폴링이 멈춰도 시각만 흐른다. */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-muted-foreground text-sm">
          선택한 장애의 원인, 영향 범위, 권장 조치를 한 화면에서 확인합니다.
        </p>
        <div className="flex items-center gap-1 text-muted-foreground text-xs">
          <span className="tabular-nums">마지막 업데이트 {clockText(state.generatedAt)}</span>
          <Button variant="ghost" size="icon" className="size-7" onClick={refresh} title="새로고침">
            <RefreshCw />
            <span className="sr-only">새로고침</span>
          </Button>
        </div>
      </div>

      <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="flex min-w-0 flex-col gap-3">
        <IncidentHeader incident={incident} now={now} />

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">원인 및 영향 경로</CardTitle>
            <CardDescription className="text-xs">
              장애 개체에서 클라우드 경계까지 — 위로 갈수록 분모가 커지고 분자(영향 GPU)는
              고정이다.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <IncidentFlow links={incident.links} impact={impact} active={incident.active} />
          </CardContent>
        </Card>

        <CauseAnalysis state={state} now={now} />

        {/* 목업은 카드 4장이 아니라 *한 카드 안 4칸*이다 — 테두리 4개가 서 있으면
            네 개의 별개 지표로 읽히지만, 이건 한 장애의 영향 범위 한 벌이다.
            칸 사이는 세로 구분선(divide-x)만. */}
        <ImpactSummary impact={impact} />
      </div>

      <div className="flex min-w-0 flex-col gap-3">
        <ActionPanel state={state} />
        <Card>
          <CardHeader className="pb-2">
            <div className="flex items-start justify-between gap-2">
              <CardTitle className="text-sm">관련 이벤트 타임라인</CardTitle>
              {/* 목업의 「전체 보기」 — 여기서는 최근 6건만 자른다. 전체 목록은
                  같은 서버 알림 상태를 쓰는 무음 장애 탐지 화면이다. */}
              <Button variant="link" size="sm" className="h-auto p-0 text-xs" asChild>
                <Link to="/gpu/health">전체 보기</Link>
              </Button>
            </div>
            <CardDescription className="text-xs">
              이 장애({victim.instance} / {victim.device})의 이번 사이클 신호만 — 최근 6건까지,
              확인 처리는 헬스 화면과 같은 서버 상태를 공유한다.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {/* 비었을 때 공용 문구("수신된 알림이 없다")를 쓰면 알림 자체가 없다는 말이
                된다 — 없는 것은 *이 장애의* 신호다. */}
            {related.length > 0 ? (
              <AlertTimeline alerts={related.slice(0, 6)} />
            ) : (
              <p className="py-6 text-center text-muted-foreground text-sm">
                이번 장애에 접수된 이벤트가 없다 — 지난 사이클·형제 GPU 의 알림은 이 장애의
                근거가 아니라 여기 싣지 않는다.
              </p>
            )}
          </CardContent>
        </Card>
      </div>
      </div>
    </div>
  );
}
