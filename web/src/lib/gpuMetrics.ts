// gpuMetrics.ts — GPU 메트릭 이름·대역·색·포맷의 SSOT.
//
// 메트릭 이름은 실 에이전트(nodevitals)의 exposition 계약(`nodevitals_hw_` 접두)
// 이다 — 기존 3페이지가 접두 없는 이름(`nodevitals_gpu_*`)을 질의해 라이브에서
// GPU 데이터가 항상 비어 있던 결함의 수리 지점. 데모 엔진도 같은 이름을 emit
// 하므로 데모/실서비스가 이 상수 하나를 공유한다.

export const METRIC_GPU_UTIL = 'nodevitals_hw_gpu_utilization_pct';
export const METRIC_GPU_MEM_USED = 'nodevitals_hw_gpu_mem_used_bytes';
export const METRIC_GPU_MEM_TOTAL = 'nodevitals_hw_gpu_mem_total_bytes';
export const METRIC_GPU_TEMP = 'nodevitals_hw_gpu_temperature_celsius';
export const METRIC_GPU_POWER = 'nodevitals_hw_gpu_power_watts';
export const METRIC_GPU_ECC_CORR = 'nodevitals_hw_gpu_ecc_corrected_total';
export const METRIC_GPU_ECC_UNC = 'nodevitals_hw_gpu_ecc_uncorrected_total';

// 데모 엔진의 사전 집계 시리즈 (서버 집계 부재 보완 — demo 인스턴스에서만 존재).
export const METRIC_DEMO_FLEET_TOTAL = 'nodevitals_demo_fleet_gpu_total';
export const METRIC_DEMO_FLEET_ALLOCATED = 'nodevitals_demo_fleet_gpu_allocated';
export const METRIC_DEMO_FLEET_ALLOC_PCT = 'nodevitals_demo_fleet_alloc_pct';
export const METRIC_DEMO_FLEET_UTIL_AVG = 'nodevitals_demo_fleet_util_avg_pct';
export const METRIC_DEMO_FLEET_FAULTED = 'nodevitals_demo_fleet_faulted';
export const METRIC_DEMO_FLEET_TEMP_ALERTS = 'nodevitals_demo_fleet_temp_alerts';
export const METRIC_DEMO_FLEET_POWER_KW = 'nodevitals_demo_fleet_power_kw';
export const METRIC_DEMO_CSP_UTIL_AVG = 'nodevitals_demo_csp_util_avg_pct';
export const METRIC_DEMO_CSP_TOTAL = 'nodevitals_demo_csp_gpu_total';
export const METRIC_DEMO_CSP_ALLOCATED = 'nodevitals_demo_csp_gpu_allocated';
export const METRIC_DEMO_CSP_TEMP_MAX = 'nodevitals_demo_csp_temp_max_celsius';
export const METRIC_DEMO_CSP_FAULTED = 'nodevitals_demo_csp_faulted';
export const METRIC_DEMO_CLUSTER_UTIL_AVG = 'nodevitals_demo_cluster_util_avg_pct';
export const METRIC_DEMO_VICTIM_HEALTH = 'nodevitals_demo_victim_health_score';

// 추론 서빙 계층 — 풀 단위(라벨 `pool`,`tenant`). 등호 매처만 지원하므로
// selector() 로 조합한다. 하드웨어 지표와 인과로 이어진 시리즈들이다:
// 사용률↑ → 배치↑ → 큐↑ → p95/p99↑ → (포화 시) 에러율↑.
export const METRIC_DEMO_POOL_RPS = 'nodevitals_demo_pool_rps';
export const METRIC_DEMO_POOL_P50 = 'nodevitals_demo_pool_latency_p50_ms';
export const METRIC_DEMO_POOL_P95 = 'nodevitals_demo_pool_latency_p95_ms';
export const METRIC_DEMO_POOL_P99 = 'nodevitals_demo_pool_latency_p99_ms';
export const METRIC_DEMO_POOL_TOKENS = 'nodevitals_demo_pool_tokens_per_sec';
export const METRIC_DEMO_POOL_QUEUE = 'nodevitals_demo_pool_queue_depth';
export const METRIC_DEMO_POOL_BATCH = 'nodevitals_demo_pool_batch_size';
export const METRIC_DEMO_POOL_ERR_PCT = 'nodevitals_demo_pool_error_rate_pct';
export const METRIC_DEMO_POOL_REPLICAS_READY = 'nodevitals_demo_pool_replicas_ready';
export const METRIC_DEMO_POOL_REPLICAS_DESIRED = 'nodevitals_demo_pool_replicas_desired';

// 서빙 플릿 롤업 (라벨 없음) — 지연·에러율은 요청량 가중 평균.
export const METRIC_DEMO_FLEET_RPS = 'nodevitals_demo_fleet_rps';
export const METRIC_DEMO_FLEET_P95 = 'nodevitals_demo_fleet_latency_p95_ms';
export const METRIC_DEMO_FLEET_TOKENS = 'nodevitals_demo_fleet_tokens_per_sec';
export const METRIC_DEMO_FLEET_ERR_PCT = 'nodevitals_demo_fleet_error_rate_pct';

// SLO (라벨 없음) — 가용 GPU 비율 목표 대비 잔여 예산.
export const METRIC_DEMO_SLO_AVAILABILITY = 'nodevitals_demo_slo_gpu_availability_pct';
export const METRIC_DEMO_SLO_TARGET = 'nodevitals_demo_slo_target_pct';
export const METRIC_DEMO_SLO_ERROR_BUDGET = 'nodevitals_demo_slo_error_budget_remaining_pct';
export const METRIC_DEMO_SLO_MTBF = 'nodevitals_demo_slo_mtbf_hours';
export const METRIC_DEMO_SLO_MTTR = 'nodevitals_demo_slo_mttr_minutes';
export const METRIC_DEMO_SLO_INCIDENTS_30D = 'nodevitals_demo_slo_incidents_30d';

/** 등호 매처 셀렉터 빌더 — 서버 파서는 `name{k="v"}` 등호 매처만 지원한다
 *  (정규식·집계는 400). 값의 `\`·`"` 는 서버 scanQuotedValue 규칙대로 이스케이프. */
export function selector(metric: string, labels?: Record<string, string>): string {
  const entries = Object.entries(labels ?? {}).filter(([, v]) => v !== '');
  if (entries.length === 0) return metric;
  const body = entries
    .map(([k, v]) => `${k}="${v.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`)
    .join(',');
  return `${metric}{${body}}`;
}

// ---- 사용률 5단 대역 (Idle / <30 / <80 / <95 / ≥95) ----

export interface UtilBand {
  /** index.css 의 --util-band-N 토큰 인덱스 */
  index: 0 | 1 | 2 | 3 | 4;
  label: string;
  /** 이 대역의 하한(%) — bandForUtil 판정용 */
  min: number;
}

export const UTIL_BANDS: readonly UtilBand[] = [
  { index: 0, label: 'Idle (<5%)', min: 0 },
  { index: 1, label: '<30%', min: 5 },
  { index: 2, label: '<80%', min: 30 },
  { index: 3, label: '<95%', min: 80 },
  { index: 4, label: '≥95%', min: 95 },
] as const;

export function bandForUtil(pct: number): UtilBand {
  for (let i = UTIL_BANDS.length - 1; i >= 0; i--) {
    if (pct >= UTIL_BANDS[i].min) return UTIL_BANDS[i];
  }
  return UTIL_BANDS[0];
}

// ---- 지표 고정색 (Sentinel 계열 관제 규약 계승) ----
// GPU=자홍, 온도·전력=주황, CPU=파랑, 메모리=보라, POD=초록, 장애=빨강.
// 실색은 index.css 의 --metric-* 토큰(라이트/다크 동일 hue)이 정의한다.

export const METRIC_COLORS = {
  gpu: 'var(--metric-gpu)',
  thermal: 'var(--metric-thermal)',
  cpu: 'var(--metric-cpu)',
  mem: 'var(--metric-mem)',
  pod: 'var(--metric-pod)',
  fault: 'var(--metric-fault)',
} as const;

// ---- 포맷터 ----

export function formatPct(v: number | null | undefined, digits = 1): string {
  return v == null || Number.isNaN(v) ? '—' : `${v.toFixed(digits)}%`;
}

export function formatCelsius(v: number | null | undefined): string {
  return v == null || Number.isNaN(v) ? '—' : `${v.toFixed(1)}°C`;
}

export function formatWatts(v: number | null | undefined): string {
  return v == null || Number.isNaN(v) ? '—' : `${Math.round(v)}W`;
}

export function formatBytes(v: number | null | undefined): string {
  if (v == null || Number.isNaN(v)) return '—';
  const gib = v / 1024 ** 3;
  if (gib >= 1) return `${gib.toFixed(1)}GiB`;
  // 1MiB 미만은 KiB 로 — 장애 보고서(수 KB) 같은 작은 값이 "0MiB"로 죽는 것을 막는다.
  const mib = v / 1024 ** 2;
  return mib >= 1 ? `${mib.toFixed(0)}MiB` : `${(v / 1024).toFixed(1)}KiB`;
}

export function formatCount(v: number | null | undefined): string {
  return v == null || Number.isNaN(v) ? '—' : v.toLocaleString('ko-KR');
}

/** 지연 — 정수 ms(천단위 구분). 소수 자리는 관제 판단에 무의미하다. */
export function formatMs(v: number | null | undefined): string {
  return v == null || Number.isNaN(v) ? '—' : `${Math.round(v).toLocaleString('ko-KR')}ms`;
}

/** 초당 요청 — 100 미만은 소수 1자리(작은 풀의 변동을 죽이지 않기 위해). */
export function formatRps(v: number | null | undefined): string {
  if (v == null || Number.isNaN(v)) return '—';
  return v < 100
    ? v.toFixed(1)
    : Math.round(v).toLocaleString('ko-KR');
}

/** 토큰 생성 속도 — 1000 이상은 k 표기(12.3k tok/s). */
export function formatTokensPerSec(v: number | null | undefined): string {
  if (v == null || Number.isNaN(v)) return '—';
  return v >= 1000 ? `${(v / 1000).toFixed(1)}k tok/s` : `${Math.round(v)} tok/s`;
}
