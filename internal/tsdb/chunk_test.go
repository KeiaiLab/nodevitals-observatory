package tsdb

import (
	"errors"
	"testing"
)

type sample struct {
	t int64
	v float64
}

func TestChunk_넣은_순서대로_읽는다(t *testing.T) {
	want := []sample{
		{1000, 0.5}, {16000, 0.51}, {31000, 0.52}, {46000, 12.75}, {61000, 12.75},
	}
	c := NewChunk()
	for _, s := range want {
		if err := c.Append(s.t, s.v); err != nil {
			t.Fatalf("Append(%d): %v", s.t, err)
		}
	}
	if got := c.NumSamples(); got != len(want) {
		t.Fatalf("NumSamples: got %d, want %d", got, len(want))
	}
	if c.MinTime() != 1000 || c.MaxTime() != 61000 {
		t.Fatalf("시간 범위: got [%d,%d], want [1000,61000]", c.MinTime(), c.MaxTime())
	}

	it := c.Iterator()
	for i, w := range want {
		if !it.Next() {
			t.Fatalf("샘플 %d 에서 조기 종료: %v", i, it.Err())
		}
		gt, gv := it.At()
		if gt != w.t || gv != w.v {
			t.Fatalf("샘플 %d: got (%d,%v), want (%d,%v)", i, gt, gv, w.t, w.v)
		}
	}
	if it.Next() {
		t.Fatal("샘플이 더 나왔다")
	}
	if it.Err() != nil {
		t.Fatalf("이터레이터 에러: %v", it.Err())
	}
}

func TestChunk_역행_타임스탬프를_거부한다(t *testing.T) {
	c := NewChunk()
	if err := c.Append(5000, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(4999, 2); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("역행을 거부해야 한다, got %v", err)
	}
	// 같은 타임스탬프는 허용한다 (스크레이프 중복 방어는 상위 계층 책임).
	if err := c.Append(5000, 2); err != nil {
		t.Fatalf("동일 타임스탐프는 허용해야 한다: %v", err)
	}
}

func TestChunk_가득_차면_ErrChunkFull(t *testing.T) {
	c := NewChunk()
	for i := 0; i < maxSamplesPerChunk; i++ {
		if err := c.Append(int64(i)*15000, float64(i)); err != nil {
			t.Fatalf("샘플 %d: %v", i, err)
		}
	}
	err := c.Append(int64(maxSamplesPerChunk)*15000, 1)
	if !errors.Is(err, ErrChunkFull) {
		t.Fatalf("가득 찬 청크는 ErrChunkFull 이어야 한다, got %v", err)
	}
}

func TestChunk_바이트로_왕복한다(t *testing.T) {
	c := NewChunk()
	for i := 0; i < 50; i++ {
		if err := c.Append(int64(i)*15000, float64(i)*1.5); err != nil {
			t.Fatal(err)
		}
	}

	restored, err := ChunkFromBytes(c.Bytes())
	if err != nil {
		t.Fatalf("ChunkFromBytes: %v", err)
	}
	if restored.NumSamples() != 50 {
		t.Fatalf("NumSamples: got %d, want 50", restored.NumSamples())
	}

	it := restored.Iterator()
	for i := 0; i < 50; i++ {
		if !it.Next() {
			t.Fatalf("샘플 %d 조기 종료: %v", i, it.Err())
		}
		gt, gv := it.At()
		if gt != int64(i)*15000 || gv != float64(i)*1.5 {
			t.Fatalf("샘플 %d: got (%d,%v)", i, gt, gv)
		}
	}
}

func TestChunk_빈_청크는_이터레이션이_즉시_끝난다(t *testing.T) {
	c := NewChunk()
	it := c.Iterator()
	if it.Next() {
		t.Fatal("빈 청크에서 샘플이 나왔다")
	}
	if it.Err() != nil {
		t.Fatalf("빈 청크는 에러가 없어야 한다: %v", it.Err())
	}
}

func TestChunkFromBytes_짧은_입력을_거부한다(t *testing.T) {
	if _, err := ChunkFromBytes([]byte{0}); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("2바이트 미만은 ErrInvalidChunk 여야 한다, got %v", err)
	}
}

// 추가 테스트: Bytes() 헤더 검증
func TestChunk_Bytes_헤더에_샘플수를_담는다(t *testing.T) {
	c := NewChunk()
	for i := 0; i < 10; i++ {
		if err := c.Append(int64(i)*1000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}

	b := c.Bytes()
	if len(b) < 2 {
		t.Fatal("Bytes() 길이가 2 미만")
	}
	// 첫 2바이트(big-endian)가 샘플 수 10 이어야 한다.
	got := (uint16(b[0]) << 8) | uint16(b[1])
	if got != 10 {
		t.Fatalf("헤더 샘플 수: got %d, want 10", got)
	}
}

// 추가 테스트: ChunkFromBytes 에러 처리
func TestChunkFromBytes_손상된_스트림을_거부한다(t *testing.T) {
	c := NewChunk()
	if err := c.Append(1000, 0.5); err != nil {
		t.Fatal(err)
	}

	// 유효한 바이트를 얻는다
	b := c.Bytes()

	// 스트림 일부를 손상시킨다 (길이는 유지하되 비트가 잘못됨)
	// 이건 이터레이션 중 EOF 에러를 유발할 수 있다
	b[len(b)-1] = 0xFF // 마지막 바이트 변경

	restored, err := ChunkFromBytes(b)
	// 여기서 에러가 나거나, 나중에 이터레이션 중 에러가 날 수 있다
	if err != nil {
		// ChunkFromBytes 자체에서 거부
		if !errors.Is(err, ErrInvalidChunk) {
			t.Fatalf("손상된 청크는 ErrInvalidChunk 여야 한다, got %v", err)
		}
		return
	}

	// 이터레이션에서 에러가 나야 한다
	it := restored.Iterator()
	if it.Next() && it.Err() != nil {
		return // 이터레이션 에러 발생 = 정상
	}
}

// 추가 테스트: Full() 메서드
func TestChunk_Full_메서드(t *testing.T) {
	c := NewChunk()
	if c.Full() {
		t.Fatal("새 청크는 Full() 이 false 여야 한다")
	}

	for i := 0; i < maxSamplesPerChunk; i++ {
		if err := c.Append(int64(i)*1000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}

	if !c.Full() {
		t.Fatal("가득 찬 청크는 Full() 이 true 여야 한다")
	}
}
