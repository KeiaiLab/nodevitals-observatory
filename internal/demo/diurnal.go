package demo

// 일간 리듬과 랙 상관 — 실제 플릿과 합성 데이터를 가르는 두 가지다.
//
//  1. 24시간 차트를 열었을 때 낮/밤 리듬이 없으면 즉시 합성으로 보인다. 실제
//     추론 서비스는 업무시간에 트래픽이 몰리고 새벽에 배치 잡이 돈다.
//  2. 실제 데이터센터에서 GPU 는 독립적으로 뜨거워지지 않는다 — 같은 랙(같은
//     냉기 통로·PDU)의 장비가 함께 오른다. 무작위로 흩어진 고온 GPU 는
//     운영자가 보면 바로 "만든 데이터"로 읽힌다.

import "math"

// kstOffsetMS 는 시연 기준 시간대(KST, UTC+9) 오프셋이다.
const kstOffsetMS = int64(9 * 3600 * 1000)

// hourOfDayKST 는 KST 기준 소수 시각(0.0~24.0)이다.
func hourOfDayKST(tMS int64) float64 {
	local := ((tMS+kstOffsetMS)%86_400_000 + 86_400_000) % 86_400_000
	return float64(local) / 3_600_000
}

// diurnalFactor 는 시각에 따른 부하 계수다(대략 -0.35 ~ +0.30).
//
//	주간 피크   14시 전후   — 업무시간 추론 트래픽
//	저녁 완만   19~22시
//	새벽 배치   03시 전후   — 야간 배치 잡의 작은 봉우리
//	최저        06시 전후   — 트래픽·배치 모두 빠진 골
func diurnalFactor(tMS int64) float64 {
	h := hourOfDayKST(tMS)
	// 주 파형: 14시 최대, 02시 최소(cos 의 마루를 14시에 맞춘다).
	primary := math.Cos(2*math.Pi*(h-14)/24) * 0.24
	// 새벽 배치 봉우리(03시, 폭 ±2시간).
	batch := 0.12 * math.Exp(-math.Pow(h-3, 2)/(2*2.0*2.0))
	// 이른 아침 골(06시) — 배치 종료와 업무 시작 사이.
	dawn := -0.10 * math.Exp(-math.Pow(h-6.5, 2)/(2*1.4*1.4))
	return primary + batch + dawn
}

// weekdayLevel 은 요일별 기준 부하 수준이다 — 주말엔 업무 트래픽 자체가
// 빠진다(진폭만 줄어드는 게 아니라 전체가 내려간다). 일주일 창을 열었을 때
// 7일이 똑같은 모양이면 그것도 합성 신호다.
func weekdayLevel(tMS int64) float64 {
	// 1970-01-01 은 목요일 — KST 기준 요일 산출.
	day := ((tMS+kstOffsetMS)/86_400_000 + 4) % 7 // 0=일 … 6=토
	switch day {
	case 0: // 일요일 — 가장 조용하다
		return 0.62
	case 6: // 토요일
		return 0.72
	case 1: // 월요일 — 주말 적체 처리로 약간 높다
		return 1.06
	default:
		return 1.0
	}
}

// loadMultiplier 는 시각·요일이 만드는 부하 배율이다(대략 0.45 ~ 1.45).
//
// 대역 오프셋(덧셈)이 아니라 배율(곱셈)인 이유: 덧셈으로 넣으면 진폭이
// 대역 spread 에 갇혀 밤낮 차이가 몇 %p 밖에 안 난다. 실제 추론 서비스는
// 새벽과 오후의 부하가 2배 가까이 차이 나고, 그 차이가 24h 차트의 형태를
// 결정한다.
func loadMultiplier(tMS int64) float64 {
	return weekdayLevel(tMS) * (1 + diurnalFactor(tMS)*1.45)
}

// rackOf 는 노드가 속한 랙 이름이다. 노드 4대가 한 랙(2U 서버 기준 현실적
// 밀도)이며, 랙 번호는 클러스터 내 노드 순번에서 파생한다.
func rackOf(cluster string, nodeIdx int) string {
	return cluster + "-r" + twoDigits(nodeIdx/4+1)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	if n < 100 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}

// rackThermalBias 는 랙 단위 냉각 편차(°C)다. 랙마다 고정 편차(핫아일 위치·
// 팬 성능 차이)를 갖고, 그 위에 느린 변동(공조 부하)이 얹힌다. 일부 랙은
// 만성적으로 뜨겁다 — 실제 운영에서 "저 랙이 원래 좀 뜨겁다"는 그 현상이다.
func rackThermalBias(seed int64, rack string, tMS int64) float64 {
	r := unitFloat(fnvHash(seed, "rackbias", rack))
	var base float64
	switch {
	case r < 0.08: // 만성 고온 랙 (냉각 불균형)
		base = 5.5
	case r < 0.25:
		base = 2.5
	case r < 0.80:
		base = (r - 0.5) * 2 // -0.6 ~ +0.6
	default:
		base = -1.8 // 냉기 통로 바로 앞 — 시원한 랙
	}
	// 공조 부하는 외기온과 함께 움직인다 — 일간 리듬을 따라간다.
	hvac := diurnalFactor(tMS) * 3.0
	// 랙별 느린 흔들림(20분 격자).
	drift := signalNoiseAt(seed, "rack:"+rack, "thermal", tMS/1_200_000) * 0.8
	return base + hvac + drift
}
