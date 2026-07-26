package demo

import (
	"math"

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
	q := tMS / 15_000
	h := fnvHash(seed, "noise", uuid, metric)
	h ^= uint64(q) * 0x9e3779b97f4a7c15
	return unitFloat(h*2654435761)*2 - 1
}

// computeSignals 는 순수 함수다 — 상태 없이 (GPU 파라미터, 시각) 만으로 신호를
// 만든다. 사인 2개 합성 + 해시 노이즈라 백필(과거 t)과 라이브(현재 t)가 같은
// 곡선을 이어 그린다. 랜덤워크(누적 상태) 대신 이 형태를 고른 이유가 그것이다.
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
	wave := 0.6*math.Sin(2*math.Pi*float64(tMS)/g.period1+g.phase1) +
		0.4*math.Sin(2*math.Pi*float64(tMS)/g.period2+g.phase2)
	sig.Util = clamp(b.base+b.spread*wave+noise*2.0, 0.3, 99.5)

	tNoise := signalNoise(seed, g.UUID, "t", tMS)
	sig.Temp = 30 + sig.Util*0.48 + tNoise*1.5

	pNoise := signalNoise(seed, g.UUID, "p", tMS)
	sig.Power = spec.IdleW + (spec.TDPWatts-spec.IdleW)*math.Pow(sig.Util/100, 1.1) + pNoise*8
	if sig.Power < spec.IdleW {
		sig.Power = spec.IdleW
	}

	mNoise := signalNoise(seed, g.UUID, "m", tMS)
	sig.MemUsed = clamp(spec.MemBytes*(0.35+0.55*sig.Util/100+mNoise*0.03), 0, spec.MemBytes)

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

	return e.emitAggregates(tMS, agg)
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
