package demo

// Health Score 는 학습 모델이 아니라 **설명 가능한 감점표** 방식이다 — 운영자
// 승인 화면에서 "왜 이 점수인가"를 근거 목록으로 제시할 수 있어야 하기
// 때문이다(관제 도메인 관행: 판정 근거 조회 가능성 > 예측 정확도).

// Deduction 은 감점 1건이다.
type Deduction struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Penalty int    `json:"penalty"`
}

// HealthResult 는 점수 + 근거 목록이다.
type HealthResult struct {
	Score      int         `json:"score"`
	Deductions []Deduction `json:"deductions"`
}

// HealthInput 은 판정 입력이다 — 시나리오가 현재 신호에서 채운다.
type HealthInput struct {
	TempC          float64
	EccCorrDelta1h float64 // 최근 1h 정정 ECC 증가량
	EccUncorrected float64
	XIDRecent      bool // 최근 1h 내 XID 발생
	ThrottleActive bool
	AgentMissing   bool
	BaselineNote   string // 감점 0 일 때도 근거 1줄을 남기기 위한 기본 항목
}

// Score 는 100 에서 감점을 누적한다(하한 0). 감점 규칙은 무음 장애 룰셋
// MVP 4종(XID·ECC·온도·Agent Missing) + throttle 로 구성한다.
func Score(in HealthInput) HealthResult {
	res := HealthResult{Score: 100}
	deduct := func(code, msg string, penalty int) {
		res.Deductions = append(res.Deductions, Deduction{Code: code, Message: msg, Penalty: penalty})
		res.Score -= penalty
	}

	switch {
	case in.TempC > 85:
		deduct("temp_critical", "온도 임계 중대 초과 (>85°C)", 20)
	case in.TempC > 80:
		deduct("temp_high", "온도 임계 초과 (>80°C)", 10)
	case in.TempC > 76:
		deduct("temp_margin", "온도 여유 부족 (임계 80°C 근접)", 4)
	}

	switch {
	case in.EccCorrDelta1h >= 6:
		deduct("ecc_corrected_surge", "정정 ECC 급증 (1h 6건 이상 — 무음 열화 신호)", 15)
	case in.EccCorrDelta1h >= 2:
		deduct("ecc_corrected_rate", "정정 ECC 누적 증가 (1h 2건 이상)", 8)
	}

	if in.EccUncorrected > 0 {
		deduct("ecc_uncorrected", "비정정 ECC 발생 — 즉시 격리 대상", 40)
	}
	if in.XIDRecent {
		deduct("xid_recent", "최근 1h 내 XID 오류 감지", 15)
	}
	if in.ThrottleActive {
		deduct("thermal_throttle", "Thermal Throttle 지속", 10)
	}
	if in.AgentMissing {
		deduct("agent_missing", "에이전트 수집 중단 (Agent Missing)", 50)
	}

	if len(res.Deductions) == 0 && in.BaselineNote != "" {
		deduct("baseline", in.BaselineNote, 2)
	}
	if res.Score < 0 {
		res.Score = 0
	}
	return res
}
