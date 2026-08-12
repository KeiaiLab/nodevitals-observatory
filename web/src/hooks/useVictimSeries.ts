// useVictimSeries — demo3 GPU Assistant 의 "주요 지표" 미니차트 공급자.
//
// 장애 대상 GPU(victim) 한 장의 온도·ECC·사용률을 range 질의로 받아 미니차트에
// 먹인다. 상황판 전체 롤업을 보는 useLiveSeries 와 달리, 여기서는 gpu_uuid 등호
// 매처로 그 한 장의 시계열만 좁힌다(서버는 등호 매처만 지원 — selector() 로 조합).
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type ApiEnvelope, type MatrixData } from '@/lib/api';
import { METRIC_GPU_ECC_UNC, METRIC_GPU_TEMP, METRIC_GPU_UTIL, selector } from '@/lib/gpuMetrics';

// 30분 창, 15s 간격 → 시리즈당 ~120점. 미니차트 폭에 맞고 응답도 가볍다.
const WINDOW_SEC = 1800;
const STEP_SEC = 15;
const POLL_MS = 15_000;

export type VictimMetricKey = 'temp' | 'ecc' | 'util';

const SELECTORS: Record<VictimMetricKey, string> = {
  temp: METRIC_GPU_TEMP,
  ecc: METRIC_GPU_ECC_UNC,
  util: METRIC_GPU_UTIL,
};

export interface VictimSeries {
  values: number[];
  /** 각 값의 표본 시각(epoch 초) — 차트 x축 눈금과 장애 시점 마커의 기준.
   *  values 와 같은 길이·같은 순서다. */
  times: number[];
  /** 마지막 값 — 큰 숫자로 보여주는 "지금". */
  last: number | null;
}

export type VictimSeriesMap = Record<VictimMetricKey, VictimSeries>;

const EMPTY: VictimSeries = { values: [], times: [], last: null };

function toSeries(res: ApiEnvelope<MatrixData>): VictimSeries {
  const pairs = res.data.result[0]?.values ?? [];
  const values: number[] = [];
  const times: number[] = [];
  for (const [ts, raw] of pairs) {
    const v = Number(raw);
    // 결측(NaN)은 값과 시각을 함께 버린다 — 한쪽만 버리면 두 배열의 짝이 어긋난다.
    if (!Number.isFinite(v)) continue;
    values.push(v);
    times.push(ts);
  }
  if (values.length === 0) return EMPTY;
  return { values, times, last: values[values.length - 1] };
}

/** victim GPU 의 uuid 로 온도·ECC·사용률 시계열을 폴링한다. uuid 가 비었거나
 *  데모 모드가 아니면 질의하지 않는다(실서비스에는 이 시나리오가 없다). */
export function useVictimSeries(uuid: string | undefined, demoMode: boolean | null) {
  return usePolledQuery<VictimSeriesMap | null>(
    async () => {
      if (demoMode !== true || !uuid) return null;
      const end = Math.floor(Date.now() / 1000);
      const start = end - WINDOW_SEC;
      const keys = Object.keys(SELECTORS) as VictimMetricKey[];
      const results = await Promise.all(
        keys.map((k) => api.rangeQuery(selector(SELECTORS[k], { gpu_uuid: uuid }), start, end, STEP_SEC)),
      );
      const out = {} as VictimSeriesMap;
      keys.forEach((k, i) => {
        out[k] = toSeries(results[i]);
      });
      return out;
    },
    POLL_MS,
    [uuid, demoMode],
  );
}
