// useLiveSeries — 상황판 상단 실시간 스트립의 시계열 공급자.
//
// 한 번의 폴링에서 플릿 롤업 6종을 range 질의로 받아 스파크라인에 그대로
// 먹인다. instant 값만 있으면 "지금 얼마"는 알아도 "오르는 중인지 내리는
// 중인지"를 모른다 — 관제 화면에서 추세가 없는 숫자는 절반짜리다.
//
// 서버 셀렉터는 등호 매처만 지원하고 집계 함수가 없으므로, 여기서 쓰는 것은
// 전부 엔진이 미리 집계해 둔 라벨 없는 롤업 시리즈다.
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type MatrixData, type ApiEnvelope } from '@/lib/api';
import {
  METRIC_DEMO_FLEET_ERR_PCT,
  METRIC_DEMO_FLEET_P95,
  METRIC_DEMO_FLEET_POWER_KW,
  METRIC_DEMO_FLEET_RPS,
  METRIC_DEMO_FLEET_TOKENS,
  METRIC_DEMO_FLEET_UTIL_AVG,
} from '@/lib/gpuMetrics';

/** 되돌아볼 구간(초) — 30분. 15s 간격이라 시리즈당 ~120점으로, 스파크라인
 *  폭(100 뷰박스)에 딱 맞고 응답도 가볍다. */
const WINDOW_SEC = 1800;
const STEP_SEC = 15;
const POLL_MS = 15_000;

export type LiveMetricKey = 'util' | 'rps' | 'p95' | 'tokens' | 'errPct' | 'powerKw';

const SELECTORS: Record<LiveMetricKey, string> = {
  util: METRIC_DEMO_FLEET_UTIL_AVG,
  rps: METRIC_DEMO_FLEET_RPS,
  p95: METRIC_DEMO_FLEET_P95,
  tokens: METRIC_DEMO_FLEET_TOKENS,
  errPct: METRIC_DEMO_FLEET_ERR_PCT,
  powerKw: METRIC_DEMO_FLEET_POWER_KW,
};

export interface LiveSeries {
  values: number[];
  /** 마지막 값 — 큰 숫자로 보여주는 "지금". */
  last: number | null;
  /** 구간 시작 대비 변화량. 추세 배지의 근거. */
  delta: number | null;
}

export type LiveSeriesMap = Record<LiveMetricKey, LiveSeries>;

const EMPTY: LiveSeries = { values: [], last: null, delta: null };

function toSeries(res: ApiEnvelope<MatrixData>): LiveSeries {
  const pairs = res.data.result[0]?.values ?? [];
  const values = pairs
    .map(([, raw]) => Number(raw))
    .filter((v) => Number.isFinite(v));
  if (values.length === 0) return EMPTY;
  const last = values[values.length - 1];
  return { values, last, delta: values.length > 1 ? last - values[0] : null };
}

/** demoMode 가 확정(true)일 때만 질의한다 — 실서비스에는 이 시리즈가 없다. */
export function useLiveSeries(demoMode: boolean | null) {
  return usePolledQuery<LiveSeriesMap | null>(
    async () => {
      if (demoMode !== true) return null;
      const end = Math.floor(Date.now() / 1000);
      const start = end - WINDOW_SEC;
      const keys = Object.keys(SELECTORS) as LiveMetricKey[];
      const results = await Promise.all(
        keys.map((k) => api.rangeQuery(SELECTORS[k], start, end, STEP_SEC)),
      );
      const out = {} as LiveSeriesMap;
      keys.forEach((k, i) => {
        out[k] = toSeries(results[i]);
      });
      return out;
    },
    POLL_MS,
    [demoMode],
  );
}
