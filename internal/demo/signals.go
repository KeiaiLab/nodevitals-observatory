package demo

import (
	"math"
	"strconv"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// per-GPU 메트릭 이름 — 실 에이전트(nodevitals)의 exposition 계약과 동일하게
// 맞춘다. 데모 인스턴스와 실서비스 인스턴스가 같은 프론트 코드를 공유하는
// 전제다(계획 §A).
const (
	metricUtil     = "nodevitals_hw_gpu_utilization_pct"
	metricMemUsed  = "nodevitals_hw_gpu_mem_used_bytes"
	metricMemTotal = "nodevitals_hw_gpu_mem_total_bytes"
	metricTemp     = "nodevitals_hw_gpu_temperature_celsius"
	metricPower    = "nodevitals_hw_gpu_power_watts"
	metricEccCorr  = "nodevitals_hw_gpu_ecc_corrected_total"
	metricEccUnc   = "nodevitals_hw_gpu_ecc_uncorrected_total"
	metricThrottle = "nodevitals_hw_gpu_throttle_active"
)

var gpuMetricNames = []string{
	metricUtil, metricMemUsed, metricMemTotal, metricTemp,
	metricPower, metricEccCorr, metricEccUnc, metricThrottle,
}

// eccAnchorMS 는 정정 ECC 카운터의 기산점이다(2026-01-01 UTC). 절대 epoch 를
// 그대로 쓰면 카운터 크기가 수백만이 되어 화면 가독성이 나빠서 최근 앵커에서
// 기산한다 — 값은 t 에 대해 단조증가라 counter 의미론을 지킨다.
const eccAnchorMS = int64(1_767_225_600_000)

// gpuSignals 는 특정 시각의 GPU 한 장 신호 묶음이다.
type gpuSignals struct {
	Util     float64
	Temp     float64
	Power    float64
	MemUsed  float64
	MemTotal float64
	EccCorr  float64
	EccUnc   float64
	Throttle bool
}

// signalNoise 는 (seed, uuid, metric, 15s 양자화 시각) → [-1,1] 결정론 노이즈다.
// 시각을 15s 로 양자화해 백필과 라이브 틱이 같은 격자 위에서 동일 값을 낸다.
func signalNoise(seed int64, uuid, metric string, tMS int64) float64 {
	return signalNoiseAt(seed, uuid, metric, tMS/15_000)
}

// signalNoiseAt 은 양자화 인덱스를 직접 받는 버전이다(다중 시간 격자 합성용).
func signalNoiseAt(seed int64, uuid, metric string, q int64) float64 {
	h := fnvHash(seed, "noise", uuid, metric)
	h ^= uint64(q) * 0x9e3779b97f4a7c15
	return unitFloat(h*2654435761)*2 - 1
}

// smoothstep 은 [0,1] 구간의 S-커브다 — 워크로드 전환을 계단이 아니라 램프로
// 만든다(실 서빙 파드의 warm-up/drain 을 닮게).
func smoothstep(x float64) float64 {
	x = clamp(x, 0, 1)
	return x * x * (3 - 2*x)
}

// episodeAt 은 시각 tMS 가 속한 "워크로드 에피소드"(잡 배치 구간)의 인덱스와
// 구간 내 진행률을 낸다. 에피소드는 GPU 마다 3~9분으로 서로 다르다 — 플릿
// 전체가 동시에 출렁이지 않고 셀마다 제각기 바뀌어야 히트맵이 살아 보인다.
func episodeAt(seed int64, uuid string, tMS int64) (idx int64, pos float64, lenMS int64) {
	lenMS = 180_000 + int64(unitFloat(fnvHash(seed, "eplen", uuid))*360_000)
	idx = tMS / lenMS
	pos = float64(tMS-idx*lenMS) / float64(lenMS)
	return idx, pos, lenMS
}

// episodeLoad 는 에피소드 하나의 목표 사용률 오프셋(대역 spread 배수)이다.
// 대부분은 평시 부하, 일부 에피소드는 고부하(배치 잡 유입)나 유휴(잡 종료)다.
func episodeLoad(seed int64, uuid string, idx int64) float64 {
	r := unitFloat(fnvHash(seed, "ep", uuid, strconv.FormatInt(idx, 10)))
	switch {
	case r < 0.12: // 잡 종료 — 유휴로 급락
		return -1.8
	case r < 0.30: // 저부하
		return -0.9
	case r < 0.72: // 평시
		return (r - 0.5) * 1.2
	case r < 0.92: // 고부하
		return 1.1
	default: // 풀로드 배치 — 대역 상단 돌파
		return 2.0
	}
}

// computeSignals 는 순수 함수다 — 상태 없이 (GPU 파라미터, 시각) 만으로 신호를
// 만든다. 백필(과거 t)과 라이브(현재 t)가 같은 곡선을 잇는 것이 계약이라
// 랜덤워크(누적 상태) 대신 이 형태를 쓴다.
//
// 3층 합성으로 "실제 운영처럼" 움직인다:
//   - 에피소드(3~9분): 잡 시작/종료로 인한 플래토 전환 — 대역을 넘나들어
//     히트맵 색이 실제로 바뀐다(같은 대역 안 미세 진동만으로는 정지해 보인다).
//   - 사인 2종(6~45분): 일간 패턴 성격의 완만한 기복.
//   - 단주기 노이즈(15s/60s): 틱마다 눈에 띄는 흔들림.
func computeSignals(g *GPU, tMS int64, seed int64) gpuSignals {
	spec := modelSpecOf(g.Model)
	sig := gpuSignals{MemTotal: spec.MemBytes}

	noise := signalNoise(seed, g.UUID, "u", tMS)
	if !g.Allocated {
		// 미할당 = 대기 전력만. 소량 노이즈로 죽은 시리즈처럼 보이지 않게 한다.
		sig.Util = math.Abs(noise) * 2
		sig.Temp = 32 + math.Abs(noise)*3
		sig.Power = spec.IdleW + math.Abs(noise)*5
		sig.MemUsed = spec.MemBytes * 0.01
		return sig
	}

	b := utilBands[g.band]

	// 에피소드 전환 — 구간 앞 20% 는 직전 에피소드 목표에서 램프로 넘어온다.
	idx, pos, _ := episodeAt(seed, g.UUID, tMS)
	cur := episodeLoad(seed, g.UUID, idx)
	prev := episodeLoad(seed, g.UUID, idx-1)
	ep := prev + (cur-prev)*smoothstep(pos/0.2)

	wave := 0.6*math.Sin(2*math.Pi*float64(tMS)/g.period1+g.phase1) +
		0.4*math.Sin(2*math.Pi*float64(tMS)/g.period2+g.phase2)
	// 단주기 2종(15s·60s)을 겹쳐 틱마다 체감되는 흔들림을 만든다.
	fast := 0.7*noise + 0.5*signalNoiseAt(seed, g.UUID, "u60", tMS/60_000)
	// 일간·주간 리듬은 배율로 얹는다 — 대역 안에서 더하면 진폭이 spread 에
	// 갇혀 밤낮 차이가 몇 %p 밖에 안 난다(24h 차트가 평평해 보이는 원인).
	lm := loadMultiplier(tMS)

	sig.Util = clamp((b.base+b.spread*(0.55*wave+ep)+fast*3.2)*lm, 0.3, 99.8)

	// 순간 스파이크 — 에피소드당 드물게(≈4%) 짧게 치솟는다(추론 트래픽 버스트).
	if unitFloat(fnvHash(seed, "spike", g.UUID, strconv.FormatInt(idx, 10))) < 0.04 {
		if burst := 1 - math.Abs(pos-0.5)/0.08; burst > 0 {
			sig.Util = clamp(sig.Util+smoothstep(burst)*(99.5-sig.Util), 0.3, 99.8)
		}
	}

	tNoise := signalNoise(seed, g.UUID, "t", tMS)
	// 온도는 부하를 지연 추종한다 — 직전 30초 부하를 섞어 관성을 준다.
	prevUtil := clamp((b.base+b.spread*(0.55*wave+prev))*lm, 0.3, 99.8)
	thermal := 0.75*sig.Util + 0.25*prevUtil
	// 랙 편차 — 실제 데이터센터에서 GPU 는 독립적으로 뜨거워지지 않는다.
	// 같은 랙(냉기 통로·PDU 공유)은 함께 오르고, 만성 고온 랙이 존재한다.
	sig.Temp = 30 + thermal*0.48 + tNoise*1.8 + rackThermalBias(seed, g.Rack, tMS)
	// 고부하 지속 시 thermal throttle — 실 운영의 간헐 스로틀 재현.
	if sig.Temp > 78 && signalNoiseAt(seed, g.UUID, "thr", tMS/60_000) > 0.55 {
		sig.Throttle = true
		sig.Util = clamp(sig.Util-6, 0.3, 99.8)
	}

	pNoise := signalNoise(seed, g.UUID, "p", tMS)
	sig.Power = spec.IdleW + (spec.TDPWatts-spec.IdleW)*math.Pow(sig.Util/100, 1.1) + pNoise*10
	if sig.Power < spec.IdleW {
		sig.Power = spec.IdleW
	}

	mNoise := signalNoise(seed, g.UUID, "m", tMS)
	// 메모리는 부하보다 느리게 변한다(모델 상주) — 에피소드 단위로만 크게 움직인다.
	sig.MemUsed = clamp(spec.MemBytes*(0.30+0.55*(0.4*sig.Util+0.6*prevUtil)/100+mNoise*0.02), 0, spec.MemBytes)

	if g.eccProne {
		// 6시간마다 1건꼴 누적 — "무음 열화" 후보군의 배경 신호.
		sig.EccCorr = float64((tMS - eccAnchorMS) / (6 * 3600 * 1000))
		if sig.EccCorr < 0 {
			sig.EccCorr = 0
		}
	}
	return sig
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- 집계 누산 ----

// aggAccum 은 fast 틱 한 번의 집계 누산기다. 서버에 집계 연산이 없으므로
// (등호 매처만) 엔진이 사전 집계 시리즈를 직접 emit 한다 — KPI·롤업 카드가
// per-GPU 7,000 시리즈를 브라우저로 끌어오지 않게 하는 장치다.
type aggAccum struct {
	total, allocated, faulted, tempAlerts int
	utilSum                               float64 // 할당 GPU 기준
	utilCnt                               int
	tempMax                               float64
	powerSumW                             float64
}

func (a *aggAccum) add(g *GPU, sig gpuSignals, faulted bool) {
	a.addCounts(g, faulted)
	if g.Allocated {
		a.utilSum += sig.Util
		a.utilCnt++
	}
	if sig.Temp > 80 {
		a.tempAlerts++
	}
	if sig.Temp > a.tempMax {
		a.tempMax = sig.Temp
	}
	a.powerSumW += sig.Power
}

// addCounts 는 신호 없이 인벤토리 카운트만 누산한다 — Agent Missing 노드·교체
// 창(emit 중단) GPU 도 총계에는 포함해, 모든 화면의 총계(총 GPU/할당/장애)가
// 인벤토리 전수와 산술 일치하게 한다(전 화면 단일 목데이터 정합 계약).
// 신호 통계(util 평균·온도·전력)는 관측된 개체만 반영한다.
func (a *aggAccum) addCounts(g *GPU, faulted bool) {
	a.total++
	if g.Allocated {
		a.allocated++
	}
	if faulted {
		a.faulted++
	}
}

func (a *aggAccum) utilAvg() float64 {
	if a.utilCnt == 0 {
		return 0
	}
	return a.utilSum / float64(a.utilCnt)
}

// tickAggregates 는 한 틱의 집계 묶음이다.
type tickAggregates struct {
	fleet   aggAccum
	csp     map[string]*aggAccum
	cluster map[string]*aggAccum
	pool    map[string]*aggAccum
}

func newTickAggregates() *tickAggregates {
	return &tickAggregates{csp: map[string]*aggAccum{}, cluster: map[string]*aggAccum{}, pool: map[string]*aggAccum{}}
}

func (ta *tickAggregates) add(g *GPU, sig gpuSignals, faulted bool) {
	ta.fleet.add(g, sig, faulted)
	ensureAgg(ta.csp, g.CSP).add(g, sig, faulted)
	ensureAgg(ta.cluster, g.Cluster).add(g, sig, faulted)
	if g.Pool != "" {
		ensureAgg(ta.pool, g.Pool).add(g, sig, faulted)
	}
}

// addCounts 는 관측 불가 개체(Agent Missing·교체 창)의 인벤토리 카운트 누산이다.
func (ta *tickAggregates) addCounts(g *GPU, faulted bool) {
	ta.fleet.addCounts(g, faulted)
	ensureAgg(ta.csp, g.CSP).addCounts(g, faulted)
	ensureAgg(ta.cluster, g.Cluster).addCounts(g, faulted)
	if g.Pool != "" {
		ensureAgg(ta.pool, g.Pool).addCounts(g, faulted)
	}
}

func ensureAgg(m map[string]*aggAccum, key string) *aggAccum {
	a, ok := m[key]
	if !ok {
		a = &aggAccum{}
		m[key] = a
	}
	return a
}

// ---- emit ----

// emitFast 는 15s 틱의 전체 emit 이다 — per-GPU fast 4종 + 노드 시리즈 +
// 사전 집계. 수집 중단 노드(Agent Missing 서사)는 통째로 건너뛴다 — 데이터가
// "안 오는" 것 자체가 그 장애의 신호다.
func (e *Engine) emitFast(tMS int64) error {
	agg := newTickAggregates()
	faults := e.scen.FaultSet()

	for _, node := range e.fleet.Nodes {
		if e.scen.NodeMissing(node.Instance) {
			// Agent Missing — 시리즈는 안 나가되 인벤토리 총계에는 포함한다
			// (총 GPU 7,000 이 화면마다 흔들리지 않게).
			for _, g := range node.GPUs {
				_, faulted := faults[g.UUID]
				agg.addCounts(g, faulted)
			}
			continue
		}
		nodeUtilSum := 0.0
		for _, g := range node.GPUs {
			sig := computeSignals(g, tMS, e.cfg.Seed)
			e.scen.Override(g, tMS, &sig)

			_, faulted := faults[g.UUID]
			if err := e.appendGPUFast(g, tMS, sig); err != nil {
				return err
			}
			if math.IsNaN(sig.Util) {
				agg.addCounts(g, faulted) // 교체 창 — 관측만 중단, 인벤토리는 유지
			} else {
				nodeUtilSum += sig.Util
				agg.add(g, sig, faulted)
			}
		}

		// 노드 수준 시리즈 — 기존 Overview(node_load1 기반 up/down 판정)가
		// 데모 인스턴스에서도 875 노드 전부 살아 있게 만든다.
		avgUtil := nodeUtilSum / float64(len(node.GPUs))
		load := 1 + avgUtil/100*12 + math.Abs(signalNoise(e.cfg.Seed, node.Instance, "l", tMS))*1.5
		if err := e.appendSimple("node_load1", map[string]string{"instance": node.Instance}, tMS, load); err != nil {
			return err
		}
		if err := e.appendSimple("observatory_up", map[string]string{"instance": node.Instance}, tMS, 1); err != nil {
			return err
		}
	}

	return e.emitDerived(tMS, agg)
}

// emitDerived 는 집계·서빙·SLO 를 한 틱 안에서 함께 emit 한다. 백필과 라이브가
// 같은 함수를 쓰는 것이 핵심 — 백필이 집계만 채우면 서빙 차트의 과거 구간이
// 통째로 비어 "방금 켠 화면"이 된다.
func (e *Engine) emitDerived(tMS int64, agg *tickAggregates) error {
	if err := e.emitAggregates(tMS, agg); err != nil {
		return err
	}
	// 서빙 계층·SLO 는 하드웨어 집계에서 파생하므로 같은 틱 안에서 계산한다
	// (다른 틱의 사용률로 지연을 만들면 두 화면이 어긋난다).
	serving, err := e.emitServing(tMS, agg)
	if err != nil {
		return err
	}
	slo, err := e.emitSLO(tMS, agg)
	if err != nil {
		return err
	}
	e.serving, e.slo = serving, slo
	return nil
}

// computeAggOnly 는 per-GPU append 없이 집계만 계산한다 — 24h 집계 백필 전용.
// emitFast 와 같은 신호 경로(override 포함)를 타서 라이브 구간과 이어진다.
func (e *Engine) computeAggOnly(tMS int64) *tickAggregates {
	agg := newTickAggregates()
	faults := e.scen.FaultSet()
	for _, node := range e.fleet.Nodes {
		missing := e.scen.NodeMissing(node.Instance)
		for _, g := range node.GPUs {
			_, faulted := faults[g.UUID]
			if missing {
				agg.addCounts(g, faulted)
				continue
			}
			sig := computeSignals(g, tMS, e.cfg.Seed)
			e.scen.Override(g, tMS, &sig)
			if math.IsNaN(sig.Util) {
				agg.addCounts(g, faulted)
				continue
			}
			agg.add(g, sig, faulted)
		}
	}
	return agg
}

// appendGPUFast 는 GPU 한 장의 fast 4종(+활성 시 throttle)을 append 한다.
// Util=NaN 은 교체 창의 "emit 중단" 계약이다 — 시리즈가 instant 5분 창에서
// 자연 소멸한다.
func (e *Engine) appendGPUFast(g *GPU, tMS int64, sig gpuSignals) error {
	if math.IsNaN(sig.Util) {
		return nil
	}
	if err := e.db.Append(g.seriesLabels[metricUtil], tMS, sig.Util); err != nil {
		return err
	}
	if err := e.db.Append(g.seriesLabels[metricTemp], tMS, sig.Temp); err != nil {
		return err
	}
	if err := e.db.Append(g.seriesLabels[metricPower], tMS, sig.Power); err != nil {
		return err
	}
	if err := e.db.Append(g.seriesLabels[metricMemUsed], tMS, sig.MemUsed); err != nil {
		return err
	}
	if sig.Throttle {
		if err := e.db.Append(g.seriesLabels[metricThrottle], tMS, 1); err != nil {
			return err
		}
	}
	return nil
}

// emitSlow 는 60s 틱 — 정적/저변동 시리즈(mem_total·ECC 카운터)다. instant
// 5분 평가창 안에만 최신 샘플이 있으면 되므로 15s 로 낼 이유가 없다(부하 절감).
func (e *Engine) emitSlow(tMS int64) error {
	for _, g := range e.fleet.GPUs {
		if e.scen.NodeMissing(g.Instance) {
			continue
		}
		sig := computeSignals(g, tMS, e.cfg.Seed)
		e.scen.Override(g, tMS, &sig)
		if math.IsNaN(sig.Util) {
			continue
		}
		if err := e.db.Append(g.seriesLabels[metricMemTotal], tMS, sig.MemTotal); err != nil {
			return err
		}
		if err := e.db.Append(g.seriesLabels[metricEccCorr], tMS, sig.EccCorr); err != nil {
			return err
		}
		if err := e.db.Append(g.seriesLabels[metricEccUnc], tMS, sig.EccUnc); err != nil {
			return err
		}
	}
	return nil
}

// aggEmit 은 집계 시리즈 emit 1건이다.
type aggEmit struct {
	name   string
	labels map[string]string
	v      float64
}

// emitAggregates 는 사전 집계 시리즈(~65개/틱)를 emit 한다.
func (e *Engine) emitAggregates(tMS int64, agg *tickAggregates) error {
	none := map[string]string{}
	fleet := &agg.fleet
	emits := []aggEmit{
		{"nodevitals_demo_fleet_gpu_total", none, float64(fleet.total)},
		{"nodevitals_demo_fleet_gpu_allocated", none, float64(fleet.allocated)},
		{"nodevitals_demo_fleet_alloc_pct", none, pct(fleet.allocated, fleet.total)},
		{"nodevitals_demo_fleet_util_avg_pct", none, fleet.utilAvg()},
		{"nodevitals_demo_fleet_faulted", none, float64(fleet.faulted)},
		{"nodevitals_demo_fleet_temp_alerts", none, float64(fleet.tempAlerts)},
		{"nodevitals_demo_fleet_power_kw", none, fleet.powerSumW / 1000},
		{"nodevitals_demo_scenario_phase", none, float64(e.scen.PhaseIndex())},
		{"nodevitals_demo_victim_health_score", none, float64(e.scen.VictimHealth(tMS).Score)},
	}

	for _, spec := range e.fleet.Specs {
		a := agg.csp[spec.ID]
		if a == nil {
			a = &aggAccum{}
		}
		l := map[string]string{"csp": spec.ID}
		emits = append(emits,
			aggEmit{"nodevitals_demo_csp_gpu_total", l, float64(a.total)},
			aggEmit{"nodevitals_demo_csp_gpu_allocated", l, float64(a.allocated)},
			aggEmit{"nodevitals_demo_csp_util_avg_pct", l, a.utilAvg()},
			aggEmit{"nodevitals_demo_csp_temp_max_celsius", l, a.tempMax},
			aggEmit{"nodevitals_demo_csp_faulted", l, float64(a.faulted)},
			aggEmit{"nodevitals_demo_csp_power_kw", l, a.powerSumW / 1000},
		)
	}
	for _, c := range e.fleet.Clusters {
		a := agg.cluster[c.Name]
		if a == nil {
			a = &aggAccum{}
		}
		l := map[string]string{"csp": c.CSP, "cluster": c.Name}
		emits = append(emits,
			aggEmit{"nodevitals_demo_cluster_util_avg_pct", l, a.utilAvg()},
			aggEmit{"nodevitals_demo_cluster_gpu_total", l, float64(a.total)},
			aggEmit{"nodevitals_demo_cluster_faulted", l, float64(a.faulted)},
		)
	}
	for _, p := range e.fleet.Pools {
		a := agg.pool[p.ID]
		if a == nil {
			a = &aggAccum{}
		}
		l := map[string]string{"pool": p.ID, "tenant": p.Tenant}
		emits = append(emits,
			aggEmit{"nodevitals_demo_pool_gpu_allocated", l, float64(a.allocated)},
			aggEmit{"nodevitals_demo_pool_util_avg_pct", l, a.utilAvg()},
		)
	}

	for _, em := range emits {
		if err := e.appendSimple(em.name, em.labels, tMS, em.v); err != nil {
			return err
		}
	}
	return nil
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

// appendSimple 은 (이름, 라벨, 시각, 값) 단발 append 헬퍼다. 집계·노드 시리즈
// (~2천 개/틱)는 per-GPU(3만 개/틱)와 달리 사전 계산 없이 만들어도 비용이
// 무시 가능해 단순성을 택한다.
func (e *Engine) appendSimple(name string, labels map[string]string, tMS int64, v float64) error {
	m := make(map[string]string, len(labels)+1)
	for k, val := range labels {
		m[k] = val
	}
	m[tsdb.MetricName] = name
	return e.db.Append(tsdb.LabelsFromMap(m), tMS, v)
}
