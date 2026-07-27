// demoApi.ts — /api/v1/demo/* 클라이언트 + 타입 SSOT.
// 백엔드 internal/demo 의 Snapshot JSON 계약을 미러한다. Go 의 nil 슬라이스는
// null 로 직렬화되므로 모든 배열 필드는 수신 시 [] 로 정규화한다(방어 파싱).
import { type ApiEnvelope, apiPostRaw } from '@/lib/api';

export interface DemoStatus {
  enabled: boolean;
  ready: boolean;
  /** public demo — 조회 API 인증 해제 상태. 로그인/로그아웃 UI 를 숨기는 근거. */
  public: boolean;
  /** 재시딩 백필 진행 중 — 그동안 상태 조회가 잠깐 멎는다. */
  reseeding?: boolean;
}

export type ScenarioPhase =
  | 'normal'
  | 'degrading'
  | 'awaiting-approval'
  | 'draining'
  | 'replacing'
  | 'burnin-1'
  | 'burnin-failed'
  | 'burnin-2'
  | 'ready-to-return'
  | 'returned';

export type DemoAction =
  | 'approve-isolation'
  | 'start-burnin'
  | 'return-to-service'
  | 'reset'
  | 'register-idle-reason'
  | 'report-false-positive'
  | 'set-mode'
  | 'configure-burnin'
  | 'jump-phase'
  | 'ack-alert'
  | 'reseed';

/** 복구 개입 수준 — 시나리오 자동 전이 규칙을 실제로 바꾼다.
 *  observe=승인 대기에서 무한 정지 / approve=타임아웃 후 자동 승인(기본) /
 *  limited-auto=degrading 종료 시 승인 단계를 건너뛰고 즉시 격리. */
export type RemediationMode = 'observe' | 'approve' | 'limited-auto';

/** 모드 셀렉터 항목 — 하드코딩하지 않고 서버 목록으로 렌더한다. */
export interface ModeOption {
  id: RemediationMode;
  label: string;
  describe: string;
}

/** 단계 점프 목록 항목(서버가 순서대로 준다). */
export interface PhaseOption {
  id: ScenarioPhase;
  label: string;
  index: number;
}

/** CSP 정비 예정으로 자동복구를 보류 중인 노드 — 서버가 실제 플릿에서 고른다. */
export interface MaintenanceLock {
  instance: string;
  csp: string;
  window: string;
  reason: string;
}

/** 액션 요청 바디 — 액션별 필드의 합집합(전부 선택).
 *  demoContext 의 act() 도 이 타입을 그대로 받는다(좁히면 호출부가 객체 리터럴
 *  초과 속성 검사에 걸려 우회 래퍼가 필요해진다). */
export interface DemoActionBody {
  uuid?: string;
  reason?: string;
  note?: string;
  mode?: RemediationMode;
  phase?: ScenarioPhase;
  profile?: string;
  durationMin?: number;
  targetUtilPct?: number;
  /** ack-alert 대상 알림 ID. */
  id?: number;
  /** reseed 시드 — 생략하면 서버가 시각에서 파생한다(매번 새 플릿). */
  seed?: number;
}

export interface Deduction {
  code: string;
  message: string;
  penalty: number;
}

export interface HealthResult {
  score: number;
  deductions: Deduction[];
}

export interface StepState {
  key: 'cordon' | 'drain' | 'taint';
  label: string;
  status: 'pending' | 'running' | 'done';
}

export interface DrainState {
  active: boolean;
  progress: number;
  inflightTotal: number;
  inflight: number;
  podsTotal: number;
  podsEvicted: number;
  droppedRequests: number;
  steps: StepState[];
}

export interface CheckItem {
  id: string;
  label: string;
  status: 'pending' | 'running' | 'pass' | 'warn';
}

export interface BurninState {
  active: boolean;
  attempt: number;
  profile: string;
  /** 운영자 설정값 — configure-burnin 으로 저장하면 부하 곡선이 실제로 바뀐다. */
  targetUtilPct: number;
  /** 운영자 설정값 — 번인 단계 길이(분). */
  durationMin: number;
  progress: number;
  checklist: CheckItem[];
  verdict?: string;
}

export interface VictimState {
  instance: string;
  device: string;
  uuid: string;
  model: string;
  csp: string;
  cluster: string;
  pool: string;
  health: HealthResult;
  drain: DrainState;
  burnin: BurninState;
}

export interface ScenarioState {
  phase: ScenarioPhase;
  phaseIndex: number;
  phaseStartAt: number;
  phaseDeadline: number;
  /** false = 자동 전이 정지(관찰 모드 + 승인 대기) — 승인해야 진행된다. */
  autoAdvance: boolean;
  pendingAction?: DemoAction;
  /** 현재 복구 개입 수준 — 셀렉터의 선택 상태 진실(SSOT). */
  mode: RemediationMode;
  modeOptions: ModeOption[];
  /** 시연 제어용 단계 점프 목록(순서대로). */
  phases: PhaseOption[];
  /** CSP 정비 예정으로 자동복구를 보류 중인 노드(실제 플릿 개체). */
  maintenanceLocks: MaintenanceLock[];
  victim: VictimState;
}

export interface CSPSummary {
  id: string;
  display: string;
  adapter: 'native' | 'mock';
  gpuTotal: number;
  allocated: number;
  faulted: number;
  allocPct: number;
}

/** 상황판 한 칸 — tone 이 색을 정한다(라벨 문자열로 색을 추론하지 않는다). */
export interface StatusCount {
  key: string;
  label: string;
  count: number;
  tone: 'ok' | 'info' | 'warn' | 'major' | 'crit' | 'muted';
}

/** 통합 상황판 분류 집계 — 서버가 한 번만 분류해 화면끼리 숫자가 어긋나지 않게 한다. */
export interface Dashboard {
  alerts: StatusCount[];
  gpus: StatusCount[];
  nodes: StatusCount[];
  repair: StatusCount[];
  collect: StatusCount[];
}

/** 리전 — 공통 필터 차원. */
export interface RegionInfo {
  id: string;
  display: string;
  csp: string;
}

export interface MissingGPU {
  uuid: string;
  instance: string;
  device: string;
  csp: string;
  cluster: string;
  region: string;
}

export interface FleetSummary {
  gpuTotal: number;
  nodeTotal: number;
  allocated: number;
  faulted: number;
  tempAlerts: number;
  agentMissing: number;
  csps: CSPSummary[];
  models: Array<{ model: string; count: number }>;
  pools: Array<{ id: string; display: string; tenant: string; allocated: number }>;
  tenants: Array<{ id: string; display: string }>;
  faultUuids: string[];
  tempAlertUuids: string[];
  missingInstances: string[];
  /** Agent Missing 노드 소속 GPU — 벽면 '데이터 없음' 합성 셀의 근거(총 셀 = 인벤토리 전수 정합). */
  missingGpus: MissingGPU[];
  /** 공통 필터의 리전 차원. */
  regions: RegionInfo[];
}

export interface AlertEvent {
  /** 알림 고유 ID — 확인 처리 키(같은 시각에 여러 알림이 날 수 있어 at 은 식별자가 아니다). */
  id: number;
  at: number;
  severity: 'info' | 'warning' | 'major' | 'critical';
  code: string;
  title: string;
  instance?: string;
  device?: string;
  detail?: string;
  /** 운영자 확인 처리 여부 — 서버가 기억하므로 새로고침 후에도 유지된다. */
  acked?: boolean;
}

export interface AuditEntry {
  at: number;
  actor: string;
  action: string;
  target?: string;
  phaseFrom?: string;
  phaseTo?: string;
  evidence?: string;
  result: string;
}

export interface IdleGPU {
  uuid: string;
  instance: string;
  device: string;
  model: string;
  csp: string;
  pool: string;
  idleDays: number;
  reason?: string;
}

/** 서빙 풀 1개의 추론 서빙 상태 — 이 플랫폼의 워크로드가 GPU 추론 서빙임을
 *  드러내는 지표 묶음(요청·지연·토큰·큐·레플리카). 하드웨어 신호와 인과로
 *  연결된다: 사용률↑ → 배치↑ → 큐↑ → p95/p99↑ → (포화 시) 에러율↑. */
export interface ServingStats {
  pool: string;
  display: string;
  tenant: string;
  gpus: number;
  utilAvgPct: number;
  rps: number;
  p50Ms: number;
  p95Ms: number;
  p99Ms: number;
  /** 임베딩·비전·OCR·추천 풀은 0 — 토큰을 생성하지 않는 계열이다. */
  tokensPerSec: number;
  queueDepth: number;
  batchSize: number;
  errorRatePct: number;
  replicasReady: number;
  replicasDesired: number;
  /** 부하 90% 초과 — 지연 급등 구간. 화면 「포화」 배지의 근거. */
  saturated: boolean;
}

/** 가용 GPU 비율 SLO — 순간값만 있으면 모니터, 목표 대비 잔여 예산이 있어야
 *  운영 콘솔이 된다(NFR-04). */
export interface SLOState {
  /** 계약 목표(99.5). */
  targetPct: number;
  availabilityPct: number;
  /** 30일 오차 예산 잔여율. */
  errorBudgetRemainingPct: number;
  windowDays: number;
  mtbfHours: number;
  mttrMinutes: number;
  incidents30d: number;
  /** 현재 서비스 불가 GPU 수(장애 + 격리 + 수집중단). */
  unavailableGpus: number;
  breaching: boolean;
}

/** K8s 이벤트 1건 — 알림(AlertEvent)과 달리 조치 대상이 아닌 일상 기록이다. */
export interface ClusterEvent {
  at: number;
  type: 'Normal' | 'Warning';
  reason: string;
  object: string;
  namespace?: string;
  node?: string;
  message: string;
}

export interface DemoState {
  seed: number;
  cycle: number;
  generatedAt: number;
  timeScale: number;
  /** 재시딩 백필 진행 중 — 화면이 "새 데이터 생성 중"을 표시하는 근거. */
  reseeding?: boolean;
  fleet: FleetSummary;
  scenario: ScenarioState;
  alerts: AlertEvent[];
  audit: AuditEntry[];
  idle: IdleGPU[];
  /** 항상 전체 서빙 풀(8개) — 부분 집합이 아니다. */
  serving: ServingStats[];
  slo: SLOState;
  /** 최신순, 최대 60건. */
  events: ClusterEvent[];
  /** 통합 상황판 분류 집계. */
  dashboard: Dashboard;
}

export interface ActionResult {
  applied: boolean;
  phase: string;
  at: number;
  message: string;
}

export type ActionOutcome =
  | { ok: true; result: ActionResult }
  | { ok: false; status: number; error: string };

// ---- 정규화 (Go nil 슬라이스 → null 방어) ----

function arr<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : [];
}

/** slo 는 객체라 nil 슬라이스 규칙이 아닌 "필드 자체 부재"(구백엔드)만 방어한다 —
 *  화면이 slo.availabilityPct 를 직접 읽으므로 undefined 면 렌더가 깨진다. */
function normalizeSlo(raw: SLOState | null | undefined): SLOState {
  return {
    targetPct: raw?.targetPct ?? 0,
    availabilityPct: raw?.availabilityPct ?? 0,
    errorBudgetRemainingPct: raw?.errorBudgetRemainingPct ?? 0,
    windowDays: raw?.windowDays ?? 0,
    mtbfHours: raw?.mtbfHours ?? 0,
    mttrMinutes: raw?.mttrMinutes ?? 0,
    incidents30d: raw?.incidents30d ?? 0,
    unavailableGpus: raw?.unavailableGpus ?? 0,
    breaching: raw?.breaching ?? false,
  };
}

function normalizeState(raw: DemoState): DemoState {
  return {
    ...raw,
    fleet: {
      ...raw.fleet,
      csps: arr(raw.fleet?.csps),
      models: arr(raw.fleet?.models),
      pools: arr(raw.fleet?.pools),
      tenants: arr(raw.fleet?.tenants),
      faultUuids: arr(raw.fleet?.faultUuids),
      tempAlertUuids: arr(raw.fleet?.tempAlertUuids),
      missingInstances: arr(raw.fleet?.missingInstances),
      missingGpus: arr(raw.fleet?.missingGpus),
      regions: arr(raw.fleet?.regions),
    },
    scenario: {
      ...raw.scenario,
      modeOptions: arr(raw.scenario?.modeOptions),
      maintenanceLocks: arr(raw.scenario?.maintenanceLocks),
      phases: arr(raw.scenario?.phases),
      victim: {
        ...raw.scenario.victim,
        health: {
          score: raw.scenario.victim?.health?.score ?? 0,
          deductions: arr(raw.scenario.victim?.health?.deductions),
        },
        drain: { ...raw.scenario.victim?.drain, steps: arr(raw.scenario.victim?.drain?.steps) },
        burnin: {
          ...raw.scenario.victim?.burnin,
          checklist: arr(raw.scenario.victim?.burnin?.checklist),
        },
      },
    },
    alerts: arr(raw.alerts),
    audit: arr(raw.audit),
    idle: arr(raw.idle),
    serving: arr(raw.serving),
    slo: normalizeSlo(raw.slo),
    events: arr(raw.events),
    dashboard: {
      alerts: arr(raw.dashboard?.alerts),
      gpus: arr(raw.dashboard?.gpus),
      nodes: arr(raw.dashboard?.nodes),
      repair: arr(raw.dashboard?.repair),
      collect: arr(raw.dashboard?.collect),
    },
  };
}

// ---- API ----

/** 비인증 status — 404/네트워크 오류는 전부 "off" 로 수렴(구백엔드 호환). */
export async function fetchDemoStatus(): Promise<DemoStatus> {
  try {
    const res = await fetch('/api/v1/demo/status', { credentials: 'include' });
    if (!res.ok) return { enabled: false, ready: false, public: false };
    const body = (await res.json()) as ApiEnvelope<DemoStatus>;
    return {
      enabled: body.data?.enabled ?? false,
      ready: body.data?.ready ?? false,
      public: body.data?.public ?? false,
    };
  } catch {
    return { enabled: false, ready: false, public: false };
  }
}

export async function fetchDemoState(): Promise<DemoState> {
  const res = await fetch('/api/v1/demo/state', { credentials: 'include' });
  if (res.status === 401) {
    throw new Error('unauthorized');
  }
  if (!res.ok) {
    throw new Error(`demo state HTTP ${res.status}`);
  }
  const body = (await res.json()) as ApiEnvelope<DemoState>;
  return normalizeState(body.data);
}

/** 액션 실행 — 409(단계 불일치)는 예외가 아니라 결과로 낸다(시연 중 오조작
 *  안내를 UI 가 부드럽게 표시하기 위함). */
export async function runDemoAction(
  action: DemoAction,
  body?: DemoActionBody,
): Promise<ActionOutcome> {
  const res = await apiPostRaw(`/api/v1/demo/actions/${action}`, body);
  const payload = (await res.json().catch(() => null)) as
    | (ApiEnvelope<ActionResult> & { error?: string })
    | { error?: string }
    | null;
  if (res.ok && payload && 'data' in payload) {
    return { ok: true, result: payload.data };
  }
  return { ok: false, status: res.status, error: payload?.error ?? `HTTP ${res.status}` };
}

// ---- 문의 접수 (ContactBubble) ----

export interface ContactInput {
  name: string;
  org: string;
  email: string;
  message: string;
}

export interface ContactMessage {
  id: number;
  at: number;
  name: string;
  org?: string;
  email?: string;
  message: string;
}

export type ContactOutcome =
  | { ok: true; result: ContactMessage }
  | { ok: false; status: number; error: string };

/** 문의 접수 — 검증 실패(400)와 접수 폭주(429)를 예외가 아니라 결과로 낸다.
 *  방문자에게 "내가 잘못 썼다"와 "잠시 후 다시"를 구분해 보여줘야 한다. */
export async function submitContact(input: ContactInput): Promise<ContactOutcome> {
  const res = await apiPostRaw('/api/v1/demo/contact', input);
  const payload = (await res.json().catch(() => null)) as
    | (ApiEnvelope<ContactMessage> & { error?: string })
    | { error?: string }
    | null;
  if (res.ok && payload && 'data' in payload) {
    return { ok: true, result: payload.data };
  }
  return { ok: false, status: res.status, error: payload?.error ?? `HTTP ${res.status}` };
}

export const PHASE_LABELS: Record<ScenarioPhase, string> = {
  normal: '정상',
  degrading: '무음 열화 진행',
  'awaiting-approval': '격리 승인 대기',
  draining: 'Graceful Drain',
  replacing: '노드 교체 중',
  'burnin-1': '번인 검증 1차',
  'burnin-failed': '번인 실패 — 재검증 필요',
  'burnin-2': '번인 검증 2차',
  'ready-to-return': '재투입 대기',
  returned: '운영 풀 복귀',
};
