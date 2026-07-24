package tsdb

import (
	"errors"
	"testing"
)

func lset(node, metric string) Labels {
	return NewLabels(Label{MetricName, metric}, Label{"node", node})
}

func TestHead_넣은_샘플을_되읽는다(t *testing.T) {
	h := NewHead()
	ref, err := h.Append(lset("e101", "node_load1"), 1000, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Append(lset("e101", "node_load1"), 16000, 0.7); err != nil {
		t.Fatal(err)
	}

	s := h.Series(ref)
	if s == nil {
		t.Fatal("ref 로 시리즈를 못 찾았다")
	}
	if s.minT != 1000 || s.maxT != 16000 {
		t.Fatalf("시간 범위: [%d,%d]", s.minT, s.maxT)
	}
	if h.NumSeries() != 1 {
		t.Fatalf("동일 라벨셋은 시리즈 1개여야 한다, got %d", h.NumSeries())
	}
	if h.MinTime() != 1000 || h.MaxTime() != 16000 {
		t.Fatalf("head 시간 범위: [%d,%d]", h.MinTime(), h.MaxTime())
	}
}

func TestHead_같은_라벨셋은_같은_ref를_준다(t *testing.T) {
	h := NewHead()
	r1, _ := h.Append(lset("e101", "node_load1"), 1000, 1)
	r2, _ := h.Append(lset("e101", "node_load1"), 2000, 2)
	if r1 != r2 {
		t.Fatalf("같은 라벨셋인데 ref 가 다르다: %d vs %d", r1, r2)
	}
	// 라벨 하나만 달라도 다른 시리즈다.
	r3, _ := h.Append(lset("e102", "node_load1"), 1000, 1)
	if r3 == r1 {
		t.Fatal("다른 라벨셋인데 ref 가 같다")
	}
	if h.NumSeries() != 2 {
		t.Fatalf("시리즈 2개여야 한다, got %d", h.NumSeries())
	}
}

func TestHead_매처로_시리즈를_고른다(t *testing.T) {
	h := NewHead()
	h.Append(NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"}, Label{"tier", "core"}), 1000, 1)
	h.Append(NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e102"}, Label{"tier", "core"}), 1000, 2)
	h.Append(NewLabels(Label{MetricName, "gpu_temp"}, Label{"node", "e101"}, Label{"tier", "gpu"}), 1000, 60)

	eq, _ := NewMatcher(MatchEqual, MetricName, "node_load1")
	got := h.Select(eq)
	if len(got) != 2 {
		t.Fatalf("__name__=node_load1: got %d 시리즈, want 2", len(got))
	}

	re, _ := NewMatcher(MatchRegexp, "node", "e10.")
	tierEq, _ := NewMatcher(MatchEqual, "tier", "gpu")
	got = h.Select(re, tierEq)
	if len(got) != 1 || got[0].lset.Get(MetricName) != "gpu_temp" {
		t.Fatalf("node=~e10. + tier=gpu: got %d 시리즈", len(got))
	}

	neq, _ := NewMatcher(MatchNotEqual, "tier", "core")
	got = h.Select(neq)
	if len(got) != 1 {
		t.Fatalf("tier!=core: got %d 시리즈, want 1", len(got))
	}
}

func TestHead_역행_샘플을_거부한다(t *testing.T) {
	h := NewHead()
	h.Append(lset("e101", "node_load1"), 5000, 1)
	_, err := h.Append(lset("e101", "node_load1"), 4000, 2)
	if !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("역행은 ErrOutOfOrder 여야 한다, got %v", err)
	}
}

func TestHead_청크가_차면_다음_청크로_넘어간다(t *testing.T) {
	h := NewHead()
	ls := lset("e101", "node_load1")
	total := maxSamplesPerChunk + 10
	for i := 0; i < total; i++ {
		if _, err := h.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatalf("샘플 %d: %v", i, err)
		}
	}
	ref, _ := h.Append(ls, int64(total)*15000, 0)
	s := h.Series(ref)
	if len(s.chunks) != 2 {
		t.Fatalf("청크가 2개여야 한다, got %d", len(s.chunks))
	}
	// 전체 샘플 수가 보존됐는지 확인.
	sum := 0
	for _, c := range s.chunks {
		sum += c.NumSamples()
	}
	if sum != total+1 {
		t.Fatalf("샘플 총합: got %d, want %d", sum, total+1)
	}
}

func TestHead_AppendRef는_기존_시리즈에_붙인다(t *testing.T) {
	h := NewHead()
	ref, _ := h.Append(lset("e101", "node_load1"), 1000, 1)
	if err := h.AppendRef(ref, 16000, 2); err != nil {
		t.Fatal(err)
	}
	if got := h.Series(ref).maxT; got != 16000 {
		t.Fatalf("maxT: got %d", got)
	}
	if err := h.AppendRef(9999, 1000, 1); err == nil {
		t.Fatal("없는 ref 는 에러여야 한다")
	}
}

func TestHead_Reset은_전부_비운다(t *testing.T) {
	h := NewHead()
	h.Append(lset("e101", "node_load1"), 1000, 1)
	h.Reset()
	if h.NumSeries() != 0 {
		t.Fatalf("Reset 후 시리즈가 남았다: %d", h.NumSeries())
	}
	eq, _ := NewMatcher(MatchEqual, "node", "e101")
	if got := h.Select(eq); len(got) != 0 {
		t.Fatalf("Reset 후 색인이 남았다: %d", len(got))
	}
}
