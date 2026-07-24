package tsdb

import "testing"

func TestRollupSeries_버킷당_네_시리즈를_만든다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	// 5분(300000ms) 버킷 하나에 값 1,2,3 을 넣는다.
	h.Append(ls, 0, 1)
	h.Append(ls, 15000, 2)
	h.Append(ls, 30000, 3)

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	out := RollupSeries(h.Select(m), rollupInterval)
	if len(out) != 4 {
		t.Fatalf("원본 1 시리즈 → 롤업 4 시리즈여야 한다, got %d", len(out))
	}

	byKind := map[string]*memSeries{}
	for _, s := range out {
		byKind[s.lset.Get(RollupLabel)] = s
	}
	for _, k := range []string{"sum", "count", "min", "max"} {
		if byKind[k] == nil {
			t.Fatalf("%s 롤업 시리즈가 없다", k)
		}
	}

	want := map[string]float64{"sum": 6, "count": 3, "min": 1, "max": 3}
	for kind, w := range want {
		s := byKind[kind]
		it := s.chunks[0].Iterator()
		if !it.Next() {
			t.Fatalf("%s: 샘플이 없다", kind)
		}
		ts, v := it.At()
		if ts != 0 {
			t.Fatalf("%s: 버킷 타임스탬프는 버킷 시작이어야 한다, got %d", kind, ts)
		}
		if v != w {
			t.Fatalf("%s: got %v, want %v", kind, v, w)
		}
		if it.Next() {
			t.Fatalf("%s: 버킷이 1개인데 샘플이 더 나왔다", kind)
		}
	}
}

func TestRollupSeries_버킷_경계를_정확히_나눈다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{"node", "e101"})
	h.Append(ls, 0, 10)                // 버킷 0
	h.Append(ls, rollupInterval-1, 20) // 버킷 0 (경계 직전)
	h.Append(ls, rollupInterval, 30)   // 버킷 1 (경계)

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	out := RollupSeries(h.Select(m), rollupInterval)

	var sumSeries *memSeries
	for _, s := range out {
		if s.lset.Get(RollupLabel) == "sum" {
			sumSeries = s
		}
	}
	it := sumSeries.chunks[0].Iterator()
	var got []sample
	for it.Next() {
		ts, v := it.At()
		got = append(got, sample{ts, v})
	}
	if len(got) != 2 {
		t.Fatalf("버킷 2개여야 한다: %v", got)
	}
	if got[0].t != 0 || got[0].v != 30 {
		t.Fatalf("버킷 0: got %+v, want {0 30}", got[0])
	}
	if got[1].t != rollupInterval || got[1].v != 30 {
		t.Fatalf("버킷 1: got %+v, want {%d 30}", got[1], rollupInterval)
	}
}

func TestRollupSeries_원본_라벨을_보존한다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "gpu_temp"}, Label{"node", "e22"}, Label{"device", "gpu3"})
	h.Append(ls, 0, 61)

	m, _ := NewMatcher(MatchEqual, "device", "gpu3")
	out := RollupSeries(h.Select(m), rollupInterval)
	s := out[0]
	if s.lset.Get(MetricName) != "gpu_temp" || s.lset.Get("node") != "e22" || s.lset.Get("device") != "gpu3" {
		t.Fatalf("원본 라벨이 유실됐다: %v", s.lset)
	}
	if s.lset.Get(RollupLabel) == "" {
		t.Fatal("__rollup__ 라벨이 없다")
	}
}

func TestRollupSeries_빈_입력은_빈_출력(t *testing.T) {
	if got := RollupSeries(nil, rollupInterval); len(got) != 0 {
		t.Fatalf("빈 입력: got %d", len(got))
	}
}

func TestRollupSeries_여러_버킷_테스트(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "metric"}, Label{"node", "e1"})
	// 3개 버킷에 각각 2개 샘플씩: 총 6개 샘플
	h.Append(ls, 0, 10)                                 // 버킷 0
	h.Append(ls, rollupInterval/2, 20)                  // 버킷 0
	h.Append(ls, rollupInterval, 30)                    // 버킷 1
	h.Append(ls, rollupInterval+rollupInterval/2, 40)   // 버킷 1
	h.Append(ls, 2*rollupInterval, 50)                  // 버킷 2
	h.Append(ls, 2*rollupInterval+rollupInterval/2, 60) // 버킷 2

	m, _ := NewMatcher(MatchEqual, "node", "e1")
	out := RollupSeries(h.Select(m), rollupInterval)
	if len(out) != 4 {
		t.Fatalf("원본 1 → 롤업 4, got %d", len(out))
	}

	var sumSeries *memSeries
	for _, s := range out {
		if s.lset.Get(RollupLabel) == "sum" {
			sumSeries = s
		}
	}

	// sum 시리즈는 3개 버킷이므로 샘플 3개
	var sums []float64
	for _, c := range sumSeries.chunks {
		it := c.Iterator()
		for it.Next() {
			_, v := it.At()
			sums = append(sums, v)
		}
	}
	if len(sums) != 3 {
		t.Fatalf("3개 버킷: got %d 샘플", len(sums))
	}
	wantSums := []float64{30, 70, 110} // 10+20, 30+40, 50+60
	for i, w := range wantSums {
		if sums[i] != w {
			t.Fatalf("버킷 %d sum: got %v, want %v", i, sums[i], w)
		}
	}
}

func TestRollupSeries_count로_평균_유도(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{"test", "avg"})
	// 버킷에 4개 값: 2, 3, 5, 10 (합 20, 개수 4, 평균 5)
	h.Append(ls, 0, 2)
	h.Append(ls, rollupInterval/4, 3)
	h.Append(ls, rollupInterval/2, 5)
	h.Append(ls, 3*rollupInterval/4, 10)

	m, _ := NewMatcher(MatchEqual, "test", "avg")
	out := RollupSeries(h.Select(m), rollupInterval)

	var sumS, countS *memSeries
	for _, s := range out {
		k := s.lset.Get(RollupLabel)
		if k == "sum" {
			sumS = s
		} else if k == "count" {
			countS = s
		}
	}

	sumIt := sumS.chunks[0].Iterator()
	countIt := countS.chunks[0].Iterator()

	sumIt.Next()
	countIt.Next()
	_, sumVal := sumIt.At()
	_, countVal := countIt.At()

	if sumVal != 20 {
		t.Fatalf("sum: got %v, want 20", sumVal)
	}
	if countVal != 4 {
		t.Fatalf("count: got %v, want 4", countVal)
	}
	// 검증: sum/count = 20/4 = 5.0 (평균)
	avg := sumVal / countVal
	if avg != 5.0 {
		t.Fatalf("avg = sum/count: got %v, want 5.0", avg)
	}
}
