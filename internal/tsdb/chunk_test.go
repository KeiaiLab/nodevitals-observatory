package tsdb

import (
	"encoding/binary"
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

// 추가 테스트: ChunkFromBytes 에러 처리 (샘플수 과대)
func TestChunkFromBytes_샘플수가_과대하면_거부한다(t *testing.T) {
	c := NewChunk()
	for i := 0; i < 10; i++ {
		if err := c.Append(int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	raw := c.Bytes()
	// 헤더의 샘플 수를 실제보다 크게 조작하면 디코더가 스트림 끝을 넘어
	// 읽으려 해 EOF 가 나고, ChunkFromBytes 가 ErrInvalidChunk 를 낸다.
	binary.BigEndian.PutUint16(raw[:2], 200)

	if _, err := ChunkFromBytes(raw); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("과대 샘플 수는 ErrInvalidChunk 여야 한다, got %v", err)
	}
}

// 추가 테스트: ChunkFromBytes 에러 처리 (샘플수 과소)
func TestChunkFromBytes_샘플수가_과소하면_조용히_잘린다(t *testing.T) {
	// 알려진 한계를 고정하는 테스트다. 헤더가 실제보다 적은 샘플 수를 주장하면
	// 디코더는 그만큼만 읽고 정상 종료한다 — 청크 계층은 자기 기술 헤더를
	// 신뢰하며, 저장 매체 손상 탐지는 상위 계층(WAL 의 CRC32C, 블록의 무결성
	// 검사)의 책임이다. 이 동작이 바뀌면 이 테스트가 알려준다.
	c := NewChunk()
	for i := 0; i < 10; i++ {
		if err := c.Append(int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	raw := c.Bytes()
	binary.BigEndian.PutUint16(raw[:2], 3)

	restored, err := ChunkFromBytes(raw)
	if err != nil {
		t.Fatalf("과소 샘플 수는 현 설계상 에러가 아니다: %v", err)
	}
	if restored.NumSamples() != 3 {
		t.Fatalf("헤더가 주장한 만큼만 읽어야 한다: got %d", restored.NumSamples())
	}
}

// 추가 테스트: ChunkFromBytes 에러 처리 (빈 청크)
func TestChunkFromBytes_헤더만_있는_빈_청크(t *testing.T) {
	// n == 0 분기 — minT/maxT 복원 루프를 건너뛰고 즉시 반환한다.
	restored, err := ChunkFromBytes([]byte{0, 0})
	if err != nil {
		t.Fatalf("빈 청크는 유효하다: %v", err)
	}
	if restored.NumSamples() != 0 {
		t.Fatalf("NumSamples: got %d, want 0", restored.NumSamples())
	}
	if restored.Iterator().Next() {
		t.Fatal("빈 청크에서 샘플이 나왔다")
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
