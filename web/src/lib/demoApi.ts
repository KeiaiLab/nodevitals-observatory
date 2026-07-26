// demoApi.ts — /api/v1/demo/* 클라이언트 + 타입 SSOT.
// 백엔드 internal/demo 의 Snapshot JSON 계약을 미러한다. Go 의 nil 슬라이스는
// null 로 직렬화되므로 모든 배열 필드는 수신 시 [] 로 정규화한다(방어 파싱).
import { type ApiEnvelope, apiPostRaw } from '@/lib/api';

export interface DemoStatus {
  enabled: boolean;
  ready: boolean;
  /** public demo — 조회 API 인증 해제 상태. 로그인/로그아웃 UI 를 숨기는 근거. */
  public: boolean;
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
  | 'report-false-positive';

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
  targetUtilPct: number;
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
  autoAdvance: boolean;
  pendingAction?: DemoAction;
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

export interface MissingGPU {
  uuid: string;
  instance: string;
  device: string;
  csp: string;
  cluster: string;
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
}

export interface AlertEvent {
  at: number;
  severity: 'info' | 'warning' | 'critical';
  code: string;
  title: string;
  instance?: string;
  device?: string;
  detail?: string;
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

export interface DemoState {
  seed: number;
  cycle: number;
  generatedAt: number;
  timeScale: number;
  fleet: FleetSummary;
  scenario: ScenarioState;
  alerts: AlertEvent[];
  audit: AuditEntry[];
  idle: IdleGPU[];
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
    },
    scenario: {
      ...raw.scenario,
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
  body?: { uuid?: string; reason?: string; note?: string },
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

// ---- 6-Step 시연 동선 (DemoRail 라우팅 표) ----

export interface DemoStep {
  step: number;
  label: string;
  route: string;
  phases: ScenarioPhase[]; // 이 스텝이 조명하는 시나리오 단계(리모컨 하이라이트)
}

export const DEMO_STEPS: readonly DemoStep[] = [
  { step: 1, label: '통합관제', route: '/gpu', phases: ['normal'] },
  { step: 2, label: '감지', route: '/gpu/health', phases: ['degrading', 'awaiting-approval'] },
  { step: 3, label: '분석', route: '/gpu/roadmap', phases: [] },
  { step: 4, label: '격리', route: '/gpu/remediation', phases: ['draining', 'replacing'] },
  {
    step: 5,
    label: '검증',
    route: '/gpu/validation',
    phases: ['burnin-1', 'burnin-failed', 'burnin-2', 'ready-to-return', 'returned'],
  },
  { step: 6, label: '효율', route: '/gpu/efficiency', phases: [] },
] as const;

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
