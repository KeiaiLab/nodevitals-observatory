package tsdb

import "io"

// bstream 은 비트 단위 append-only 스트림이다. 한 바이트 안에서는 MSB 부터
// 채운다 — Gorilla 논문과 Prometheus 구현의 관례를 따르며, 이 순서가 어긋나면
// 인코딩된 청크를 서로 읽을 수 없다.
type bstream struct {
	stream []byte
	count  uint8 // 마지막 바이트에 남은 여유 비트 수 (0 이면 새 바이트가 필요)
}

func (b *bstream) writeBit(bit bool) {
	if b.count == 0 {
		b.stream = append(b.stream, 0)
		b.count = 8
	}
	if bit {
		b.stream[len(b.stream)-1] |= 1 << (b.count - 1)
	}
	b.count--
}

// writeBits 는 u 의 하위 nbits 비트를 MSB 부터 기록한다. 음수를 int64 →
// uint64 로 캐스팅해 넘기면 2의 보수 하위 비트가 그대로 기록되므로,
// 디코더에서 부호를 복원할 수 있다 (encoding_ts.go 참조).
func (b *bstream) writeBits(u uint64, nbits int) {
	u <<= 64 - uint(nbits)
	for nbits > 0 {
		b.writeBit(u>>63 == 1)
		u <<= 1
		nbits--
	}
}

func (b *bstream) bytes() []byte { return b.stream }

// breader 는 bstream 이 쓴 바이트를 같은 순서로 읽는다.
type breader struct {
	stream []byte
	idx    int   // 다음에 읽을 바이트 인덱스
	count  uint8 // 현재 바이트에 남은 비트 수
}

func newBReader(s []byte) *breader { return &breader{stream: s} }

func (r *breader) readBit() (bool, error) {
	if r.count == 0 {
		if r.idx >= len(r.stream) {
			return false, io.EOF
		}
		r.idx++
		r.count = 8
	}
	bit := r.stream[r.idx-1]&(1<<(r.count-1)) != 0
	r.count--
	return bit, nil
}

func (r *breader) readBits(nbits int) (uint64, error) {
	var u uint64
	for i := 0; i < nbits; i++ {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		u <<= 1
		if bit {
			u |= 1
		}
	}
	return u, nil
}
