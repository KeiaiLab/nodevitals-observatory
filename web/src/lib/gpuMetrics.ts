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
  return gib >= 1 ? `${gib.toFixed(1)}GiB` : `${(v / 1024 ** 2).toFixed(0)}MiB`;
}

export function formatCount(v: number | null | undefined): string {
  return v == null || Number.isNaN(v) ? '—' : v.toLocaleString('ko-KR');
}
