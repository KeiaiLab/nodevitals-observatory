// GpuAssistant — demo3 "GPU Assistant". 진행 중 장애를 문맥으로, 자연어 질문에
// 카드로 답하는 대화형 콘솔이다. 응답은 결정론 스크립트다(LLM 아님 — 규약
// §2.3 "단순 계산에 LLM 금지"): 질문을 6개 의도(원인/영향/이벤트/보고서/유휴/
// 격리)로 분류해 이미 백엔드 DemoState 에 있는 데이터를 카드로 조립한다. 신규
// 데이터 소스는 없다 — victim 미니차트만 useVictimSeries 가 range 질의로 가져온다.
//
// 조회 6종 중 '노드 격리 실행' 하나만 실제 조치다(approve-isolation) — 단계
// 규칙은 서버가 SSOT 라 409 사유를 그대로 말풍선에 되돌린다.
import {
  Ban,
  Bot,
  CheckCheck,
  CheckCircle2,
  ChevronRight,
  Clock,
  Download,
  ExternalLink,
  FileText,
  RefreshCw,
  Send,
  Server,
  Share2,
} from 'lucide-react';
import { Fragment, type ReactNode, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import AlertTimeline from '@/components/gpu/AlertTimeline';
import IdleDonut from '@/components/gpu/IdleDonut';
import RemediationStepper from '@/components/gpu/RemediationStepper';
import VictimMetricChart from '@/components/gpu/VictimMetricChart';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemo } from '@/hooks/demoContext';
import { useVictimSeries } from '@/hooks/useVictimSeries';
import type { AlertEvent, ChainLink, DemoState } from '@/lib/demoApi';
import {
  formatBytes,
  formatCelsius,
  formatCount,
  formatPct,
  METRIC_COLORS,
} from '@/lib/gpuMetrics';

// ---- 의도(질문) 정의 — 퀵액션 칩 + 키워드 매칭 ----

type Intent = 'cause' | 'impact' | 'events' | 'report' | 'idle' | 'isolate';

interface IntentDef {
  id: Intent;
  chip: string;
  icon: ReactNode;
  /** 사용자 말풍선으로 되비출 문장(칩 클릭 시). */
  utterance: string;
  /** 어시스턴트 답변의 리드인 한 줄. */
  reply: string;
  /** 자유 입력 매칭 키워드. */
  keywords: string[];
}

const INTENTS: IntentDef[] = [
  {
    id: 'cause',
    chip: '원인 요약',
    icon: <FileText className="size-3" />,
    utterance: '원인 요약해줘',
    reply: '확인했습니다. 현재 장애의 요약 정보를 아래 정리했어요.',
    keywords: ['원인', '이유', '왜', 'cause'],
  },
  {
    id: 'impact',
    chip: '영향 범위',
    icon: <Share2 className="size-3" />,
    utterance: '영향 범위 보여줘',
    reply: '영향 범위를 정리했어요.',
    keywords: ['영향', '범위', '경로', 'impact', 'blast'],
  },
  {
    id: 'events',
    chip: '이벤트 정리',
    icon: <Clock className="size-3" />,
    utterance: '관련 이벤트 정리해줘',
    reply: '관련 이벤트 타임라인이에요.',
    keywords: ['이벤트', '타임라인', '알림', 'event'],
  },
  {
    id: 'report',
    chip: '장애보고서 생성',
    icon: <FileText className="size-3" />,
    utterance: '장애보고서 만들어줘',
    reply: '장애 보고서를 생성했어요. 다운로드해서 확인할 수 있어요.',
    keywords: ['보고서', '리포트', 'report', 'pdf'],
  },
  {
    id: 'idle',
    chip: '유휴 장비',
    icon: <Server className="size-3" />,
    utterance: '현재 유휴 장비도 알려줘',
    reply: '현재 유휴 장비 현황을 조회했어요.',
    keywords: ['유휴', '놀고', 'idle', '여유'],
  },
  {
    id: 'isolate',
    chip: '노드 격리 실행',
    icon: <Ban className="size-3" />,
    utterance: '노드 격리 실행해줘',
    reply: '노드 격리를 시작했어요.',
    keywords: ['격리', 'cordon', 'isolate', '차단'],
  },
];

function matchIntent(text: string): Intent | null {
  const t = text.toLowerCase();
  for (const it of INTENTS) {
    if (it.keywords.some((k) => t.includes(k.toLowerCase()))) return it.id;
  }
  return null;
}

// ---- 대화 메시지 ----

/** 보고서는 질문한 그 시점에 얼려 둔다 — 카드가 폴링마다 다시 만들어지면
 *  파일명·생성일·용량이 화면에서 흔들린다. */
interface ReportSnap {
  name: string;
  text: string;
  at: number;
}

type Message =
  | { role: 'user'; text: string; at: number }
  | { role: 'assistant'; text: string; at: number; card?: Intent; report?: ReportSnap };

type AssistantMessage = Extract<Message, { role: 'assistant' }>;

// ---- 유틸 ----

const TONE_TEXT: Record<ChainLink['tone'], string> = {
  crit: 'text-metric-fault',
  major: 'text-metric-thermal',
  warn: 'text-metric-thermal',
  info: 'text-metric-cpu',
  muted: 'text-muted-foreground',
  ok: 'text-metric-pod',
};

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

function shortModel(model: string): string {
  return model.replace(/^NVIDIA\s+/, '');
}

/** GPU UUID 를 앞 4 + 뒤 4 로 — 7rem 박스에 36자를 넣으면 아무도 못 읽는다.
 *  전 GPU 공통인 'GPU-' 접두사는 먼저 벗긴다: 그대로 자르면 앞 4자가 늘 'GPU-'
 *  라 'GPU-…62fc' 가 되어 식별 hex 가 뒤 4자만 남는다(목업은 '3f2a…d91c'). */
function shortUuid(uuid: string): string {
  const hex = uuid.replace(/^GPU-/, '');
  return hex.length <= 12 ? hex : `${hex.slice(0, 4)}…${hex.slice(-4)}`;
}

function hhmm(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** epoch ms 이후 경과를 hh:mm:ss 로. 0(장애 없음)이면 '—'. */
function elapsedText(fromMs: number, nowMs: number): string {
  if (fromMs <= 0) return '—';
  const s = Math.max(0, Math.floor((nowMs - fromMs) / 1000));
  const hh = String(Math.floor(s / 3600)).padStart(2, '0');
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, '0');
  const ss = String(s % 60).padStart(2, '0');
  return `${hh}:${mm}:${ss}`;
}

/** 지속 시간의 기준점 — 최초 발생(firstAt)이다. 진행 중인 장애가 없으면 0 이다:
 *  그때 서버 incident.at 은 스냅샷 생성 시각(=지금)이라, 그걸로 물러나면 폴링마다
 *  '지금'으로 되감기는 지속 시간과 차트 오른쪽 끝에 찍히는 '장애 시점' 마커가
 *  생긴다 — 같은 화면 헤더가 '진행 중인 장애 없음' 이라 말하는 동안. */
function incidentStart(state: DemoState): number {
  if (!state.incident.active) return 0;
  return state.incident.firstAt || state.incident.at;
}

/** victim GPU 의, *이번 장애 사이클* 알림만 최신순. 없으면 빈 배열이다 — 타
 *  노드·형제 GPU 의 알림으로 물러나면 화면이 남의 사건을 victim 소행으로 단정한다
 *  (실측: victim 알림 0건인데 '원인 요약' 첫 줄이 다른 노드의 AGENT_MISSING 을
 *  gpu7 소행으로 적었다). 개체(device)가 빈 알림은 노드 단위 사건이라 이 GPU 의
 *  근거가 맞다.
 *
 *  개체만큼 시간 축도 서버와 같은 기준이어야 한다. 알림 버퍼는 사이클이 넘어가도
 *  비지 않으므로(리셋도 단계·장애 시계만 되감는다) 개체만 보면 *지난 사이클*의
 *  CORDON·REPLACE·RETURNED 가 그대로 통과한다 — 실측: 2사이클째 degrading 에서
 *  서버는 eventCount=0 인데 이 함수가 3건을 돌려줬고 원인 카드 첫 불릿이 지난
 *  사이클의 '운영 풀 재투입 완료' 를 지금 진행 중인 무음 열화의 원인이라 적었다.
 *  그래서 firstAt(현 사이클 최초 발생) 이전 알림을 배제한다 — 서버가 같은 판정을
 *  하는 지점이 incident.go:112 의 `a.At < s.incidentAt` 다. firstAt=0(진행 중인
 *  장애 없음)이면 거르지 않는다: 그때는 서버 헤드라인도 이 노드의 마지막 사건
 *  그대로다(incident.go:107-110). */
function victimAlerts(state: DemoState): AlertEvent[] {
  const { instance, device } = state.scenario.victim;
  const since = state.incident.firstAt;
  return state.alerts
    .filter(
      (a) =>
        a.instance === instance &&
        (!a.device || a.device === device) &&
        (since === 0 || a.at >= since),
    )
    .sort((a, b) => b.at - a.at);
}

/** 지속 시간 셀만 1초마다 다시 그린다 — 페이지 전체를 초당 렌더하면 카드가
 *  쌓일수록 Recharts 가 통째로 재렌더된다. */
function ElapsedValue({ from }: { from: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);
  return <span className="tabular-nums">{elapsedText(from, now)}</span>;
}

// ---- 장애 보고서 (클라이언트 생성 — 실제 PDF 파이프라인 대신 결정론 텍스트) ----

/** 목업의 INC-YYYYMMDD-HHMMSS 포맷. 확장자는 실제 내용대로 .txt 다(한글 폰트를
 *  임베드해야 하는 PDF 생성기를 이 데모에 끌어들이지 않는다). */
function reportName(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `INC-${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}.txt`;
}

function buildReportSnap(state: DemoState, nowMs: number): ReportSnap {
  const { incident, scenario } = state;
  const v = scenario.victim;
  const events = victimAlerts(state).slice(0, 12);
  const lines = [
    `장애 보고서 — ${incident.code || 'INCIDENT'}`,
    `생성 시각: ${new Date(nowMs).toLocaleString('ko-KR')}`,
    '',
    `제목: ${incident.title}`,
    `심각도: ${incident.severity}`,
    `최초 발생: ${incidentStart(state) > 0 ? new Date(incidentStart(state)).toLocaleString('ko-KR') : '—'}`,
    `지속: ${elapsedText(incidentStart(state), nowMs)}`,
    `대상 GPU: ${v.device} (${v.model}, uuid ${v.uuid})`,
    `노드: ${v.instance} · 클러스터: ${v.cluster} · CSP: ${v.csp}`,
    `영향 Pod: ${incident.pods.length}개 / 클러스터 파드 ${incident.podsTotal}`,
    ...(incident.cause ? ['', `추정 원인: ${incident.cause}`] : []),
    '',
    '영향 경로:',
    ...incident.links.map((l) => `  - ${l.level}: ${l.value} — ${l.detail} (${l.scope}/${l.total})`),
    '',
    `Health 감점 (점수 ${v.health.score}):`,
    ...(v.health.deductions.length > 0
      ? v.health.deductions.map((d) => `  - ${d.code}: ${d.message} (-${d.penalty})`)
      : ['  - 없음']),
    '',
    '관련 이벤트:',
    // victim 알림이 없으면 '없음' 이다 — 타 노드 이벤트를 '관련' 으로 실으면
    // 보고서가 남의 사건을 이 장애의 증적으로 남긴다.
    ...(events.length > 0
      ? events.map(
          (a) =>
            `  - ${new Date(a.at).toLocaleTimeString('ko-KR')} [${a.severity}] ${a.code} ${a.title}`,
        )
      : ['  - 없음']),
  ];
  return { name: reportName(nowMs), text: lines.join('\n'), at: nowMs };
}

function downloadReport(snap: ReportSnap) {
  const blob = new Blob([snap.text], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = snap.name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// ---- 카드들 ----

function SummaryCell({
  label,
  value,
  sub,
  tone,
}: {
  label: string;
  value: ReactNode;
  sub?: string;
  tone?: string;
}) {
  // 목업(demo3)의 셀은 가운데 정렬 + 값이 크다. justify-center 는 유휴 카드처럼
  // 옆 칸(도넛)이 키를 늘릴 때 값이 위로 붙어 아래가 텅 비는 것을 막는다.
  return (
    <div className="flex flex-col items-center justify-center gap-0.5 rounded-md border bg-card p-2.5 text-center">
      <span className="text-muted-foreground text-[11px]">{label}</span>
      <span
        className={`w-full truncate font-semibold text-lg ${tone ?? ''}`}
        title={typeof value === 'string' ? value : undefined}
      >
        {value}
      </span>
      {sub ? (
        <span className="w-full truncate text-[10px] text-muted-foreground" title={sub}>
          {sub}
        </span>
      ) : null}
    </div>
  );
}

function CauseCard({ state }: { state: DemoState }) {
  const { incident, scenario, fleet, audit } = state;
  const v = scenario.victim;
  // victim 은 tenant 를 직접 담지 않는다 — 소속 pool 로 fleet 에서 조회한다.
  const pool = fleet.pools.find((p) => p.id === v.pool);
  const tenant = fleet.tenants.find((t) => t.id === pool?.tenant)?.display ?? pool?.tenant ?? '—';
  const startAt = incidentStart(state);
  const deductions = v.health.deductions;

  // 원인 불릿은 전부 실데이터 파생이다 — ECC 문구를 하드코딩하면 번인·교체
  // 단계에서 화면이 거짓말을 한다.
  //
  // 첫 불릿은 *최신* 알림이 아니라 *서버가 원인으로 고른* 알림이다. 최신을
  // 쓰면 격리 승인 직후 발행되는 조치 알림(CORDON 등)이 원인 칸을 뺏는다 —
  // 실측: draining 에서 서버 헤드라인은 XID_63(critical)인데 이 불릿은 "노드
  // Cordon(CORDON)"을 원인이라 적었고, 바로 아래 두 번째 불릿(incident.cause)
  // 은 "ECC DBE" 라 같은 카드가 서로 다른 원인을 말했다. 판정 규칙(어떤 코드가
  // 원인을 말할 수 있는가)은 서버 incident.go 의 isIncidentCode 하나뿐이므로,
  // 화면은 그 결론(incident.code)에 해당하는 알림을 되짚어 상세만 붙인다 —
  // 화이트리스트를 프론트에 복제하면 코드가 늘 때 두 곳이 어긋난다.
  const causeAlert = victimAlerts(state).find((a) => a.code === incident.code);
  const bullets: string[] = [];
  if (causeAlert) {
    bullets.push(
      `${v.device}에서 ${causeAlert.title} (${causeAlert.code})${causeAlert.detail ? ` — ${causeAlert.detail}` : ''}`,
    );
  } else {
    // 근거가 없으면 없다고 말한다 — 다른 노드 알림을 끌어와 문장을 만들면 그게 날조다.
    // 조치 알림만 있는 구간(단계 점프 직후)에서는 서버 헤드라인을 그대로 옮긴다.
    bullets.push(`${v.device} — ${incident.title} · 원인 판정 알림 없음, 아래는 Health 판정 근거입니다.`);
  }
  if (incident.cause) bullets.push(incident.cause);
  for (const d of deductions.slice(0, 3)) bullets.push(d.message);
  if (deductions.length === 0) bullets.push(`현재 감점 항목 없음 (Health ${v.health.score})`);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="font-semibold text-sm">장애 요약</span>
        <Badge
          variant="outline"
          className="text-[10px] uppercase"
          style={{
            color: SEVERITY_COLOR[incident.severity] ?? METRIC_COLORS.cpu,
            borderColor: SEVERITY_COLOR[incident.severity] ?? METRIC_COLORS.cpu,
          }}
        >
          {SEVERITY_LABEL[incident.severity] ?? incident.severity}
        </Badge>
      </div>

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-6">
        <SummaryCell
          label="장애 상태"
          value={SEVERITY_LABEL[incident.severity] ?? incident.severity}
          tone={incident.severity === 'critical' ? 'text-metric-fault' : 'text-metric-thermal'}
        />
        <SummaryCell
          label="장애 지속 시간"
          value={<ElapsedValue from={startAt} />}
          sub={startAt > 0 ? `${hhmm(startAt)} ~ 현재` : undefined}
        />
        <SummaryCell
          label="영향 GPU"
          value={`${v.device} · ${shortModel(v.model)}`}
          sub={`UUID: ${shortUuid(v.uuid)}`}
        />
        <SummaryCell
          label="영향 Pod"
          value={`${formatCount(incident.pods.length)}개`}
          sub={`클러스터 파드 ${formatCount(incident.podsTotal)} 중`}
        />
        <SummaryCell label="테넌트" value={tenant} sub="프로젝트" />
        <SummaryCell label="서비스" value={pool?.display ?? '—'} sub={pool?.kind} />
      </div>

      <div className="rounded-md border bg-card p-3">
        <p className="mb-1.5 font-medium text-sm">원인 요약</p>
        <ul className="flex flex-col gap-1.5 text-sm leading-relaxed">
          {bullets.map((b, i) => (
            <li key={`${i}-${b}`} className="flex gap-2">
              <span className="text-metric-fault">•</span>
              <span>{b}</span>
            </li>
          ))}
        </ul>
        <details className="mt-2">
          <summary className="cursor-pointer text-[11px] text-muted-foreground hover:text-foreground">
            자세히 보기
          </summary>
          <div className="mt-2 flex flex-col gap-1.5 border-t pt-2 text-xs">
            <p className="text-muted-foreground">
              Health <span className="font-semibold text-foreground tabular-nums">{v.health.score}</span>
            </p>
            {deductions.length > 0 ? (
              deductions.map((d) => (
                <p key={d.code} className="flex items-baseline gap-2">
                  <span className="font-mono text-muted-foreground">{d.code}</span>
                  <span className="flex-1">{d.message}</span>
                  <span className="text-metric-fault tabular-nums">-{d.penalty}</span>
                </p>
              ))
            ) : (
              <p className="text-muted-foreground">감점 항목 없음</p>
            )}
            {audit[0] ? (
              <p className="text-muted-foreground">
                최근 조치 — {audit[0].actor} · {audit[0].action}
                {audit[0].evidence ? ` · ${audit[0].evidence}` : ''}
              </p>
            ) : null}
          </div>
        </details>
      </div>
    </div>
  );
}

interface FlowBox {
  level: string;
  value: string;
  detail: string;
  tone: ChainLink['tone'];
}

/** 서버 links(6계층, GPU 수 축) 위에 Pod/Job 한 칸을 흐름도에만 합성한다.
 *  links 배열 자체에는 넣지 않는다 — 그 배열은 "분자 고정, 분모만 증가"라는
 *  blast radius 불변식을 지키는 계약이고 파드 수는 그 축이 아니다. */
function impactFlow(state: DemoState): FlowBox[] {
  const { links, pods } = state.incident;
  const out: FlowBox[] = [];
  for (const l of links) {
    out.push({
      level: l.level,
      value: l.level === 'GPU' ? shortUuid(l.value) : l.value,
      detail: l.detail,
      tone: l.tone,
    });
    if (l.level === 'Kubernetes Node' && pods.length > 0) {
      out.push({
        level: 'Pod / Job',
        value: `${formatCount(pods.length)}개 영향`,
        detail: pods.slice(0, 2).join(' · '),
        tone: 'major',
      });
    }
  }
  return out;
}

function ImpactCard({ state, uuid }: { state: DemoState; uuid: string }) {
  const { demoMode } = useDemo();
  const series = useVictimSeries(uuid, demoMode);
  const s = series.data;
  const boxes = impactFlow(state);
  const startAt = incidentStart(state);
  // 장애 시점 마커 — 30분 창 밖으로 밀려나면 Recharts 가 그리지 않는다(정상).
  const markerAt = startAt > 0 ? Math.floor(startAt / 1000) : undefined;

  return (
    <div className="flex flex-col gap-4">
      {/* 영향 경로 — 가로 흐름(장애 GPU → … → CSP). */}
      <div>
        <p className="mb-2 font-medium text-sm">영향 경로</p>
        {boxes.length === 0 ? (
          <p className="text-muted-foreground text-sm">체인을 구성할 대상이 없다.</p>
        ) : (
          // 목업은 한 줄 가로 흐름이다. wrap 시키면 줄 끝에 화살표가 허공에 매달리고
          // 박스 폭이 내용대로 제각각이 된다 — 균등 폭 1줄로 두고 좁은 화면에서만
          // 가로 스크롤한다.
          <div className="flex items-stretch gap-1 overflow-x-auto">
            {boxes.map((b, i) => (
              <Fragment key={b.level}>
                <div className="flex min-w-0 flex-1 flex-col justify-center rounded-md border bg-card p-2 text-center">
                  <span className="truncate text-[10px] text-muted-foreground uppercase tracking-wide">
                    {b.level}
                  </span>
                  <span className={`truncate font-semibold text-sm ${TONE_TEXT[b.tone]}`} title={b.value}>
                    {b.value}
                  </span>
                  <span className="truncate text-[11px] text-muted-foreground" title={b.detail}>
                    {b.detail}
                  </span>
                </div>
                {i < boxes.length - 1 ? (
                  <span className="flex shrink-0 items-center text-muted-foreground text-xs">→</span>
                ) : null}
              </Fragment>
            ))}
          </div>
        )}
      </div>

      {/* 주요 지표 — victim GPU 최근 30분 추이 + 장애 시점 점선. */}
      <div>
        <p className="mb-2 font-medium text-sm">주요 지표 (최근 30분)</p>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          <VictimMetricChart
            label="GPU Temperature (°C)"
            valueText={s ? formatCelsius(s.temp.last) : null}
            color={METRIC_COLORS.thermal}
            values={s?.temp.values ?? []}
            times={s?.temp.times}
            markerAt={markerAt}
            domainMax={100}
          />
          <VictimMetricChart
            label="GPU Memory ECC Errors"
            valueText={s ? formatCount(s.ecc.last) : null}
            color={METRIC_COLORS.fault}
            values={s?.ecc.values ?? []}
            times={s?.ecc.times}
            markerAt={markerAt}
          />
          <VictimMetricChart
            label="GPU Utilization (%)"
            valueText={s ? formatPct(s.util.last, 0) : null}
            color={METRIC_COLORS.cpu}
            values={s?.util.values ?? []}
            times={s?.util.times}
            markerAt={markerAt}
            domainMax={100}
          />
        </div>
      </div>
    </div>
  );
}

function EventsCard({ state }: { state: DemoState }) {
  const alerts = victimAlerts(state).slice(0, 10);
  return (
    <div className="rounded-md border bg-card p-3">
      <p className="mb-2 font-medium text-sm">관련 이벤트 타임라인</p>
      {alerts.length > 0 ? (
        <AlertTimeline alerts={alerts} severityBadge />
      ) : (
        <p className="py-4 text-center text-muted-foreground text-sm">
          {state.scenario.victim.device}에 접수된 이벤트가 아직 없습니다. 다른 노드의 알림은 이
          장애의 근거가 아니라 여기 싣지 않습니다.
        </p>
      )}
    </div>
  );
}

function ReportCard({ snap, device }: { snap: ReportSnap; device: string }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-md border bg-card p-3">
      {/* 목업은 왼쪽에 빨간 문서 블록이 큼직하게 서 있다. */}
      <div className="flex size-11 shrink-0 items-center justify-center rounded-md border border-metric-fault/40 bg-metric-fault/10">
        <FileText className="size-6 text-metric-fault" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium text-sm">
          {device} 장애 보고서 ({snap.name})
        </p>
        <p className="text-muted-foreground text-xs">
          생성일 {new Date(snap.at).toLocaleString('ko-KR')} ·{' '}
          <span className="tabular-nums">{formatBytes(new Blob([snap.text]).size)}</span>
        </p>
      </div>
      <Badge variant="outline" className="gap-1 border-metric-pod text-[10px] text-metric-pod">
        <CheckCircle2 className="size-3" />
        생성 완료
      </Badge>
      <Button
        size="sm"
        variant="outline"
        className="h-8 shrink-0"
        onClick={() => downloadReport(snap)}
      >
        <Download className="size-3.5" />
        다운로드
      </Button>
    </div>
  );
}

function IdleCard({ state }: { state: DemoState }) {
  const navigate = useNavigate();
  const idle = state.idle;
  // 세는 것은 "유휴 GPU 를 1장이라도 가진 노드" 다 — 실측 플릿은 185장이 183개
  // 노드에 1장씩 흩어져 있어 '전 GPU 유휴' 노드는 0대다. 라벨이 '유휴 노드'라고
  // 말하면 화면이 세지도 않은 것을 말하게 되므로, 라벨을 세는 것에 맞춘다.
  const nodes = new Set(idle.map((g) => g.instance)).size;
  const gpuTotal = state.fleet.gpuTotal;
  const nodeTotal = state.fleet.nodeTotal;
  return (
    <div className="flex flex-col gap-3">
      <p className="font-semibold text-sm">유휴 장비 현황</p>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-[1fr_1fr_1.6fr]">
        <SummaryCell
          label="총 유휴 GPU"
          value={`${formatCount(idle.length)} 개`}
          sub={gpuTotal > 0 ? `전체 GPU 대비 ${formatPct((idle.length / gpuTotal) * 100)}` : undefined}
        />
        <SummaryCell
          label="유휴 GPU 보유 노드"
          value={`${formatCount(nodes)} 대`}
          sub={
            nodeTotal > 0
              ? `전체 노드 ${formatCount(nodeTotal)} 대 중 ${formatPct((nodes / nodeTotal) * 100)}`
              : undefined
          }
        />
        <div className="rounded-md border bg-card p-3">
          <p className="mb-2 font-medium text-sm">유휴 GPU 모델 분포</p>
          <IdleDonut idle={idle} />
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-card p-3">
        <div className="min-w-0">
          <p className="font-medium text-sm">유휴 장비 관리는 아래 메뉴에서 확인할 수 있습니다.</p>
          <p className="text-muted-foreground text-xs">
            유휴 장비 상세 조회, 필터링, 알림 설정, 자동 반납 정책 등 더 많은 기능을 이용해 보세요.
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <Button size="sm" variant="outline" className="h-8" onClick={() => navigate('/gpu/idle')}>
            유휴 장비 관리로 이동
            <ExternalLink className="size-3.5" />
          </Button>
          <span className="text-[11px] text-muted-foreground">
            자원·토폴로지 &gt; 유휴장비 &gt; 유휴장비 관리
          </span>
        </div>
      </div>
    </div>
  );
}

// ---- 카드 라우팅 ----

function AssistantCard({ msg, state }: { msg: AssistantMessage; state: DemoState }) {
  switch (msg.card) {
    case 'cause':
      return <CauseCard state={state} />;
    case 'impact':
      return <ImpactCard state={state} uuid={state.scenario.victim.uuid} />;
    case 'events':
      return <EventsCard state={state} />;
    case 'report':
      return msg.report ? (
        <ReportCard snap={msg.report} device={state.scenario.victim.device} />
      ) : null;
    case 'idle':
      return <IdleCard state={state} />;
    case 'isolate':
      return <RemediationStepper drain={state.scenario.victim.drain} />;
    default:
      return null;
  }
}

function QuickChips({ onPick, disabled }: { onPick: (id: Intent) => void; disabled: boolean }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {INTENTS.map((it) => (
        <Button
          key={it.id}
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          disabled={disabled}
          onClick={() => onPick(it.id)}
        >
          {it.icon}
          {it.chip}
        </Button>
      ))}
    </div>
  );
}

// ---- 페이지 ----

export default function GpuAssistant() {
  const { demoMode, state, act, refresh } = useDemo();
  const navigate = useNavigate();
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState('');
  const [acting, setActing] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const seeded = useRef(false);
  const prevCount = useRef(0);

  // 시연 첫 화면이 빈 채팅이면 안 된다 — 상태가 준비되는 즉시 목업(demo3)의
  // 5문답(원인·영향·이벤트·보고서·유휴)을 미리 쌓아 둔다. 칩을 하나씩 눌러야
  // 카드가 나타나면 화면을 훑는 시연에서 아무것도 안 보인다.
  useEffect(() => {
    if (seeded.current || !state) return;
    seeded.current = true;
    const base = Date.now() - 4 * 60_000;
    const script: Intent[] = ['cause', 'impact', 'events', 'report', 'idle'];
    setMessages(
      script.flatMap((id, i): Message[] => {
        const def = INTENTS.find((d) => d.id === id);
        if (!def) return [];
        const at = base + i * 60_000;
        return [
          { role: 'user', text: def.utterance, at },
          {
            role: 'assistant',
            text: def.reply,
            card: id,
            at,
            report: id === 'report' ? buildReportSnap(state, at) : undefined,
          },
        ];
      }),
    );
  }, [state]);

  // 새 메시지가 쌓이면 맨 아래로. 단 최초 시드는 예외다 — 시연은 대화를
  // 위에서부터 훑으므로 로드 직후 맨 아래로 튀면 안 된다.
  useEffect(() => {
    if (prevCount.current > 0) {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
    }
    prevCount.current = messages.length;
  }, [messages]);

  // 로딩/비데모 가드
  if (demoMode === null || (demoMode === true && !state)) {
    return (
      <div className="flex flex-col gap-3" aria-busy="true">
        <Skeleton className="h-14 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }
  if (demoMode !== true || !state) {
    return (
      <Card>
        <CardContent className="p-6">
          <p className="font-semibold">GPU Assistant</p>
          <p className="text-muted-foreground text-sm">
            어시스턴트는 데모 모드의 진행 중 장애를 문맥으로 동작한다 — 실서비스 인스턴스에는
            응답할 장애 시나리오가 없다.
          </p>
        </CardContent>
      </Card>
    );
  }

  const snapshot = state;
  const incident = snapshot.incident;
  const victim = snapshot.scenario.victim;

  /** 노드 격리 — 이 화면의 유일한 실조치. 단계 규칙은 서버가 판정하므로
   *  409 사유를 그대로 말풍선에 되돌린다(규칙을 클라에 복제하지 않는다). */
  async function runIsolate(utterance: string) {
    setMessages((prev) => [...prev, { role: 'user', text: utterance, at: Date.now() }]);
    setActing(true);
    const out = await act('approve-isolation');
    setActing(false);
    setMessages((prev) => [
      ...prev,
      out.ok
        ? {
            role: 'assistant',
            text: `${out.result.message} 진행 상황은 아래에서 확인할 수 있어요.`,
            card: 'isolate',
            at: Date.now(),
          }
        : { role: 'assistant', text: `격리를 실행하지 못했어요 — ${out.error}`, at: Date.now() },
    ]);
    refresh();
  }

  /** 칩 클릭은 의도만 준다(문장은 INTENTS 가 갖고 있다). 자유 입력은 사용자가
   *  실제로 친 문장을 그대로 되비춘다. */
  function ask(intent: Intent, typed?: string) {
    const def = INTENTS.find((i) => i.id === intent);
    if (!def) return;
    const utterance = typed ?? def.utterance;
    if (intent === 'isolate') {
      void runIsolate(utterance);
      return;
    }
    const at = Date.now();
    setMessages((prev) => [
      ...prev,
      { role: 'user', text: utterance, at },
      {
        role: 'assistant',
        text: def.reply,
        card: intent,
        at,
        report: intent === 'report' ? buildReportSnap(snapshot, at) : undefined,
      },
    ]);
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text) return;
    setInput('');
    const intent = matchIntent(text);
    if (intent) {
      ask(intent, text);
      return;
    }
    const at = Date.now();
    setMessages((prev) => [
      ...prev,
      { role: 'user', text, at },
      {
        role: 'assistant',
        text: '원인 요약 · 영향 범위 · 이벤트 정리 · 장애보고서 · 유휴 장비 · 노드 격리 실행 중에서 물어봐 주세요.',
        at,
      },
    ]);
  }

  // 퀵액션 칩은 마지막 어시스턴트 말풍선 아래에만 붙는다(목업의 인라인 배치).
  const lastAssistant = messages.reduce((acc, m, i) => (m.role === 'assistant' ? i : acc), -1);

  return (
    // 차감값 = 셸 헤더(2행 97px) + 본문 패딩 + 제목줄. 헤더 높이가 바뀌면 이
    // 값도 같이 바꿔야 한다 — 안 그러면 대화창이 뷰포트 밖으로 밀려 페이지가
    // 세로로 스크롤된다(채팅 화면은 화면에 딱 맞아야 한다).
    <div className="flex h-[calc(100vh-10.5rem)] flex-col gap-3">
      {/* 헤더 */}
      <Card className="gap-0 py-0">
        <CardContent className="flex items-center gap-3 p-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary/10">
            <Bot className="size-5 text-primary" />
          </div>
          <span className="font-semibold">GPU Assistant</span>
          <Badge variant="outline" className="gap-1 text-[10px]">
            <span className="size-1.5 rounded-full bg-metric-pod" />
            온라인
          </Badge>
          {incident.active ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-8 min-w-0 gap-1.5 rounded-full border"
              onClick={() => navigate('/gpu/incident')}
            >
              <span className="size-2 shrink-0 rounded-full bg-metric-fault" />
              <span className="truncate font-mono text-xs">
                {victim.instance} / {victim.device}
              </span>
              <span className="truncate text-xs">{incident.title}</span>
              <ChevronRight className="size-3.5 shrink-0" />
            </Button>
          ) : (
            <span className="rounded-full border px-3 py-1 text-muted-foreground text-xs">
              진행 중인 장애 없음
            </span>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            className="ml-auto shrink-0"
            aria-label="새로고침"
            onClick={refresh}
          >
            <RefreshCw className="size-4" />
          </Button>
        </CardContent>
      </Card>

      {/* 대화 영역 */}
      <div ref={scrollRef} className="flex-1 overflow-auto">
        <div className="flex flex-col gap-4 pb-2">
          {/* 첫 안내 */}
          <div className="flex gap-2">
            <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary/10">
              <Bot className="size-4 text-primary" />
            </div>
            <Card className="max-w-[92%] flex-1 py-0">
              <CardContent className="flex flex-col gap-3 p-3 text-sm leading-relaxed">
                {/* 장애가 없을 때 서버 title 은 '진행 중인 장애 없음' 이다 — 앞에 '진행 중인
                    장애' 를 덧대면 명사가 겹쳐 "진행 중인 장애 진행 중인 장애 없음" 이 된다.
                    조사(를)도 붙이지 않는다: 제목이 무엇으로 끝날지 모르는데 을/를 을 고르면
                    틀리고, JSX 로 띄어 쓰면 "감지 를" 처럼 조사 앞이 벌어진다. */}
                {incident.active ? (
                  <p>
                    확인했습니다. 아래 항목 중 궁금한 것을 눌러 주세요 — 진행 중인 장애{' '}
                    <span className="font-semibold">{incident.title}</span> 기준으로 정리해
                    드립니다.
                  </p>
                ) : (
                  <p>
                    확인했습니다. 아래 항목 중 궁금한 것을 눌러 주세요 — 지금은 진행 중인 장애가
                    없어 <span className="font-semibold">{victim.device}</span> 현재 상태를 기준으로
                    정리해 드립니다.
                  </p>
                )}
                {/* 시드 대화가 깔려도 첫 화면에 칩이 보여야 한다(목업도 상단 카드 아래에 칩 줄이 있다). */}
                <QuickChips onPick={ask} disabled={acting} />
              </CardContent>
            </Card>
          </div>

          {messages.map((m, i) =>
            m.role === 'user' ? (
              <div key={`${m.at}-${i}`} className="flex items-end justify-end gap-2">
                {/* 목업의 사용자 말풍선은 연한 파랑 배경 + 진한 글씨다(검정 말풍선 아님). */}
                <div className="max-w-[85%] rounded-lg bg-metric-cpu/10 px-3 py-2 text-foreground text-sm">
                  {m.text}
                </div>
                <div className="flex shrink-0 flex-col items-start text-[10px] text-muted-foreground">
                  <span className="tabular-nums">{hhmm(m.at)}</span>
                  <span className="flex items-center gap-0.5">
                    <CheckCheck className="size-3" />
                    읽음
                  </span>
                </div>
              </div>
            ) : (
              <div key={`${m.at}-${i}`} className="flex gap-2">
                <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary/10">
                  <Bot className="size-4 text-primary" />
                </div>
                <Card className="max-w-[92%] flex-1 py-0">
                  <CardContent className="flex flex-col gap-3 p-3">
                    <p className="text-sm">{m.text}</p>
                    <AssistantCard msg={m} state={snapshot} />
                    {i === lastAssistant ? <QuickChips onPick={ask} disabled={acting} /> : null}
                  </CardContent>
                </Card>
              </div>
            ),
          )}
        </div>
      </div>

      {/* 입력 */}
      <form onSubmit={onSubmit} className="flex gap-2">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="메시지를 입력하세요…"
          className="h-9"
        />
        <Button type="submit" size="sm" className="h-9 shrink-0" disabled={!input.trim()}>
          <Send className="size-4" />
        </Button>
      </form>
    </div>
  );
}
