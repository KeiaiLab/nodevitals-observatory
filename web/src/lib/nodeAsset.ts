// nodeAsset.ts — 노드 자산 대장(NodeAsset) 타입 SSOT + 조회 클라이언트.
//
// 드릴다운이 사용률 숫자만 보이면 관제 화면이 아니라 그래프 위젯이다. 실제
// 운영자는 노드를 열면 드라이버·펌웨어·시리얼·MIG·PCIe·물리 위치(랙/PDU)·
// K8s 라벨/테인트를 본다 — 그 대장을 `GET /api/v1/demo/node/{instance}` 가
// 낸다(백엔드 internal/demo/asset.go 계약 미러).
//
// 자산은 노드 수명 동안 사실상 불변이라 **폴링하지 않는다** — instance 당 1회.
// 실시간 지표(util/temp/power/mem)만 부모 시트가 15초 폴링한다.
//
// fetch 관례는 api.ts 를 따른다(credentials:'include' · 401 = UnauthorizedError).
// 단 404(없는 노드)와 503(백필 중)은 화면 문구가 달라야 하므로 문자열 예외가
// 아니라 판정 가능한 kind 로 올린다.
import { type ApiEnvelope, UnauthorizedError } from '@/lib/api';

/** GPU 1장의 자산 정보(Go GPUAsset 미러). */
export interface GpuAsset {
  uuid: string;
  device: string;
  model: string;
  serial: string;
  vbios: string;
  driverVersion: string;
  cudaVersion: string;
  /** "비활성" 또는 "1g.10gb × 7" 형태. */
  migProfile: string;
  /** "Gen4" / "Gen5". */
  pcieGen: string;
  /** "x16". */
  pcieWidth: string;
  /** 0 이면 NVLink 미탑재 모델(L40S 등). */
  nvlinkLinks: number;
  /** NVLINK_OK 가 아니면 링크 저하 — UI 는 주황으로 강조한다. */
  nvlinkStatus: string;
  pool: string;
  tenant: string;
  allocated: boolean;
  /** 할당 시의 파드명(미할당이면 없음). */
  pod?: string;
}

/** 노드 자산 + 소속 GPU 자산(Go NodeAsset 미러). */
export interface NodeAsset {
  instance: string;
  csp: string;
  cluster: string;
  /** 물리 랙 — 냉각·전원을 공유하는 단위(동시 장애 반경의 근거). */
  rack: string;
  /** 전원 분기 "A" | "B". */
  pdu: string;
  model: string;
  gpuCount: number;
  kubeletVersion: string;
  containerRuntime: string;
  osImage: string;
  kernelVersion: string;
  /** "key=value" 형태 문자열 배열(값 없는 라벨은 "key="). */
  labels: string[];
  /** 없으면 빈 배열. */
  taints: string[];
  serialNumber: string;
  /** "2024-03-11" (UTC 날짜). */
  installedAt: string;
  ageDays: number;
  warrantyEnds: string;
  ready: boolean;
  cordoned: boolean;
  agentMissing: boolean;
  gpus: GpuAsset[];
}

/** NVLink 정상 상태 문자열 — 이 값이 아니면 저하로 간주한다. */
export const NVLINK_OK = '정상';

/** 보증 만료 임박 판정 창(일). 이 안에 들면 교체 품의 리드타임이 시작된다. */
export const WARRANTY_WARN_DAYS = 180;

// ---- 실패 분류 ----

export type NodeAssetFailure = 'not-found' | 'backfilling' | 'error';

export class NodeAssetError extends Error {
  readonly kind: NodeAssetFailure;
  readonly status: number;

  constructor(kind: NodeAssetFailure, status: number) {
    super(`nodeAsset: ${kind} (HTTP ${status})`);
    this.name = 'NodeAssetError';
    this.kind = kind;
    this.status = status;
  }
}

const FAILURE_TEXT: Record<NodeAssetFailure, string> = {
  'not-found': '노드 정보를 찾을 수 없다',
  backfilling: '데이터 준비 중 — 잠시 후 재시도',
  error: '노드 자산을 불러오지 못했다',
};

/** 자산 조회 실패의 사용자 문구. 401 은 로그인 화면 전환이 처리하므로 null 을
 *  낸다 — 시트에 오류 문구를 겹쳐 그리지 않는다(usePolledQuery 와 같은 규약). */
export function nodeAssetMessage(err: unknown): string | null {
  if (err instanceof UnauthorizedError) return null;
  if (err instanceof NodeAssetError) return FAILURE_TEXT[err.kind];
  return FAILURE_TEXT.error;
}

// ---- 방어 파싱 (Go 의 nil 슬라이스는 null 로 직렬화된다) ----

function arr<T>(v: readonly T[] | null | undefined): T[] {
  return Array.isArray(v) ? [...v] : [];
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

function num(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : 0;
}

function normalizeGpu(raw: Partial<GpuAsset>): GpuAsset {
  const gpu: GpuAsset = {
    uuid: str(raw.uuid),
    device: str(raw.device),
    model: str(raw.model),
    serial: str(raw.serial),
    vbios: str(raw.vbios),
    driverVersion: str(raw.driverVersion),
    cudaVersion: str(raw.cudaVersion),
    migProfile: str(raw.migProfile),
    pcieGen: str(raw.pcieGen),
    pcieWidth: str(raw.pcieWidth),
    nvlinkLinks: num(raw.nvlinkLinks),
    nvlinkStatus: str(raw.nvlinkStatus),
    pool: str(raw.pool),
    tenant: str(raw.tenant),
    allocated: raw.allocated === true,
  };
  // pod 는 omitempty — 빈 값이면 키 자체를 만들지 않는다(옵셔널 계약 유지).
  const pod = str(raw.pod);
  if (pod !== '') gpu.pod = pod;
  return gpu;
}

function normalizeAsset(raw: Partial<NodeAsset> | null | undefined): NodeAsset {
  const src = raw ?? {};
  return {
    instance: str(src.instance),
    csp: str(src.csp),
    cluster: str(src.cluster),
    rack: str(src.rack),
    pdu: str(src.pdu),
    model: str(src.model),
    gpuCount: num(src.gpuCount),
    kubeletVersion: str(src.kubeletVersion),
    containerRuntime: str(src.containerRuntime),
    osImage: str(src.osImage),
    kernelVersion: str(src.kernelVersion),
    labels: arr(src.labels),
    taints: arr(src.taints),
    serialNumber: str(src.serialNumber),
    installedAt: str(src.installedAt),
    ageDays: num(src.ageDays),
    warrantyEnds: str(src.warrantyEnds),
    ready: src.ready === true,
    cordoned: src.cordoned === true,
    agentMissing: src.agentMissing === true,
    gpus: arr(src.gpus).map((g) => normalizeGpu(g)),
  };
}

// ---- 조회 ----

/** GET /api/v1/demo/node/{instance} — 없는 노드 404, 백필 중 503. */
export async function fetchNodeAsset(instance: string): Promise<NodeAsset> {
  const path = `/api/v1/demo/node/${encodeURIComponent(instance)}`;
  const res = await fetch(path, { credentials: 'include' });
  if (res.status === 401) throw new UnauthorizedError(path);
  if (res.status === 404) throw new NodeAssetError('not-found', 404);
  if (res.status === 503) throw new NodeAssetError('backfilling', 503);
  if (!res.ok) throw new NodeAssetError('error', res.status);
  const body = (await res.json()) as ApiEnvelope<Partial<NodeAsset>>;
  return normalizeAsset(body.data);
}

// ---- 파생 헬퍼 ----

/** 보증 만료까지 남은 일수(음수 = 이미 만료). 파싱 실패는 null — 배지 판정 제외. */
export function warrantyDaysLeft(warrantyEnds: string, now: Date = new Date()): number | null {
  const end = Date.parse(`${warrantyEnds}T00:00:00Z`);
  if (Number.isNaN(end)) return null;
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  return Math.round((end - today) / 86_400_000);
}

export interface ParsedLabel {
  key: string;
  value: string;
}

/** "k=v" 라벨 1건 파싱. 값이 없는 라벨("node-role...=")은 value 가 빈 문자열. */
export function parseLabel(raw: string): ParsedLabel {
  const at = raw.indexOf('=');
  if (at < 0) return { key: raw, value: '' };
  return { key: raw.slice(0, at), value: raw.slice(at + 1) };
}

/** "Gen4 x16" — PCIe 링크 표기(둘 중 하나라도 비면 있는 쪽만). */
export function formatPcie(gen: string, width: string): string {
  const parts = [gen, width].filter((p) => p !== '');
  return parts.length === 0 ? '—' : parts.join(' ');
}

/** NVLink 표기 — 0 링크는 미탑재 모델(L40S 등)이다. */
export function formatNvlink(links: number, status: string): string {
  if (links <= 0) return '미탑재';
  return status === '' ? `${links}링크` : `${links}링크 · ${status}`;
}
