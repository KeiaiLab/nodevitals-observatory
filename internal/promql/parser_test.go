package promql

import (
	"strings"
	"testing"
	"time"
)

func TestParseVectorSelector(t *testing.T) {
	n, err := Parse(`node_load1{node="e106",tier=~"co.*"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	vs, ok := n.(*VectorSelector)
	if !ok {
		t.Fatalf("타입 %T, want *VectorSelector", n)
	}
	if vs.Name != "node_load1" {
		t.Errorf("Name=%q", vs.Name)
	}
	if len(vs.Matchers) != 3 {
		t.Errorf("매처 %d 개, want 3: %+v", len(vs.Matchers), vs.Matchers)
	}
}

func TestParseRangeInsideCall(t *testing.T) {
	n, err := Parse(`increase(nodevitals_events_total[5m])`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, ok := n.(*Call)
	if !ok {
		t.Fatalf("타입 %T, want *Call", n)
	}
	if c.Func != "increase" {
		t.Errorf("Func=%q", c.Func)
	}
	ms, ok := c.Args[0].(*MatrixSelector)
	if !ok {
		t.Fatalf("인자 타입 %T, want *MatrixSelector", c.Args[0])
	}
	if ms.Range != 5*time.Minute {
		t.Errorf("Range=%v, want 5m", ms.Range)
	}
}

func TestParseAggregationByAndWithout(t *testing.T) {
	n, err := Parse(`sum by (node) (up)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a, ok := n.(*Aggregation)
	if !ok {
		t.Fatalf("타입 %T, want *Aggregation", n)
	}
	if a.Op != "sum" || !a.By || len(a.Grouping) != 1 || a.Grouping[0] != "node" {
		t.Errorf("집계 파싱 결과가 다르다: %+v", a)
	}

	n2, err := Parse(`count without (device) (up)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a2 := n2.(*Aggregation)
	if a2.By {
		t.Error("without 인데 By 가 true 다")
	}
}

// 곱하기가 더하기보다 먼저 묶여야 한다. 우선순위를 무시하면 값이 조용히 달라진다.
func TestParseBinaryPrecedence(t *testing.T) {
	n, err := Parse(`1 + 2 * 3`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	be, ok := n.(*BinaryExpr)
	if !ok {
		t.Fatalf("타입 %T, want *BinaryExpr", n)
	}
	if be.Op != "+" {
		t.Fatalf("최상위 연산자 %q, want +", be.Op)
	}
	rhs, ok := be.RHS.(*BinaryExpr)
	if !ok || rhs.Op != "*" {
		t.Errorf("오른쪽이 곱셈 노드가 아니다: %+v", be.RHS)
	}
}

func TestParseComparisonWithBool(t *testing.T) {
	n, err := Parse(`up > bool 0`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	be := n.(*BinaryExpr)
	if !be.ReturnBool {
		t.Error("bool modifier 가 반영되지 않았다")
	}
}

// 지원하지 않는 문법은 이름을 밝히며 거절해야 한다 — 조용히 무시하면
// 사용자는 다른 답을 받고도 알아채지 못한다.
func TestParseRejectsUnsupported(t *testing.T) {
	cases := map[string]string{
		`up offset 5m`:               "offset",
		`topk(3, up)`:                "topk",
		`histogram_quantile(0.9, x)`: "histogram_quantile",
	}
	for expr, want := range cases {
		_, err := Parse(expr)
		if err == nil {
			t.Errorf("%q 가 에러 없이 통과했다", expr)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q 의 에러가 %q 를 언급하지 않는다: %v", expr, want, err)
		}
	}
}
