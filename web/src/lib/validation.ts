// validation.ts — 재사용 검증 상세(ValidationDetail) 타입 SSOT + 조회 클라이언트.
//
// `GET /api/v1/demo/validation/{uuid}` 가 GPU 1장의 사전검증 4항목·번인 진행·
// 실시간 로그를 낸다(백엔드 internal/demo/validation.go 계약 미러).
//
// 전역 스냅샷(/demo/state)에 싣지 않는 이유: 로그 200줄짜리 상세를 전 페이지가
// 2~5초로 폴링하는 스냅샷에 태울 수 없다. 노드 자산 대장(nodeAsset.ts)이 같은
// 이유로 이미 분리돼 있고, 이 파일은 그 선례를 그대로 따른다 — 404(없는 GPU)와
// 503(백필 중)은 화면 문구가 달라야 하므로 판정 가능한 kind 로 올린다.
//
// 폴링은 화면 몫이다: 진행 중(run.active)이면 2s, 아니면 15s 정도로
// usePolledQuery 에 태운다(스냅샷 폴링과 별개 주기).
import { type ApiEnvelope, UnauthorizedError } from '@/lib/api';

/** 검증 항목 1건(사전검증 4종 / 번인 7종). */
export interface ValidationItem {
  id: string;
  label: string;
  describe: string;
  status: 'pending' | 'running' | 'pass' | 'warn' | 'fail';
  /** 측정 결과("Driver 550.90.07"). */
  detail: string;
  /** 보조 문구("CUDA Driver API OK"). */
  detailSub: string;
  ranAt?: number;
  durationSec?: number;
}

/** 실시간 로그 1줄. */
export interface ValidationLog {
  at: number;
  level: 'INFO' | 'WARN' | 'ERROR';
  text: string;
}

/** 검증 스테이지 1회차. 집계 4종(total/passed/warned/failed)은 서버가 낸다 —
 *  화면이 다시 세면 검증 개요와 항목 표가 어긋난다. */
export interface ValidationRun {
  kind: 'precheck' | 'burnin';
  uuid: string;
  active: boolean;
  startedAt?: number;
  /** 0~1. */
  progress: number;
  items: ValidationItem[];
  log: ValidationLog[];
  verdict?: 'pass' | 'warn' | 'fail';
  total: number;
  passed: number;
  warned: number;
  failed: number;
}

/** 재사용 4단계 스테퍼의 한 칸. status 어휘는 GateDiagram 의 GateStageStatus 와
 *  같은 4종이다(새 상태 어휘를 만들지 않는다). */
export interface ReuseStep {
  id: string;
  label: string;
  status: 'done' | 'active' | 'blocked' | 'idle';
  hint: string;
}

/** 액션 1건의 실행 가능 여부 + 사유. 버튼 활성 조건의 진실은 서버 가드다 —
 *  화면이 같은 조건을 다시 유도하면(예: precheck.active 하나로만 막으면) 서버가
 *  거부하는 상태에서 버튼이 살아 있어 시연 중 빨간 배너가 뜬다. reason 은
 *  비활성 사유(버튼 아래 안내)이자 거부 시 배너 문구다. */
export interface ValidationGate {
  allowed: boolean;
  reason: string;
}

export interface ValidationDetail {
  uuid: string;
  instance: string;
  device: string;
  model: string;
  cluster: string;
  csp: string;
  idleDays: number;
  precheck: ValidationRun;
  burnin: ValidationRun;
  steps: ReuseStep[];
  precheckGate: ValidationGate;
  burninGate: ValidationGate;
  /** 교체 노드(victim) 시나리오 액션 2종 — 재번인 / 운영 풀 재투입. 단계가
   *  진실이라 대상 GPU 와 무관하다(액션도 uuid 를 받지 않는다). */
  restartBurninGate: ValidationGate;
  returnGate: ValidationGate;
}

// ---- 실패 분류 (nodeAsset.ts 와 같은 규약) ----

export type ValidationFailure = 'not-found' | 'backfilling' | 'error';

export class ValidationError extends Error {
  readonly kind: ValidationFailure;
  readonly status: number;

  constructor(kind: ValidationFailure, status: number) {
    super(`validation: ${kind} (HTTP ${status})`);
    this.name = 'ValidationError';
    this.kind = kind;
    this.status = status;
  }
}

const FAILURE_TEXT: Record<ValidationFailure, string> = {
  'not-found': '검증 대상 GPU 를 찾을 수 없다',
  backfilling: '데이터 준비 중 — 잠시 후 재시도',
  error: '검증 상세를 불러오지 못했다',
};

/** 조회 실패의 사용자 문구. 401 은 로그인 화면 전환이 처리하므로 null. */
export function validationMessage(err: unknown): string | null {
  if (err instanceof UnauthorizedError) return null;
  if (err instanceof ValidationError) return FAILURE_TEXT[err.kind];
  return FAILURE_TEXT.error;
}

// ---- 방어 파싱 (Go 의 nil 슬라이스는 null 로 직렬화된다) ----

function arr<T>(v: readonly T[] | null | undefined): T[] {
  return Array.isArray(v) ? [...v] : [];
}

function normalizeRun(
  raw: Partial<ValidationRun> | null | undefined,
  kind: ValidationRun['kind'],
): ValidationRun {
  return {
    kind: raw?.kind ?? kind,
    uuid: raw?.uuid ?? '',
    active: raw?.active === true,
    startedAt: raw?.startedAt ?? 0,
    progress: raw?.progress ?? 0,
    items: arr(raw?.items),
    log: arr(raw?.log),
    verdict: raw?.verdict,
    total: raw?.total ?? 0,
    passed: raw?.passed ?? 0,
    warned: raw?.warned ?? 0,
    failed: raw?.failed ?? 0,
  };
}

/** 게이트 부재는 "실행 불가"로 읽는다 — 판단 근거가 없는데 버튼을 열면
 *  서버가 거부하는 조작을 화면이 권하는 꼴이 된다(fail-closed). */
function normalizeGate(raw: Partial<ValidationGate> | null | undefined): ValidationGate {
  return { allowed: raw?.allowed === true, reason: raw?.reason ?? '' };
}

function normalizeDetail(raw: Partial<ValidationDetail> | null | undefined): ValidationDetail {
  const src = raw ?? {};
  return {
    uuid: src.uuid ?? '',
    instance: src.instance ?? '',
    device: src.device ?? '',
    model: src.model ?? '',
    cluster: src.cluster ?? '',
    csp: src.csp ?? '',
    idleDays: src.idleDays ?? 0,
    precheck: normalizeRun(src.precheck, 'precheck'),
    burnin: normalizeRun(src.burnin, 'burnin'),
    steps: arr(src.steps),
    precheckGate: normalizeGate(src.precheckGate),
    burninGate: normalizeGate(src.burninGate),
    restartBurninGate: normalizeGate(src.restartBurninGate),
    returnGate: normalizeGate(src.returnGate),
  };
}

// ---- 조회 ----

/** GET /api/v1/demo/validation/{uuid} — 없는 GPU 404, 백필 중 503.
 *  미실행 GPU 는 404 가 아니라 200 + 전 항목 pending 이다. */
export async function fetchValidation(uuid: string): Promise<ValidationDetail> {
  const path = `/api/v1/demo/validation/${encodeURIComponent(uuid)}`;
  const res = await fetch(path, { credentials: 'include' });
  if (res.status === 401) throw new UnauthorizedError(path);
  if (res.status === 404) throw new ValidationError('not-found', 404);
  if (res.status === 503) throw new ValidationError('backfilling', 503);
  if (!res.ok) throw new ValidationError('error', res.status);
  const body = (await res.json()) as ApiEnvelope<Partial<ValidationDetail>>;
  return normalizeDetail(body.data);
}
