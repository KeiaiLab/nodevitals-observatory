package tsdb

import (
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"os"
	"path/filepath"
	"testing"
)

func TestWAL_기록한_시리즈와_샘플을_재생한다(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, defaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}

	ls1 := labels.NewLabels(labels.Label{labels.MetricName, "node_load1"}, labels.Label{"node", "e101"})
	ls2 := labels.NewLabels(labels.Label{labels.MetricName, "gpu_temp"}, labels.Label{"node", "e101"}, labels.Label{"device", "gpu0"})
	if err := w.LogSeries(1, ls1); err != nil {
		t.Fatal(err)
	}
	if err := w.LogSeries(2, ls2); err != nil {
		t.Fatal(err)
	}
	if err := w.LogSamples([]RefSample{{1, 1000, 0.5}, {2, 1000, 61.5}}); err != nil {
		t.Fatal(err)
	}
	if err := w.LogSamples([]RefSample{{1, 16000, 0.7}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	var gotSeries []labels.Labels
	var gotSamples []RefSample
	err = ReplayWAL(dir,
		func(ref uint64, ls labels.Labels) error {
			gotSeries = append(gotSeries, ls)
			return nil
		},
		func(ss []RefSample) error {
			gotSamples = append(gotSamples, ss...)
			return nil
		})
	if err != nil {
		t.Fatalf("ReplayWAL: %v", err)
	}

	if len(gotSeries) != 2 {
		t.Fatalf("시리즈 레코드: got %d, want 2", len(gotSeries))
	}
	if !gotSeries[0].Equal(ls1) || !gotSeries[1].Equal(ls2) {
		t.Fatalf("라벨셋이 왕복하지 않았다: %v", gotSeries)
	}
	if len(gotSamples) != 3 {
		t.Fatalf("샘플: got %d, want 3", len(gotSamples))
	}
	want := []RefSample{{1, 1000, 0.5}, {2, 1000, 61.5}, {1, 16000, 0.7}}
	for i, w := range want {
		if gotSamples[i] != w {
			t.Fatalf("샘플 %d: got %+v, want %+v", i, gotSamples[i], w)
		}
	}
}

func TestWAL_크기를_넘기면_세그먼트를_회전한다(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, 512) // 아주 작은 세그먼트
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if err := w.LogSamples([]RefSample{{uint64(i), int64(i) * 1000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	entries, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(entries) < 2 {
		t.Fatalf("세그먼트가 회전하지 않았다: %v", entries)
	}

	n := 0
	err = ReplayWAL(dir, func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if n != 200 {
		t.Fatalf("회전 후 재생 샘플 수: got %d, want 200", n)
	}
}

func TestWAL_재오픈시_기존_세그먼트에_이어쓴다(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSamples([]RefSample{{1, 1000, 1}})
	w.Close()

	w2, err := OpenWAL(dir, defaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	w2.LogSamples([]RefSample{{1, 2000, 2}})
	w2.Close()

	n := 0
	ReplayWAL(dir, func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil })
	if n != 2 {
		t.Fatalf("이어쓰기 후 샘플: got %d, want 2", n)
	}
}

func TestWAL_Truncate는_모든_세그먼트를_지운다(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSamples([]RefSample{{1, 1000, 1}})
	if err := w.Truncate(); err != nil {
		t.Fatal(err)
	}
	w.LogSamples([]RefSample{{1, 2000, 2}})
	w.Close()

	n := 0
	ReplayWAL(dir, func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil })
	if n != 1 {
		t.Fatalf("Truncate 후 샘플: got %d, want 1 (자른 뒤 쓴 것만)", n)
	}
}

func TestReplayWAL_빈_디렉터리는_에러가_아니다(t *testing.T) {
	if err := ReplayWAL(t.TempDir(),
		func(uint64, labels.Labels) error { return nil },
		func([]RefSample) error { return nil }); err != nil {
		t.Fatalf("빈 WAL 재생은 성공해야 한다: %v", err)
	}
}

func TestReplayWAL_없는_디렉터리는_에러가_아니다(t *testing.T) {
	if err := ReplayWAL(filepath.Join(t.TempDir(), "없음"),
		func(uint64, labels.Labels) error { return nil },
		func([]RefSample) error { return nil }); err != nil {
		t.Fatalf("미존재 WAL 재생은 성공해야 한다: %v", err)
	}
	_ = os.Remove
}

// 테스트 함정 점검: CRC 검증이 실제로 작동하는가?
// 기록된 바이트를 한 곳 손상시키면 재생이 멈춰야 한다.
func TestWAL_CRC_손상_감지_테스트(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSeries(1, labels.NewLabels(labels.Label{labels.MetricName, "test"}))
	w.LogSamples([]RefSample{{1, 1000, 0.5}})
	w.LogSamples([]RefSample{{1, 2000, 0.7}})
	w.Close()

	// 세그먼트 파일을 읽어서 첫 번째 레코드의 payload 부분을 손상시킨다
	segs, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(segs) == 0 {
		t.Fatal("세그먼트가 없다")
	}
	data, _ := os.ReadFile(segs[0])
	if len(data) < 20 {
		t.Fatal("데이터가 너무 작다")
	}

	// 첫 번째 레코드의 payload 부분(타입+길이 이후)을 수정
	// 포맷: [1B type][4B len][payload...][4B crc]
	// offset 5부터가 payload 시작, offset 12 정도를 손상
	if len(data) > 12 {
		data[12] ^= 0xff // 한 바이트를 뒤집는다
	}
	os.WriteFile(segs[0], data, 0o644)

	// 재생 시도 — CRC 불일치로 인해 첫 레코드 이후 멈춰야 한다
	nSeries := 0
	nSamples := 0
	err := ReplayWAL(dir,
		func(uint64, labels.Labels) error { nSeries++; return nil },
		func([]RefSample) error { nSamples++; return nil })

	// 손상된 레코드는 건너뛰고, 그 이후는 재생되지 않아야 한다
	// 첫 레코드(시리즈)가 손상됐으므로 재생이 멈춰야 함
	if err != nil {
		t.Fatalf("손상된 WAL 재생 중 에러: %v", err)
	}
	// CRC가 손상되었으므로 첫 번째 레코드부터 멈춰야 한다
	// 즉, nSeries와 nSamples가 기대값보다 작아야 한다
	if nSeries != 0 {
		t.Fatalf("CRC 손상된 첫 레코드는 읽히면 안 됨: got %d 시리즈, want 0", nSeries)
	}
	if nSamples != 0 {
		t.Fatalf("손상 후 샘플들도 읽히면 안 됨: got %d, want 0", nSamples)
	}
}

// 테스트 함정: 길이 필드 손상 감지
// 페이로드 길이를 실제보다 크게 조작하면 재생이 멈춘다
func TestWAL_길이_손상_감지_테스트(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSamples([]RefSample{{1, 1000, 0.5}})
	w.LogSamples([]RefSample{{1, 2000, 0.7}})
	w.Close()

	// 세그먼트 파일을 읽어서 길이 필드를 손상시킨다
	segs, _ := filepath.Glob(filepath.Join(dir, "*"))
	data, _ := os.ReadFile(segs[0])

	// 첫 레코드: [1B type][4B len][...]
	// offset 1~4에 길이가 있다. 이를 매우 크게 설정하면 end > len(data) 가 된다
	if len(data) > 5 {
		// 길이 필드를 0xFFFFFFFF로 설정 (매우 크게)
		data[1] = 0xFF
		data[2] = 0xFF
		data[3] = 0xFF
		data[4] = 0xFF
	}
	os.WriteFile(segs[0], data, 0o644)

	// 재생 시도 — 길이가 잘못되었으므로 멈춰야 한다
	nSamples := 0
	err := ReplayWAL(dir,
		func(uint64, labels.Labels) error { return nil },
		func([]RefSample) error { nSamples++; return nil })

	if err != nil {
		t.Fatalf("손상된 길이 필드 재생 중 에러: %v", err)
	}
	// 첫 레코드의 길이가 손상되었으므로 읽히면 안 된다
	if nSamples != 0 {
		t.Fatalf("길이 손상된 레코드: got %d 샘플 그룹, want 0", nSamples)
	}
}

// 테스트 함정: 빈 샘플 슬라이스는 레코드를 만들지 않는다
func TestWAL_빈_샘플_슬라이스는_레코드를_만들지_않는다(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSamples([]RefSample{{1, 1000, 0.5}})
	// 빈 슬라이스 호출 — 레코드가 생기면 안 된다
	err := w.LogSamples([]RefSample{})
	if err != nil {
		t.Fatalf("빈 샘플 로깅 실패: %v", err)
	}
	w.LogSamples([]RefSample{{1, 2000, 0.7}})
	w.Close()

	nSamples := 0
	ReplayWAL(dir,
		func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { nSamples += len(ss); return nil })
	// 두 개의 LogSamples 호출만 레코드가 생기고, 빈 호출은 레코드를 만들지 않아야 한다
	if nSamples != 2 {
		t.Fatalf("빈 슬라이스 무시: got %d 샘플, want 2", nSamples)
	}
}

// 테스트 함정: 라벨 값에 특수 바이트가 들어가도 왕복한다
func TestWAL_특수_바이트_라벨_왕복(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)

	// 개행, null, 고비트 바이트 등이 들어간 라벨
	// NewLabels가 정렬하므로 (Name 오름차순), "__name__" < "binary" < "key"
	specialLabels := labels.NewLabels(
		labels.Label{labels.MetricName, "test\nwith\nnewlines"},
		labels.Label{"key", "value\x00with\x00nulls"},
		labels.Label{"binary", "\xff\xfe\xfd"},
	)
	w.LogSeries(1, specialLabels)
	w.Close()

	var gotLabels labels.Labels
	ReplayWAL(dir,
		func(ref uint64, ls labels.Labels) error {
			if ref == 1 {
				gotLabels = ls
			}
			return nil
		},
		func([]RefSample) error { return nil })

	// 라벨이 정확히 왕복해야 한다
	if !gotLabels.Equal(specialLabels) {
		t.Fatalf("특수 바이트 라벨 손상: got %v, want %v", gotLabels, specialLabels)
	}
	if len(gotLabels) != 3 {
		t.Fatalf("라벨 개수: got %d, want 3", len(gotLabels))
	}
	// 정렬 순서: "__name__" < "binary" < "key"
	if gotLabels[0].Name != "__name__" {
		t.Fatalf("첫 번째 라벨 이름: got %q, want __name__", gotLabels[0].Name)
	}
	if gotLabels[0].Value != "test\nwith\nnewlines" {
		t.Fatalf("개행 바이트 손상: got %q", gotLabels[0].Value)
	}
	if gotLabels[1].Name != "binary" || gotLabels[1].Value != "\xff\xfe\xfd" {
		t.Fatalf("고비트 바이트 손상: got %q=%q", gotLabels[1].Name, gotLabels[1].Value)
	}
	if gotLabels[2].Name != "key" || gotLabels[2].Value != "value\x00with\x00nulls" {
		t.Fatalf("null 바이트 손상: got %q=%q", gotLabels[2].Name, gotLabels[2].Value)
	}
}

// 테스트 함정: 동어반복 단정 점검
// 절단된 레코드는 정상 상황이므로 nil 반환 (에러 아님)
func TestWAL_절단된_레코드는_정상_종료(t *testing.T) {
	dir := t.TempDir()
	w, _ := OpenWAL(dir, defaultSegmentSize)
	w.LogSamples([]RefSample{{1, 1000, 0.5}})
	w.LogSamples([]RefSample{{2, 2000, 0.7}})
	w.Close()

	// 파일을 잘라낸다 (마지막 바이트 몇 개 제거)
	segs, _ := filepath.Glob(filepath.Join(dir, "*"))
	data, _ := os.ReadFile(segs[0])
	if len(data) > 10 {
		data = data[:len(data)-5] // 끝에서 5바이트 제거
		os.WriteFile(segs[0], data, 0o644)
	}

	// 재생 시도 — 에러가 아니어야 한다 (절단은 정상)
	n := 0
	err := ReplayWAL(dir,
		func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil })

	if err != nil {
		t.Fatalf("절단된 WAL 재생 중 에러 반환: %v (정상 상황이어야 함)", err)
	}
	// 첫 번째 레코드는 완전하므로 재생되어야 하지만, 두 번째는 절단됐으므로 안 된다
	if n != 1 {
		t.Fatalf("절단 후 재생: got %d 샘플, want 1", n)
	}
}

// 리뷰 발견: 중간 세그먼트가 손상되면 그 지점에서 전체 재생을 끝내야 한다.
// 손상된 세그먼트를 무시하고 뒤를 계속 읽으면 시간순이 뒤엉킨다.
func TestReplayWAL_중간_세그먼트_손상시_이후를_읽지_않는다(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, 200) // 작은 세그먼트로 여러 개를 만든다
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := w.LogSamples([]RefSample{{1, int64(i) * 1000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	segs, err := listSegments(dir)
	if err != nil || len(segs) < 3 {
		t.Fatalf("세그먼트가 3개 이상이어야 한다: %v (%v)", segs, err)
	}

	// 첫 세그먼트 한가운데를 뒤집어 CRC 를 깨뜨린다.
	data, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 0 {
		data[len(data)/2] ^= 0xff
	}
	if err := os.WriteFile(segs[0], data, 0o644); err != nil {
		t.Fatal(err)
	}

	n := 0
	if err := ReplayWAL(dir, func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil }); err != nil {
		t.Fatal(err)
	}
	// 손상 지점 앞의 샘플만 나와야 한다 — 뒤 세그먼트를 읽었다면 30 에 가까워진다.
	if n == 0 {
		t.Fatal("손상 지점 앞의 샘플까지 모두 잃었다")
	}
	if n >= 25 {
		t.Fatalf("손상된 세그먼트 뒤를 계속 읽었다: %d 샘플 (뒤 세그먼트 미읽 기대)", n)
	}
}

// 리뷰 발견: listSegments 가 8글자 비-숫자 파일을 필터링하지 않으면
// OpenWAL 의 segIdx 파싱이 실패해 낮은 번호에 이어쓴다.
// 또한 부호를 포함한 파일명(예: -0000009)이 목록 맨 앞에 와서
// "손상 시 전체 중단"과 맞물리면 조용한 전량 손실이 날 수 있다.
func TestListSegments_숫자가_아닌_8글자_파일을_무시한다(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenWAL(dir, 200) // 작은 세그먼트로 여러 개 만든다
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := w.LogSamples([]RefSample{{1, int64(i) * 1000, float64(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()

	segs, err := listSegments(dir)
	if err != nil || len(segs) < 3 {
		t.Fatalf("세그먼트가 3개 이상이어야 한다: %v (%v)", segs, err)
	}

	// 오염 파일 2종: 비-숫자, 그리고 부호로 시작하는 8글자(사전순 맨 앞에 온다).
	for _, junk := range []string{"ZZZZZZZZ", "-0000009"} {
		if err := os.WriteFile(filepath.Join(dir, junk), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	segs2, err := listSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs2 {
		base := filepath.Base(s)
		if base == "ZZZZZZZZ" || base == "-0000009" {
			t.Fatalf("숫자가 아닌 파일이 세그먼트로 잡혔다: %v", segs2)
		}
	}
	if len(segs2) != len(segs) {
		t.Fatalf("오염 파일이 세그먼트 수를 바꿨다: %d → %d", len(segs), len(segs2))
	}

	// 오염 파일이 있어도 재생이 30개 전부 읽어야 한다(전체 중단 없이).
	n := 0
	if err := ReplayWAL(dir, func(uint64, labels.Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 30 {
		t.Fatalf("오염 파일 존재 시 재생 샘플: got %d, want 30", n)
	}
}
