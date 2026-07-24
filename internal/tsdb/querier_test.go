package tsdb

import "testing"

func collect(t *testing.T, s Series) []sample {
	t.Helper()
	var out []sample
	it := s.Iterator()
	for it.Next() {
		ts, v := it.At()
		out = append(out, sample{ts, v})
	}
	if it.Err() != nil {
		t.Fatalf("이터레이터 에러: %v", it.Err())
	}
	return out
}

func TestQuerier_head만_있을_때_읽는다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		h.Append(ls, int64(i)*15000, float64(i))
	}

	q := NewQuerier(0, 1<<62, h, nil)
	m, _ := NewMatcher(MatchEqual, MetricName, "node_load1")
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("시리즈: got %d, want 1", len(got))
	}
	if s := collect(t, got[0]); len(s) != 10 {
		t.Fatalf("샘플: got %d, want 10", len(s))
	}
}

func TestQuerier_시간범위_밖_샘플을_잘라낸다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		h.Append(ls, int64(i)*15000, float64(i))
	}

	// [30000, 60000] → i=2,3,4 (30000,45000,60000)
	q := NewQuerier(30000, 60000, h, nil)
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	got := q.Select(m)
	s := collect(t, got[0])
	if len(s) != 3 {
		t.Fatalf("샘플: got %d, want 3 (%v)", len(s), s)
	}
	if s[0].t != 30000 || s[2].t != 60000 {
		t.Fatalf("범위: got [%d..%d]", s[0].t, s[2].t)
	}
}

func TestQuerier_블록과_head를_이어붙인다(t *testing.T) {
	base := t.TempDir()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})

	// 과거 구간은 블록으로 굳힌다.
	oldHead := NewHead()
	for i := 0; i < 10; i++ {
		oldHead.Append(ls, int64(i)*15000, float64(i))
	}
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	dir, err := WriteBlock(base, oldHead.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// 최근 구간은 head 에 남아 있다.
	h := NewHead()
	for i := 10; i < 15; i++ {
		h.Append(ls, int64(i)*15000, float64(i))
	}

	q := NewQuerier(0, 1<<62, h, []*Block{b})
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("같은 라벨셋은 시리즈 1개로 합쳐야 한다, got %d", len(got))
	}
	s := collect(t, got[0])
	if len(s) != 15 {
		t.Fatalf("샘플: got %d, want 15", len(s))
	}
	for i, sm := range s {
		if sm.t != int64(i)*15000 || sm.v != float64(i) {
			t.Fatalf("샘플 %d: got (%d,%v)", i, sm.t, sm.v)
		}
	}
}

func TestQuerier_라벨_이름과_값을_모아준다(t *testing.T) {
	h := NewHead()
	h.Append(NewLabels(Label{MetricName, "a"}, Label{"node", "e101"}), 1000, 1)
	h.Append(NewLabels(Label{MetricName, "b"}, Label{"tier", "gpu"}), 1000, 1)

	q := NewQuerier(0, 1<<62, h, nil)
	names := q.LabelNames()
	if len(names) != 3 || names[0] != MetricName || names[1] != "node" || names[2] != "tier" {
		t.Fatalf("LabelNames: got %v", names)
	}
	if vals := q.LabelValues(MetricName); len(vals) != 2 || vals[0] != "a" || vals[1] != "b" {
		t.Fatalf("LabelValues(__name__): got %v", vals)
	}
}

func TestQuerier_매칭_시리즈가_없으면_빈_결과(t *testing.T) {
	h := NewHead()
	h.Append(NewLabels(Label{"node", "e101"}), 1000, 1)

	q := NewQuerier(0, 1<<62, h, nil)
	m, _ := NewMatcher(MatchEqual, "node", "없음")
	if got := q.Select(m); len(got) != 0 {
		t.Fatalf("빈 결과여야 한다: %d", len(got))
	}
}

func TestQuerier_클로저_캡처_각각_다른_청크를_읽는다(t *testing.T) {
	// go.mod 가 1.22+ 를 선언하므로 range 변수(b, cr, c 등)는 반복마다 새로
	// 만들어져 blk, ref := b, cr 같은 수동 복사 없이도 클로저 캡처가 안전하다.
	// 이 테스트는 그 전제 위에서, 여러 청크(여러 블록 + head)를 가진 시리즈가
	// 각 청크를 올바른 순서·값으로 잇는지(캡처 정합)를 검증한다.

	base := t.TempDir()
	ls := NewLabels(Label{MetricName, "m"}, Label{"l", "v"})

	// 같은 시리즈, 서로 다른 시간대의 청크 2개를 만든다.
	h1 := NewHead()
	h1.Append(ls, 1000, 1.0)
	h1.Append(ls, 2000, 2.0)

	m, _ := NewMatcher(MatchEqual, MetricName, "m")
	dir1, err := WriteBlock(base, h1.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := OpenBlock(dir1)
	if err != nil {
		t.Fatal(err)
	}
	defer b1.Close()

	h2 := NewHead()
	h2.Append(ls, 3000, 3.0)
	h2.Append(ls, 4000, 4.0)

	dir2, err := WriteBlock(base, h2.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := OpenBlock(dir2)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()

	// 최근 데이터는 head 에
	h := NewHead()
	h.Append(ls, 5000, 5.0)

	// 모든 블록과 head 를 조회
	q := NewQuerier(0, 1<<62, h, []*Block{b1, b2})
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("시리즈 1개여야 한다: %d", len(got))
	}

	s := collect(t, got[0])
	if len(s) != 5 {
		t.Fatalf("샘플 5개여야 한다: got %d", len(s))
	}

	// 각 샘플이 정확한 값을 가지는지 확인 — 클로저가 마지막 청크만 읽었다면
	// 3.0, 4.0, 5.0 만 반복되거나 다른 값이 나온다.
	for i, sm := range s {
		expected := float64(i + 1) // 1.0, 2.0, 3.0, 4.0, 5.0
		if sm.v != expected {
			t.Fatalf("샘플 %d: got %.1f, want %.1f (클로저 캡처 실패 신호)", i, sm.v, expected)
		}
	}
}

func TestQuerier_여러_블록은_시간순서로_이어붙인다(t *testing.T) {
	base := t.TempDir()
	ls := NewLabels(Label{MetricName, "m"})

	// 블록 1: 시간 [0, 100)
	h1 := NewHead()
	for i := 0; i < 5; i++ {
		h1.Append(ls, int64(i*20), float64(i))
	}
	m, _ := NewMatcher(MatchEqual, MetricName, "m")
	dir1, err := WriteBlock(base, h1.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := OpenBlock(dir1)
	if err != nil {
		t.Fatal(err)
	}
	defer b1.Close()

	// 블록 2: 시간 [100, 200)
	h2 := NewHead()
	for i := 5; i < 10; i++ {
		h2.Append(ls, int64(i*20), float64(i))
	}
	dir2, err := WriteBlock(base, h2.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := OpenBlock(dir2)
	if err != nil {
		t.Fatal(err)
	}
	defer b2.Close()

	// 블록을 역순으로 넣어도 시간순으로 정렬되어야 한다.
	q := NewQuerier(0, 1<<62, nil, []*Block{b2, b1})
	got := q.Select(m)
	s := collect(t, got[0])

	if len(s) != 10 {
		t.Fatalf("샘플 10개여야 한다: got %d", len(s))
	}

	// 샘플이 시간 오름차순인지 확인
	for i, sm := range s {
		if sm.v != float64(i) {
			t.Fatalf("샘플 %d: got %.1f, want %.1f (시간순 정렬 실패)", i, sm.v, float64(i))
		}
		if sm.t != int64(i*20) {
			t.Fatalf("샘플 %d 시간: got %d, want %d", i, sm.t, int64(i*20))
		}
	}
}

func TestQuerier_시간범위로_청크를_걸러낸다(t *testing.T) {
	// 청크 시간 범위([MinT, MaxT])가 쿼리 범위와 겹치지 않으면 스킵되는지 확인
	base := t.TempDir()
	ls := NewLabels(Label{MetricName, "m"})

	h := NewHead()
	for i := 0; i < 5; i++ {
		h.Append(ls, int64(i*100), float64(i))
	}

	m, _ := NewMatcher(MatchEqual, MetricName, "m")
	dir, err := WriteBlock(base, h.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// [200, 300] 범위만 조회 — 샘플 2개만 있어야 함 (i=2,3 → t=200,300)
	q := NewQuerier(200, 300, nil, []*Block{b})
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("시리즈 1개여야 한다: %d", len(got))
	}

	s := collect(t, got[0])
	if len(s) != 2 {
		t.Fatalf("샘플 2개여야 한다: got %d (%v)", len(s), s)
	}
	if s[0].t != 200 || s[1].t != 300 {
		t.Fatalf("범위: got [%d, %d], want [200, 300]", s[0].t, s[1].t)
	}
}

func TestQuerier_시간범위_완전히_밖인_블록은_열지_않는다(t *testing.T) {
	base := t.TempDir()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})

	old := NewHead()
	for i := 0; i < 10; i++ {
		old.Append(ls, int64(i)*15000, float64(i)) // 0 ~ 135000
	}
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	dir, err := WriteBlock(base, old.Select(m), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// 블록 전체(0~135000)가 쿼리 범위(500000~600000) 밖이다.
	q := NewQuerier(500000, 600000, nil, []*Block{b})
	if got := q.Select(m); len(got) != 0 {
		t.Fatalf("범위 밖 블록은 결과가 비어야 한다: got %d 시리즈", len(got))
	}
}

func TestQuerier_전_청크가_범위밖인_head시리즈는_제외된다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		h.Append(ls, int64(i)*15000, float64(i)) // 0 ~ 135000
	}
	// 매처는 맞지만 시간창이 전부 뒤에 있다 — 병합 후 청크 0개라 제외돼야 한다.
	q := NewQuerier(500000, 600000, h, nil)
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	if got := q.Select(m); len(got) != 0 {
		t.Fatalf("전 청크가 범위 밖인 시리즈는 제외돼야 한다: got %d", len(got))
	}
}

func TestQuerier_여러_시리즈를_구분해_반환한다(t *testing.T) {
	h := NewHead()
	for _, node := range []string{"e101", "e102", "e103"} {
		ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", node})
		for i := 0; i < 5; i++ {
			h.Append(ls, int64(i)*15000, float64(i))
		}
	}
	q := NewQuerier(0, 1<<62, h, nil)
	m, _ := NewMatcher(MatchEqual, MetricName, "node_load1")
	got := q.Select(m)
	if len(got) != 3 {
		t.Fatalf("서로 다른 시리즈 3개여야 한다: got %d", len(got))
	}
	// 결정론적 순서인지 두 번 호출해 확인한다.
	got2 := q.Select(m)
	for i := range got {
		if got[i].Labels().MapKey() != got2[i].Labels().MapKey() {
			t.Fatalf("Select 순서가 결정론적이지 않다: 위치 %d", i)
		}
	}
	// 각 시리즈가 자기 노드의 샘플만 담는지(뒤섞이지 않는지) 확인한다.
	seen := map[string]bool{}
	for _, s := range got {
		node := s.Labels().Get("node")
		if seen[node] {
			t.Fatalf("노드 %s 가 중복 시리즈로 나왔다", node)
		}
		seen[node] = true
		n := 0
		it := s.Iterator()
		for it.Next() {
			n++
		}
		if n != 5 {
			t.Fatalf("노드 %s: 샘플 %d, want 5", node, n)
		}
	}
}

func TestQuerier_head_동시_Append중_조회가_안전하다(t *testing.T) {
	h := NewHead()
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	h.Append(ls, 0, 0) // 시드

	done := make(chan struct{})
	go func() {
		for i := 1; i < 500; i++ {
			h.Append(ls, int64(i)*15000, float64(i))
		}
		close(done)
	}()

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	for {
		select {
		case <-done:
			return
		default:
			q := NewQuerier(0, 1<<62, h, nil)
			for _, s := range q.Select(m) {
				it := s.Iterator()
				for it.Next() {
					_, _ = it.At()
				}
				if it.Err() != nil {
					t.Errorf("이터레이션 에러: %v", it.Err())
					return
				}
			}
		}
	}
}
