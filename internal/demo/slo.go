package demo

// SLO — 요구사항 NFR-04("가용 GPU 비율 · MTBF/MTTR 목표")의 화면 표면.
// 운영 콘솔과 대시보드를 가르는 지표이기도 하다: 순간값만 있으면 모니터,
// 목표 대비 잔여 예산이 있어야 운영이 된다.
//
// 값은 플릿 규모에서 역산한 통계적 추정이다 — 데모가 "그럴듯한 수치"를
// 지어내는 대신, 장애 개체 수·복구 시간이라는 관측 가능한 양에서 파생한다.

const (
	metricSLOAvailability = "nodevitals_demo_slo_gpu_availability_pct"
	metricSLOTarget       = "nodevitals_demo_slo_target_pct"
	metricSLOErrorBudget  = "nodevitals_demo_slo_error_budget_remaining_pct"
	metricSLOMTBF         = "nodevitals_demo_slo_mtbf_hours"
	metricSLOMTTR         = "nodevitals_demo_slo_mttr_minutes"
	metricSLOIncidents30d = "nodevitals_demo_slo_incidents_30d"
)

// SLOState 는 운영 품질 지표 묶음이다.
type SLOState struct {
	// TargetPct 는 가용 GPU 비율 목표(계약값).
	TargetPct float64 `json:"targetPct"`
	// AvailabilityPct 는 현재 가용 GPU 비율(장애·격리 제외).
	AvailabilityPct float64 `json:"availabilityPct"`
	// ErrorBudgetRemainingPct 는 30일 오차 예산 잔여율.
	ErrorBudgetRemainingPct float64 `json:"errorBudgetRemainingPct"`
	// WindowDays 는 예산 산정 창.
	WindowDays int `json:"windowDays"`
	// MTBFHours / MTTRMinutes 는 최근 30일 기준 추정치.
	MTBFHours    float64 `json:"mtbfHours"`
	MTTRMinutes  float64 `json:"mttrMinutes"`
	Incidents30d int     `json:"incidents30d"`
	// UnavailableGPUs 는 현재 서비스 불가 GPU 수(장애 + 격리 + 수집중단).
	UnavailableGPUs int `json:"unavailableGpus"`
	// Breaching 은 목표 미달 상태다 — 화면 경고 배지 근거.
	Breaching bool `json:"breaching"`
}

// sloTargetPct 는 계약 목표다(가용 GPU 비율 99.5%).
const sloTargetPct = 99.5

// computeSLO 는 현재 플릿 상태에서 SLO 를 산출한다.
//
//	가용률 = (전체 − 불가) / 전체
//	MTBF   = 창 시간 / 인시던트 수      (인시던트 0 이면 창 전체)
//	오차예산 = 1 − (실제 불가 시간 / 허용 불가 시간)
func computeSLO(total, unavailable, incidents30d int, mttrMinutes float64) SLOState {
	s := SLOState{
		TargetPct: sloTargetPct, WindowDays: 30,
		Incidents30d: incidents30d, UnavailableGPUs: unavailable,
		MTTRMinutes: mttrMinutes,
	}
	if total == 0 {
		return s
	}
	s.AvailabilityPct = float64(total-unavailable) / float64(total) * 100

	windowHours := 30 * 24.0
	if incidents30d > 0 {
		s.MTBFHours = windowHours / float64(incidents30d)
	} else {
		s.MTBFHours = windowHours
	}

	// 오차 예산 — 허용 불가율(1 − 목표) 대비 실제 불가율의 소진분.
	//
	// GPU·시간 총량으로 나누면(전체 × 720h) 분모가 압도적이라 예산이 늘 100%
	// 근처에 붙어 패널이 아무것도 말하지 않는다. SRE 관행대로 **비율 대 비율**로
	// 본다: 지금 상시 불가인 GPU 비율이 허용 불가율의 몇 %를 쓰고 있는가.
	//
	//   allowedRatio  = 1 − 목표(0.5%)
	//   consumedRatio = 상시 불가율(장애·수집중단 — 계속 빠져 있다)
	//                 + 인시던트 전이분(창 내 평균, 복구되므로 기여가 작다)
	allowedRatio := 1 - sloTargetPct/100
	standing := float64(unavailable) / float64(total)
	// 인시던트당 영향 = 노드 1대(8장)가 MTTR 동안 빠진다고 본다.
	const gpusPerIncident = 8.0
	transient := float64(incidents30d) * (mttrMinutes / 60) * gpusPerIncident /
		(float64(total) * windowHours)
	if allowedRatio > 0 {
		s.ErrorBudgetRemainingPct = clamp((1-(standing+transient)/allowedRatio)*100, 0, 100)
	}
	s.Breaching = s.AvailabilityPct < sloTargetPct
	return s
}

// sloInputs 는 시나리오·플릿에서 SLO 입력을 모은다.
// incidents30d 와 MTTR 은 seed 에서 파생한 안정적 추정치에 현재 사이클을
// 더한다 — 시연 중 격리를 승인하면 인시던트가 실제로 1건 늘어난다.
func (e *Engine) sloInputs(agg *tickAggregates) (total, unavailable, incidents int, mttr float64) {
	total = agg.fleet.total
	unavailable = agg.fleet.faulted
	if e.scen.missingNode != nil {
		unavailable += len(e.scen.missingNode.GPUs)
	}
	// 기저 인시던트 — 30일간 플릿 규모에 비례(1,000장당 약 4건).
	base := total/250 + 6
	incidents = base + (e.scen.cycle - 1)
	// MTTR — 자동 복구 파이프라인 기준 18~26분(격리→교체→검증).
	mttr = 18 + unitFloat(fnvHash(e.cfg.Seed, "mttr"))*8
	return total, unavailable, incidents, mttr
}

// emitSLO 는 SLO 시리즈를 emit 하고 상태를 낸다.
func (e *Engine) emitSLO(tMS int64, agg *tickAggregates) (SLOState, error) {
	total, unavailable, incidents, mttr := e.sloInputs(agg)
	s := computeSLO(total, unavailable, incidents, mttr)

	none := map[string]string{}
	for _, em := range []aggEmit{
		{metricSLOAvailability, none, s.AvailabilityPct},
		{metricSLOTarget, none, s.TargetPct},
		{metricSLOErrorBudget, none, s.ErrorBudgetRemainingPct},
		{metricSLOMTBF, none, s.MTBFHours},
		{metricSLOMTTR, none, s.MTTRMinutes},
		{metricSLOIncidents30d, none, float64(s.Incidents30d)},
	} {
		if err := e.appendSimple(em.name, em.labels, tMS, em.v); err != nil {
			return s, err
		}
	}
	return s, nil
}
