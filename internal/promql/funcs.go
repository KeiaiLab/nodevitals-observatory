package promql

import (
	"math"
	"time"
)

// rangeFuncs 는 range vector 를 받아 값 하나를 내는 함수들이다. bool 은
// "값을 낼 수 있는가" — 낼 수 없으면 그 시리즈는 결과에서 빠진다. 0 을
// 대신 내면 "증가 없음" 같은 거짓 사실이 메트릭이 된다.
var rangeFuncs = map[string]func(Series, time.Duration) (float64, bool){
	"increase":        funcIncrease,
	"rate":            funcRate,
	"max_over_time":   overTime(math.Max, math.Inf(-1)),
	"min_over_time":   overTime(math.Min, math.Inf(1)),
	"avg_over_time":   funcAvgOverTime,
	"count_over_time": funcCountOverTime,
	"last_over_time":  funcLastOverTime,
	"changes":         funcChanges,
}

// delta 는 counter reset 을 보정한 누적 증가량이다. 값이 이전보다 작아지면
// counter 가 재시작한 것으로 보고, 새 값 전체를 증가분으로 인정한 뒤 거기서
// 이어 센다. 보정하지 않으면 하강분이 음수로 상쇄되어 "증가량" 이 실제보다
// 작아지거나 음수가 된다 — 파드 재시작마다 일어나는 일이다.
func delta(pts []Point) float64 {
	var total float64
	for i := 1; i < len(pts); i++ {
		cur, prev := pts[i].V, pts[i-1].V
		if cur >= prev {
			total += cur - prev
			continue
		}
		total += cur // 재시작: 0 에서 cur 까지 올라온 것으로 본다
	}
	return total
}

func funcIncrease(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) < 2 {
		return 0, false
	}
	return delta(s.Points), true
}

func funcRate(s Series, r time.Duration) (float64, bool) {
	if len(s.Points) < 2 {
		return 0, false
	}
	secs := r.Seconds()
	if secs <= 0 {
		return 0, false
	}
	return delta(s.Points) / secs, true
}

func overTime(fold func(a, b float64) float64, init float64) func(Series, time.Duration) (float64, bool) {
	return func(s Series, _ time.Duration) (float64, bool) {
		if len(s.Points) == 0 {
			return 0, false
		}
		acc := init
		for _, p := range s.Points {
			acc = fold(acc, p.V)
		}
		return acc, true
	}
}

func funcAvgOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	var sum float64
	for _, p := range s.Points {
		sum += p.V
	}
	return sum / float64(len(s.Points)), true
}

func funcCountOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	return float64(len(s.Points)), true
}

func funcLastOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	return s.Points[len(s.Points)-1].V, true
}

func funcChanges(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	var n float64
	for i := 1; i < len(s.Points); i++ {
		if s.Points[i].V != s.Points[i-1].V {
			n++
		}
	}
	return n, true
}

// instantFuncs 는 값 하나를 값 하나로 바꾸는 함수다.
var instantFuncs = map[string]func(float64) float64{
	"abs": math.Abs,
}
