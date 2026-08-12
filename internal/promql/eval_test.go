package promql

import "testing"

func TestEvalVectorSelectorTakesLatestSample(t *testing.T) {
	st := mkStorage(srs("up", map[string]string{"node": "e106"},
		Point{T: 1000, V: 1}, Point{T: 2000, V: 0}))

	expr, err := Parse(`up{node="e106"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := NewEngine().Eval(st, expr, 2000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec, ok := got.(Vector)
	if !ok {
		t.Fatalf("타입 %T, want Vector", got)
	}
	if len(vec) != 1 || vec[0].V != 0 {
		t.Errorf("결과 %+v, want 값 0 하나 (평가 시점의 최신 샘플)", vec)
	}
}

// lookback 창(5분) 밖의 샘플은 없는 것으로 봐야 한다. 그러지 않으면 몇 시간 전
// 죽은 시리즈가 계속 살아 있는 것처럼 보이고, 알림이 해소되지 않는다.
func TestEvalVectorSelectorIgnoresStaleSamples(t *testing.T) {
	st := mkStorage(srs("up", nil, Point{T: 1000, V: 1}))
	expr, _ := Parse(`up`)
	got, err := NewEngine().Eval(st, expr, 1000+6*60*1000) // 6분 뒤
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if v := got.(Vector); len(v) != 0 {
		t.Errorf("결과 %+v, want 빈 벡터 (5분 lookback 밖)", v)
	}
}

func TestEvalAggregationSumBy(t *testing.T) {
	st := mkStorage(
		srs("x", map[string]string{"node": "a", "dev": "1"}, Point{T: 1000, V: 2}),
		srs("x", map[string]string{"node": "a", "dev": "2"}, Point{T: 1000, V: 3}),
		srs("x", map[string]string{"node": "b", "dev": "1"}, Point{T: 1000, V: 10}),
	)
	expr, _ := Parse(`sum by (node) (x)`)
	got, err := NewEngine().Eval(st, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec := got.(Vector)
	if len(vec) != 2 {
		t.Fatalf("그룹 %d 개, want 2: %+v", len(vec), vec)
	}
	byNode := map[string]float64{}
	for _, s := range vec {
		byNode[s.Labels.Get("node")] = s.V
	}
	if byNode["a"] != 5 || byNode["b"] != 10 {
		t.Errorf("집계 결과 %+v, want a=5 b=10", byNode)
	}
	for _, s := range vec {
		if s.Labels.Get("dev") != "" {
			t.Errorf("by (node) 결과에 dev 라벨이 남았다: %+v", s.Labels)
		}
	}
}

// without 은 나열한 라벨과 __name__ 만 뺀다 — 나머지는 남아야 그룹이 유지된다.
func TestEvalAggregationWithout(t *testing.T) {
	st := mkStorage(
		srs("x", map[string]string{"node": "a", "dev": "1"}, Point{T: 1000, V: 2}),
		srs("x", map[string]string{"node": "a", "dev": "2"}, Point{T: 1000, V: 3}),
	)
	expr, _ := Parse(`sum without (dev) (x)`)
	got, _ := NewEngine().Eval(st, expr, 1000)
	vec := got.(Vector)
	if len(vec) != 1 || vec[0].V != 5 {
		t.Fatalf("결과 %+v, want node=a 하나에 5", vec)
	}
	if vec[0].Labels.Get("node") != "a" {
		t.Errorf("node 라벨이 사라졌다: %+v", vec[0].Labels)
	}
	if vec[0].Labels.Get(labelsMetricNameForTest) != "" {
		t.Errorf("without 결과에 __name__ 이 남았다: %+v", vec[0].Labels)
	}
}

func TestEvalComparisonFiltersAndBoolConverts(t *testing.T) {
	st := mkStorage(
		srs("x", map[string]string{"n": "a"}, Point{T: 1000, V: 5}),
		srs("x", map[string]string{"n": "b"}, Point{T: 1000, V: 1}),
	)

	expr, _ := Parse(`x > 3`)
	got, err := NewEngine().Eval(st, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if v := got.(Vector); len(v) != 1 || v[0].Labels.Get("n") != "a" {
		t.Errorf("필터 결과 %+v, want n=a 하나", got)
	}

	expr2, _ := Parse(`x > bool 3`)
	got2, err := NewEngine().Eval(st, expr2, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	v2 := got2.(Vector)
	if len(v2) != 2 {
		t.Fatalf("bool 결과 %d 개, want 2 (필터가 아니라 변환)", len(v2))
	}
	vals := map[string]float64{}
	for _, s := range v2 {
		vals[s.Labels.Get("n")] = s.V
	}
	if vals["a"] != 1 || vals["b"] != 0 {
		t.Errorf("bool 변환 결과 %+v, want a=1 b=0", vals)
	}
}

func TestEvalArithmeticWithScalar(t *testing.T) {
	st := mkStorage(srs("x", map[string]string{"n": "a"}, Point{T: 1000, V: 4}))
	expr, _ := Parse(`x * 2`)
	got, err := NewEngine().Eval(st, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	v := got.(Vector)
	if len(v) != 1 || v[0].V != 8 {
		t.Errorf("결과 %+v, want 8", v)
	}
}

// vector 끼리의 연산은 on/ignoring 매칭이 필요해 이번 범위 밖이다. 조용히
// 아무 짝이나 맞추면 값이 틀린 채로 나온다.
func TestEvalRejectsVectorToVector(t *testing.T) {
	st := mkStorage(srs("x", nil, Point{T: 1000, V: 1}))
	expr, _ := Parse(`x + x`)
	if _, err := NewEngine().Eval(st, expr, 1000); err == nil {
		t.Error("vector 끼리의 연산이 에러 없이 통과했다")
	}
}
