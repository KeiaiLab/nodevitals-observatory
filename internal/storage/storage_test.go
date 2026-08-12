package storage

import (
	"testing"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

func newDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func add(t *testing.T, db *tsdb.DB, name string, kv map[string]string, ts int64, v float64) {
	t.Helper()
	m := map[string]string{labels.MetricName: name}
	for k, val := range kv {
		m[k] = val
	}
	if err := db.Append(labels.LabelsFromMap(m), ts, v); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

// 질의 언어와 저장 엔진이 실제로 맞물리는지 — 두 계층을 따로 검증한 뒤
// 남는 유일한 위험이 이 이음매다.
func TestPromQLQueriesRealStorage(t *testing.T) {
	db := newDB(t)
	add(t, db, "x", map[string]string{"node": "a"}, 1000, 2)
	add(t, db, "x", map[string]string{"node": "b"}, 1000, 3)

	q := New(db)
	expr, err := promql.Parse(`sum(x)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := promql.NewEngine().Eval(q, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec, ok := got.(promql.Vector)
	if !ok {
		t.Fatalf("타입 %T, want Vector", got)
	}
	if len(vec) != 1 || vec[0].V != 5 {
		t.Errorf("sum(x)=%+v, want 5", vec)
	}
}

func TestPromQLRangeFunctionOverRealStorage(t *testing.T) {
	db := newDB(t)
	for i, v := range []float64{1, 4, 9} {
		add(t, db, "c", map[string]string{"n": "a"}, int64(i)*30000, v)
	}

	q := New(db)
	expr, _ := promql.Parse(`increase(c[2m])`)
	got, err := promql.NewEngine().Eval(q, expr, 60000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec := got.(promql.Vector)
	if len(vec) != 1 {
		t.Fatalf("결과 %d 개, want 1: %+v", len(vec), vec)
	}
	if vec[0].V != 8 { // 1→4→9 = +3 +5
		t.Errorf("increase=%v, want 8", vec[0].V)
	}
}

// 매처가 저장 계층까지 그대로 전달되는지 — 여기서 어긋나면 질의가 조용히
// 다른 시리즈를 고른다.
func TestLabelMatchersReachStorage(t *testing.T) {
	db := newDB(t)
	add(t, db, "x", map[string]string{"node": "a"}, 1000, 1)
	add(t, db, "x", map[string]string{"node": "b"}, 1000, 2)

	q := New(db)
	expr, _ := promql.Parse(`x{node="b"}`)
	got, _ := promql.NewEngine().Eval(q, expr, 1000)
	vec := got.(promql.Vector)
	if len(vec) != 1 || vec[0].V != 2 {
		t.Errorf("결과 %+v, want node=b 하나(값 2)", vec)
	}
}
