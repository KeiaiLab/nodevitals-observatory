package tsdb

import (
	"io"
	"testing"
)

func TestBstream_비트를_쓴_순서대로_읽는다(t *testing.T) {
	var b bstream
	want := []bool{true, false, true, true, false, false, false, true, true}
	for _, bit := range want {
		b.writeBit(bit)
	}

	r := newBReader(b.bytes())
	for i, w := range want {
		got, err := r.readBit()
		if err != nil {
			t.Fatalf("비트 %d 읽기 실패: %v", i, err)
		}
		if got != w {
			t.Fatalf("비트 %d: got %v, want %v", i, got, w)
		}
	}
}

func TestBstream_다중비트_값을_왕복한다(t *testing.T) {
	cases := []struct {
		v     uint64
		nbits int
	}{
		{0, 1}, {1, 1}, {5, 3}, {255, 8}, {256, 9},
		{1 << 20, 21}, {^uint64(0), 64}, {0, 64},
	}
	var b bstream
	for _, c := range cases {
		b.writeBits(c.v, c.nbits)
	}

	r := newBReader(b.bytes())
	for i, c := range cases {
		got, err := r.readBits(c.nbits)
		if err != nil {
			t.Fatalf("케이스 %d 읽기 실패: %v", i, err)
		}
		if got != c.v {
			t.Fatalf("케이스 %d: got %d, want %d", i, got, c.v)
		}
	}
}

func TestBstream_스트림_끝에서_EOF를_낸다(t *testing.T) {
	var b bstream
	b.writeBit(true)

	r := newBReader(b.bytes())
	for i := 0; i < 8; i++ {
		if _, err := r.readBit(); err != nil {
			t.Fatalf("첫 바이트 안에서 실패하면 안 된다 (비트 %d): %v", i, err)
		}
	}
	if _, err := r.readBit(); err != io.EOF {
		t.Fatalf("9번째 비트는 EOF 여야 한다, got %v", err)
	}
}
