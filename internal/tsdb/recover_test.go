package tsdb

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeWALFixture 는 시리즈 1개에 n 개 샘플을 기록한 WAL 을 만든다.
func writeWALFixture(t *testing.T, dir string, n int) Labels {
	t.Helper()
	w, err := OpenWAL(dir, defaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	if err := w.LogSeries(1, ls); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := w.LogSamples([]RefSample{{1, int64(i) * 15000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return ls
}

func TestRecoverHead_온전한_WAL을_전부_복구한다(t *testing.T) {
	dir := t.TempDir()
	ls := writeWALFixture(t, dir, 100)

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("RecoverHead: %v", err)
	}
	if h.NumSeries() != 1 {
		t.Fatalf("시리즈: got %d, want 1", h.NumSeries())
	}
	s := h.Series(1)
	if s == nil {
		t.Fatal("ref 1 시리즈가 없다")
	}
	if !s.lset.Equal(ls) {
		t.Fatalf("라벨셋: got %v", s.lset)
	}
	total := 0
	for _, c := range s.chunks {
		total += c.NumSamples()
	}
	if total != 100 {
		t.Fatalf("복구 샘플: got %d, want 100", total)
	}
	if h.MaxTime() != 99*15000 {
		t.Fatalf("maxT: got %d, want %d", h.MaxTime(), 99*15000)
	}
}

func TestRecoverHead_잘린_WAL은_앞부분까지_복구한다(t *testing.T) {
	dir := t.TempDir()
	writeWALFixture(t, dir, 100)

	// 크래시 주입: 마지막 세그먼트를 임의 지점에서 자른다.
	segs, err := listSegments(dir)
	if err != nil || len(segs) == 0 {
		t.Fatalf("세그먼트를 못 찾았다: %v", err)
	}
	last := segs[len(segs)-1]
	st, _ := os.Stat(last)
	if err := os.Truncate(last, st.Size()-7); err != nil {
		t.Fatal(err)
	}

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("절단된 WAL 복구는 에러가 아니어야 한다: %v", err)
	}
	s := h.Series(1)
	if s == nil {
		t.Fatal("절단 전 시리즈는 살아 있어야 한다")
	}
	total := 0
	for _, c := range s.chunks {
		total += c.NumSamples()
	}
	// 마지막 레코드 하나만 잃는다 — 그 앞은 온전해야 한다.
	if total != 99 {
		t.Fatalf("절단 복구 샘플: got %d, want 99", total)
	}
}

func TestRecoverHead_CRC가_깨지면_그_지점에서_멈춘다(t *testing.T) {
	dir := t.TempDir()
	writeWALFixture(t, dir, 100)

	segs, _ := listSegments(dir)
	last := segs[len(segs)-1]
	data, err := os.ReadFile(last)
	if err != nil {
		t.Fatal(err)
	}
	// 파일 한가운데의 한 바이트를 뒤집는다.
	mid := len(data) / 2
	data[mid] ^= 0xff
	if err := os.WriteFile(last, data, 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("손상 WAL 복구는 에러가 아니어야 한다: %v", err)
	}
	s := h.Series(1)
	if s == nil {
		t.Fatal("손상 전 시리즈는 살아 있어야 한다")
	}
	total := 0
	for _, c := range s.chunks {
		total += c.NumSamples()
	}
	// 파일 절반에서 손상 발생 → 약 50개 샘플 복구
	if total != 49 {
		t.Fatalf("손상 지점 복구: got %d, want 49 (파일 절반까지 읽음)", total)
	}
}

func TestRecoverHead_WAL이_없으면_빈_head를_준다(t *testing.T) {
	h, err := RecoverHead(filepath.Join(t.TempDir(), "없음"))
	if err != nil {
		t.Fatalf("미존재 WAL: %v", err)
	}
	if h.NumSeries() != 0 {
		t.Fatalf("빈 head 여야 한다: %d", h.NumSeries())
	}
}

func TestRecoverHead_복구후_추가_append가_이어진다(t *testing.T) {
	dir := t.TempDir()
	writeWALFixture(t, dir, 10)

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 복구된 ref 1 다음 번호인 2가 나가야 한다. 증분이 정확해야 한다.
	newRef, err := h.Append(NewLabels(Label{MetricName, "gpu_temp"}, Label{"node", "e101"}), 20000, 61)
	if err != nil {
		t.Fatal(err)
	}
	if newRef != 2 {
		t.Fatalf("복구 후 append ref: got %d, want 2 (복구된 ref 1 다음)", newRef)
	}
	if h.NumSeries() != 2 {
		t.Fatalf("시리즈: got %d, want 2", h.NumSeries())
	}
}

// === 추가 테스트 — 브리프 예시의 빈 공간 채우기 ===

// TestRecoverHead_다양한_절단_위치도_앞부분을_복구한다 는 절단 지점을 여러 군데에서
// 테스트한다. 브리프는 size-7 한 지점만 자르므로, 이 테스트가 깨진다면
// RecoverHead 의 경계 처리 로직이 특정 위치에만 우연히 동작한다는 뜻이다.
func TestRecoverHead_다양한_절단_위치도_앞부분을_복구한다(t *testing.T) {
	// 절단 오프셋별 기대 샘플 수
	cases := []struct {
		cut  int64
		want int
	}{
		{3, 99},  // 작은 절단: 마지막 1개 샘플 잘림
		{7, 99},  // 작은 절단: 마지막 1개 샘플 잘림
		{15, 99}, // 중간 절단: 마지막 1개 샘플 잘림
		{50, 98}, // 큰 절단: 마지막 2개 샘플 잘림
	}
	for _, c := range cases {
		t.Run("offset="+strconv.FormatInt(c.cut, 10), func(t *testing.T) {
			dir := t.TempDir()
			writeWALFixture(t, dir, 100)

			segs, _ := listSegments(dir)
			last := segs[len(segs)-1]
			st, _ := os.Stat(last)
			if err := os.Truncate(last, st.Size()-c.cut); err != nil {
				t.Fatal(err)
			}

			h, err := RecoverHead(dir)
			if err != nil {
				t.Fatalf("절단 복구 실패: %v", err)
			}
			s := h.Series(1)
			if s == nil {
				t.Fatal("절단 전 시리즈가 사라졌다")
			}
			total := 0
			for _, ch := range s.chunks {
				total += ch.NumSamples()
			}
			// 절단 크기에 따라 기대값이 결정된다.
			if total != c.want {
				t.Fatalf("절단 offset=%d: 복구 샘플 got %d, want %d", c.cut, total, c.want)
			}
		})
	}
}

// TestRecoverHead_시리즈레코드_손상_샘플만_남은경우 는 시리즈 로그가 없거나
// 손상돼 버려진 경우를 테스트한다. 이 케이스가 깨진다면 RecoverHead 가
// "시리즈 없으면 샘플 버린다"는 계약을 위반한다는 뜻이다.
func TestRecoverHead_시리즈레코드_손상_샘플만_남은경우(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, defaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	// 시리즈 레코드를 기록하지 않고 바로 샘플만 기록한다.
	for i := 0; i < 10; i++ {
		if err := w.LogSamples([]RefSample{{1, int64(i) * 15000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("RecoverHead: %v", err)
	}
	// 시리즈 레코드가 없으므로 ref 1 이 없어야 한다.
	s := h.Series(1)
	if s != nil {
		t.Fatalf("라벨 없는 시리즈가 살아있다: %v", s.lset)
	}
	// NumSeries 가 0 이어야 한다.
	if h.NumSeries() != 0 {
		t.Fatalf("시리즈: got %d, want 0", h.NumSeries())
	}
}

// TestRecoverHead_역행_샘플은_버린다 는 시간 역행 샘플을 주입해 처리를 검증한다.
// 이 케이스가 깨진다면 RecoverHead 가 역행 샘플을 에러로 반환한다는 뜻이다.
func TestRecoverHead_역행_샘플은_버린다(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, defaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	ls := NewLabels(Label{MetricName, "metric"}, Label{"node", "n1"})
	if err := w.LogSeries(1, ls); err != nil {
		t.Fatal(err)
	}

	// 정상 샘플 5개: 0ms, 15000ms, 30000ms, 45000ms, 60000ms
	for i := 0; i < 5; i++ {
		if err := w.LogSamples([]RefSample{{1, int64(i) * 15000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}

	// 역행 샘플: 45000ms (더 이전 시간)
	if err := w.LogSamples([]RefSample{{1, 45000, 99.0}}); err != nil {
		t.Fatal(err)
	}

	// 마지막 정상 샘플: 75000ms
	if err := w.LogSamples([]RefSample{{1, 75000, 5.0}}); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("역행 샘플 포함 WAL 복구: %v", err)
	}

	s := h.Series(1)
	if s == nil {
		t.Fatal("시리즈가 없다")
	}

	total := 0
	for _, c := range s.chunks {
		total += c.NumSamples()
	}

	// 정상 샘플 5개 + 역행(버려짐) + 정상 1개 = 총 6개 예상
	// 역행 샘플이 버려지므로 정상 6개만 살아야 한다.
	if total != 6 {
		t.Fatalf("역행 샘플 복구: got %d, want 6 (정상 5개 + 뒤의 1개)", total)
	}
}

// TestRecoverHead_절단_후_MinTime_MaxTime_올바른가 는 절단된 WAL 을 복구한 후
// MinTime/MaxTime 이 올바른지 확인한다. 이 케이스가 깨진다면
// RecoverHead 가 head 의 시간 경계를 올바르게 갱신하지 않는다는 뜻이다.
func TestRecoverHead_절단_후_MinTime_MaxTime_올바른가(t *testing.T) {
	dir := t.TempDir()
	writeWALFixture(t, dir, 100) // 0ms ~ 1485000ms

	// 마지막 세그먼트를 자른다.
	segs, _ := listSegments(dir)
	last := segs[len(segs)-1]
	st, _ := os.Stat(last)
	if err := os.Truncate(last, st.Size()-7); err != nil {
		t.Fatal(err)
	}

	h, err := RecoverHead(dir)
	if err != nil {
		t.Fatalf("RecoverHead: %v", err)
	}

	s := h.Series(1)
	if s == nil {
		t.Fatal("시리즈 없음")
	}

	total := 0
	for _, c := range s.chunks {
		total += c.NumSamples()
	}

	// 99 샘플이 복구됐으므로 최대 시간은 98*15000 = 1470000 이어야 한다.
	if h.MaxTime() != 98*15000 {
		t.Fatalf("MaxTime: got %d, want %d (복구 샘플=%d)", h.MaxTime(), 98*15000, total)
	}

	// MinTime 은 0 이어야 한다 (첫 샘플의 시간).
	if h.MinTime() != 0 {
		t.Fatalf("MinTime: got %d, want 0", h.MinTime())
	}
}
