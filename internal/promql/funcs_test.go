package promql

import (
	"math"
	"testing"
	"time"
)

func mkSeries(pts ...Point) Series { return Series{Points: pts} }

// counter 가 재시작하면 값이 0 으로 떨어진다. 그 하강을 그대로 빼면 음수가
// 나오고 increase 는 "증가량" 이라는 의미를 잃는다. 파드 재시작마다 실제로
// 일어나는 일이라 반드시 처리해야 한다.
func TestIncreaseHandlesCounterReset(t *testing.T) {
	s := mkSeries(
		Point{T: 0, V: 10},
		Point{T: 30000, V: 20},
		Point{T: 60000, V: 5}, // 재시작
		Point{T: 90000, V: 8},
	)
	got, ok := rangeFuncs["increase"](s, 90*time.Second)
	if !ok {
		t.Fatal("increase 가 값을 내지 못했다")
	}
	if got < 13 {
		t.Errorf("increase=%v, want >=13 (재시작 구간이 음수로 상쇄되면 안 된다)", got)
	}
}

func TestRateIsIncreaseOverSeconds(t *testing.T) {
	s := mkSeries(Point{T: 0, V: 0}, Point{T: 60000, V: 60})
	inc, _ := rangeFuncs["increase"](s, time.Minute)
	rt, _ := rangeFuncs["rate"](s, time.Minute)
	if math.Abs(rt-inc/60) > 1e-9 {
		t.Errorf("rate=%v, increase=%v — rate 는 increase/초 여야 한다", rt, inc)
	}
}

// 점이 하나뿐이면 증가량을 알 수 없다. 0 을 내면 "증가 없음" 이라는 거짓이 된다.
func TestRangeFuncsNeedTwoPoints(t *testing.T) {
	s := mkSeries(Point{T: 0, V: 5})
	if _, ok := rangeFuncs["increase"](s, time.Minute); ok {
		t.Error("점 하나로 increase 가 값을 냈다")
	}
	if _, ok := rangeFuncs["rate"](s, time.Minute); ok {
		t.Error("점 하나로 rate 가 값을 냈다")
	}
}

func TestOverTimeFuncs(t *testing.T) {
	s := mkSeries(Point{T: 0, V: 3}, Point{T: 1000, V: 9}, Point{T: 2000, V: 6})
	cases := map[string]float64{
		"max_over_time":   9,
		"min_over_time":   3,
		"avg_over_time":   6,
		"count_over_time": 3,
		"last_over_time":  6,
	}
	for name, want := range cases {
		got, ok := rangeFuncs[name](s, time.Minute)
		if !ok {
			t.Errorf("%s 가 값을 내지 못했다", name)
			continue
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s=%v, want %v", name, got, want)
		}
	}
}

// changes 는 값이 바뀐 횟수다 — 같은 값이 이어지면 세지 않는다.
func TestChanges(t *testing.T) {
	s := mkSeries(Point{T: 0, V: 1}, Point{T: 1000, V: 1}, Point{T: 2000, V: 2}, Point{T: 3000, V: 1})
	got, ok := rangeFuncs["changes"](s, time.Minute)
	if !ok || got != 2 {
		t.Errorf("changes=%v ok=%v, want 2", got, ok)
	}
}

// 함수 결과는 더 이상 원래 메트릭이 아니므로 __name__ 이 남으면 안 된다.
func TestIncreaseDropsMetricName(t *testing.T) {
	st := mkStorage(srs("c", map[string]string{"n": "a"},
		Point{T: 0, V: 1}, Point{T: 30000, V: 5}))
	expr, err := Parse(`increase(c[1m])`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := NewEngine().Eval(st, expr, 60000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	v := got.(Vector)
	if len(v) != 1 {
		t.Fatalf("결과 %d 개, want 1: %+v", len(v), v)
	}
	if v[0].Labels.Get(labelsMetricNameForTest) != "" {
		t.Errorf("__name__ 이 남았다: %+v", v[0].Labels)
	}
	if v[0].Labels.Get("n") != "a" {
		t.Errorf("다른 라벨은 유지돼야 한다: %+v", v[0].Labels)
	}
}

// absent 는 데이터가 없을 때만 1 을 낸다 — 스크레이프가 끊긴 것을 잡는 용도다.
func TestAbsent(t *testing.T) {
	empty := mkStorage()
	expr, _ := Parse(`absent(missing_metric)`)
	got, err := NewEngine().Eval(empty, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if v := got.(Vector); len(v) != 1 || v[0].V != 1 {
		t.Errorf("없는 메트릭에 absent=%+v, want 1 하나", v)
	}

	st := mkStorage(srs("present", nil, Point{T: 1000, V: 1}))
	expr2, _ := Parse(`absent(present)`)
	got2, _ := NewEngine().Eval(st, expr2, 1000)
	if v := got2.(Vector); len(v) != 0 {
		t.Errorf("있는 메트릭에 absent=%+v, want 빈 벡터", v)
	}
}

func TestRangeFuncNeedsMatrixArgument(t *testing.T) {
	st := mkStorage(srs("c", nil, Point{T: 1000, V: 1}))
	expr, _ := Parse(`increase(c)`) // range 없이
	if _, err := NewEngine().Eval(st, expr, 1000); err == nil {
		t.Error("range vector 없이 increase 가 통과했다")
	}
}
