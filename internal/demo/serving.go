package demo

// 서빙 계층 — 이 플랫폼의 워크로드는 GPU **추론 서빙**이다. 하드웨어 지표
// (util/temp/power)만 있으면 "장비 모니터"로 보이지, 운영 중인 추론 서비스
// 관제로 보이지 않는다. 여기서 풀(서빙 풀) 단위의 요청·지연·토큰·큐를 만들되,
// 하드웨어 신호와 **인과로 연결**한다:
//
//	GPU 사용률 ↑ → 배치 크기 ↑ → 큐 깊이 ↑ → p95/p99 지연 ↑ → (포화 시) 에러율 ↑
//
// 그래서 히트맵이 붉어질 때 서빙 지연도 함께 올라간다 — 두 화면이 서로 다른
// 이야기를 하면 즉시 가짜로 보인다.

import (
	"math"
	"strconv"
)

const (
	metricPoolRPS       = "nodevitals_demo_pool_rps"
	metricPoolP50       = "nodevitals_demo_pool_latency_p50_ms"
	metricPoolP95       = "nodevitals_demo_pool_latency_p95_ms"
	metricPoolP99       = "nodevitals_demo_pool_latency_p99_ms"
	metricPoolTokens    = "nodevitals_demo_pool_tokens_per_sec"
	metricPoolQueue     = "nodevitals_demo_pool_queue_depth"
	metricPoolBatch     = "nodevitals_demo_pool_batch_size"
	metricPoolErrPct    = "nodevitals_demo_pool_error_rate_pct"
	metricPoolReplicas  = "nodevitals_demo_pool_replicas_ready"
	metricPoolReplicasD = "nodevitals_demo_pool_replicas_desired"

	metricFleetRPS    = "nodevitals_demo_fleet_rps"
	metricFleetP95    = "nodevitals_demo_fleet_latency_p95_ms"
	metricFleetTokens = "nodevitals_demo_fleet_tokens_per_sec"
	metricFleetErrPct = "nodevitals_demo_fleet_error_rate_pct"
)

// poolProfile 은 서빙 풀의 성격이다 — 모델 크기에 따라 요청당 비용·토큰 생성
// 속도·기본 지연이 크게 다르다(대형 LLM 과 임베딩은 자릿수가 다르다).
type poolProfile struct {
	// rpsPerGPU 는 GPU 1장이 감당하는 초당 요청 수(정상 부하 기준).
	rpsPerGPU float64
	// baseLatencyMS 는 무부하 시 p50 지연.
	baseLatencyMS float64
	// tokensPerReq 는 요청당 생성 토큰 수(임베딩·분류는 0에 가깝다).
	tokensPerReq float64
	// batchMax 는 최대 배치 크기.
	batchMax float64
}

// 풀 ID → 프로파일. 미등록 풀은 일반 LLM 서빙으로 취급한다.
var poolProfiles = map[string]poolProfile{
	"llm-serving-a":    {rpsPerGPU: 2.4, baseLatencyMS: 320, tokensPerReq: 420, batchMax: 32},
	"llm-serving-b":    {rpsPerGPU: 3.1, baseLatencyMS: 260, tokensPerReq: 310, batchMax: 32},
	"vision-inference": {rpsPerGPU: 42, baseLatencyMS: 28, tokensPerReq: 0, batchMax: 64},
	"speech-inference": {rpsPerGPU: 18, baseLatencyMS: 95, tokensPerReq: 0, batchMax: 16},
	"rag-api":          {rpsPerGPU: 8.5, baseLatencyMS: 180, tokensPerReq: 140, batchMax: 24},
	"ocr-batch":        {rpsPerGPU: 26, baseLatencyMS: 55, tokensPerReq: 0, batchMax: 96},
	"embedding":        {rpsPerGPU: 120, baseLatencyMS: 12, tokensPerReq: 0, batchMax: 128},
	"recsys-serving":   {rpsPerGPU: 64, baseLatencyMS: 18, tokensPerReq: 0, batchMax: 48},
}

func profileOf(pool string) poolProfile {
	if p, ok := poolProfiles[pool]; ok {
		return p
	}
	return poolProfile{rpsPerGPU: 5, baseLatencyMS: 200, tokensPerReq: 200, batchMax: 32}
}

// ServingStats 는 풀 하나의 서빙 상태다.
type ServingStats struct {
	Pool            string  `json:"pool"`
	Display         string  `json:"display"`
	Tenant          string  `json:"tenant"`
	GPUs            int     `json:"gpus"`
	UtilAvgPct      float64 `json:"utilAvgPct"`
	RPS             float64 `json:"rps"`
	P50MS           float64 `json:"p50Ms"`
	P95MS           float64 `json:"p95Ms"`
	P99MS           float64 `json:"p99Ms"`
	TokensPerSec    float64 `json:"tokensPerSec"`
	QueueDepth      float64 `json:"queueDepth"`
	BatchSize       float64 `json:"batchSize"`
	ErrorRatePct    float64 `json:"errorRatePct"`
	ReplicasReady   int     `json:"replicasReady"`
	ReplicasDesired int     `json:"replicasDesired"`
	// Saturated 는 큐 포화(지연 급등) 상태다 — 화면 경고 배지의 근거.
	Saturated bool `json:"saturated"`
}

// computeServing 은 풀 집계(GPU 수·평균 사용률)에서 서빙 지표를 파생한다.
// 순수 함수 — 같은 입력이면 같은 결과(백필·라이브 연속성 계약).
func computeServing(pool PoolInfo, gpus int, utilAvg float64, tMS int64, seed int64) ServingStats {
	p := profileOf(pool.ID)
	s := ServingStats{
		Pool: pool.ID, Display: pool.Display, Tenant: pool.Tenant,
		GPUs: gpus, UtilAvgPct: utilAvg,
	}
	if gpus == 0 {
		return s
	}

	// 사용률을 부하 계수로 환산 — 100% 에서 이론 처리량에 도달한다.
	load := clamp(utilAvg/100, 0, 1)
	jitter := signalNoise(seed, "pool:"+pool.ID, "rps", tMS)

	s.RPS = float64(gpus) * p.rpsPerGPU * load * (1 + jitter*0.06)
	if s.RPS < 0 {
		s.RPS = 0
	}
	s.TokensPerSec = s.RPS * p.tokensPerReq

	// 배치 크기는 부하를 따라 커진다(서빙 엔진의 동적 배칭).
	s.BatchSize = 1 + (p.batchMax-1)*math.Pow(load, 1.3)

	// 큐 깊이 — 부하 85% 를 넘기면서 급격히 쌓인다(대기행렬 이론의 무릎).
	knee := 0.85
	if load <= knee {
		s.QueueDepth = float64(gpus) * load * 0.25
	} else {
		over := (load - knee) / (1 - knee)
		s.QueueDepth = float64(gpus) * (0.25*knee + over*over*3.5)
	}
	s.QueueDepth *= 1 + jitter*0.1

	// 지연 — 기본 지연 + 큐 대기(리틀의 법칙 근사). p95/p99 는 꼬리가 더 길다.
	queueWaitMS := s.QueueDepth / math.Max(1, s.RPS) * 1000
	s.P50MS = p.baseLatencyMS*(1+0.35*load) + queueWaitMS*0.4
	s.P95MS = s.P50MS*1.9 + queueWaitMS*1.6
	s.P99MS = s.P50MS*2.8 + queueWaitMS*3.2

	// 에러율 — 평시엔 거의 0, 포화 구간에서 타임아웃/거절이 발생한다.
	switch {
	case load > 0.97:
		s.ErrorRatePct = 0.8 + math.Abs(jitter)*1.4
	case load > 0.9:
		s.ErrorRatePct = 0.05 + math.Abs(jitter)*0.15
	default:
		s.ErrorRatePct = math.Abs(jitter) * 0.02
	}
	s.Saturated = load > 0.9

	// 레플리카 — GPU 1장당 파드 1개 전제. 드물게 1개가 준비 중이다.
	s.ReplicasDesired = gpus
	s.ReplicasReady = gpus
	if unitFloat(fnvHash(seed, "replica", pool.ID, strconv.FormatInt(tMS/300_000, 10))) < 0.12 {
		s.ReplicasReady = gpus - 1
	}
	return s
}

// emitServing 은 서빙 시리즈를 emit 하고 풀별 통계를 낸다(상태 스냅샷 공용).
func (e *Engine) emitServing(tMS int64, agg *tickAggregates) ([]ServingStats, error) {
	stats := make([]ServingStats, 0, len(e.fleet.Pools))
	var fleetRPS, fleetTokens, weightedP95, weightedErr, weightW float64

	for _, pool := range e.fleet.Pools {
		a := agg.pool[pool.ID]
		gpus, util := 0, 0.0
		if a != nil {
			gpus, util = a.allocated, a.utilAvg()
		}
		st := computeServing(pool, gpus, util, tMS, e.cfg.Seed)
		stats = append(stats, st)

		l := map[string]string{"pool": pool.ID, "tenant": pool.Tenant}
		for _, em := range []aggEmit{
			{metricPoolRPS, l, st.RPS},
			{metricPoolP50, l, st.P50MS},
			{metricPoolP95, l, st.P95MS},
			{metricPoolP99, l, st.P99MS},
			{metricPoolTokens, l, st.TokensPerSec},
			{metricPoolQueue, l, st.QueueDepth},
			{metricPoolBatch, l, st.BatchSize},
			{metricPoolErrPct, l, st.ErrorRatePct},
			{metricPoolReplicas, l, float64(st.ReplicasReady)},
			{metricPoolReplicasD, l, float64(st.ReplicasDesired)},
		} {
			if err := e.appendSimple(em.name, em.labels, tMS, em.v); err != nil {
				return nil, err
			}
		}

		fleetRPS += st.RPS
		fleetTokens += st.TokensPerSec
		// 플릿 지연·에러율은 요청량 가중 평균이라야 의미가 있다.
		weightedP95 += st.P95MS * st.RPS
		weightedErr += st.ErrorRatePct * st.RPS
		weightW += st.RPS
	}

	p95, errPct := 0.0, 0.0
	if weightW > 0 {
		p95 = weightedP95 / weightW
		errPct = weightedErr / weightW
	}
	none := map[string]string{}
	for _, em := range []aggEmit{
		{metricFleetRPS, none, fleetRPS},
		{metricFleetTokens, none, fleetTokens},
		{metricFleetP95, none, p95},
		{metricFleetErrPct, none, errPct},
	} {
		if err := e.appendSimple(em.name, em.labels, tMS, em.v); err != nil {
			return nil, err
		}
	}
	return stats, nil
}
