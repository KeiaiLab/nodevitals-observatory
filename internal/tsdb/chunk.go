package tsdb

import (
	"encoding/binary"
	"errors"
)

// maxSamplesPerChunk 는 한 청크에 담는 샘플 수 상한이다. 15초 간격이면
// 30분치에 해당한다 — 청크가 너무 크면 부분 조회에도 전체를 풀어야 하고,
// 너무 작으면 첫 두 샘플의 비압축 오버헤드(16바이트)가 두드러진다.
const maxSamplesPerChunk = 120

var (
	ErrChunkFull    = errors.New("tsdb: 청크가 가득 참")
	ErrOutOfOrder   = errors.New("tsdb: 타임스탬프 역행")
	ErrInvalidChunk = errors.New("tsdb: 청크 바이트가 손상됨")
)

// Chunk 는 한 시리즈의 연속 샘플을 담는 append-only 압축 청크다.
// 동시 접근은 보호하지 않는다 — 소유자(memSeries)가 잠근다.
type Chunk struct {
	b      bstream
	tsEnc  tsEncoder
	valEnc valEncoder

	numSamples uint16
	minT, maxT int64
}

func NewChunk() *Chunk {
	return &Chunk{minT: 0, maxT: 0}
}

func (c *Chunk) Append(t int64, v float64) error {
	if c.Full() {
		return ErrChunkFull
	}
	if c.numSamples > 0 && t < c.maxT {
		return ErrOutOfOrder
	}
	if c.numSamples == 0 {
		c.minT = t
	}
	c.tsEnc.append(&c.b, t)
	c.valEnc.append(&c.b, v)
	c.maxT = t
	c.numSamples++
	return nil
}

func (c *Chunk) NumSamples() int { return int(c.numSamples) }
func (c *Chunk) MinTime() int64  { return c.minT }
func (c *Chunk) MaxTime() int64  { return c.maxT }
func (c *Chunk) Full() bool      { return c.numSamples >= maxSamplesPerChunk }

// Bytes 는 [2바이트 샘플 수][비트스트림] 형태로 직렬화한다. 샘플 수를 앞에
// 두어야 ChunkFromBytes 가 외부 메타 없이 자족적으로 복원된다.
// 반환값은 호출 시점의 독립 복사본이다 — 이후 Append 가 내부 스트림을
// 늘려도 이미 넘겨준 바이트는 바뀌지 않는다.
func (c *Chunk) Bytes() []byte {
	out := make([]byte, 2, 2+len(c.b.stream))
	binary.BigEndian.PutUint16(out, c.numSamples)
	return append(out, c.b.stream...)
}

// ChunkFromBytes 는 Bytes 의 역이다. 복원된 청크는 읽기 전용으로 쓴다
// (인코더 상태가 없으므로 Append 하면 스트림이 깨진다).
func ChunkFromBytes(b []byte) (*Chunk, error) {
	if len(b) < 2 {
		return nil, ErrInvalidChunk
	}
	n := binary.BigEndian.Uint16(b[:2])
	c := &Chunk{numSamples: n}
	c.b.stream = append([]byte(nil), b[2:]...)

	// minT/maxT 는 스트림을 훑어 채운다 — 블록 인덱스가 따로 들고 있지만,
	// 단독으로도 올바른 값을 답하도록 여기서 복원한다.
	if n > 0 {
		it := c.Iterator()
		first := true
		for it.Next() {
			t, _ := it.At()
			if first {
				c.minT = t
				first = false
			}
			c.maxT = t
		}
		if it.Err() != nil {
			return nil, ErrInvalidChunk
		}
	}
	return c, nil
}

func (c *Chunk) Iterator() *ChunkIterator {
	return &ChunkIterator{
		r:         newBReader(c.b.stream),
		remaining: int(c.numSamples),
	}
}

// ChunkIterator 는 청크의 샘플을 시간 오름차순으로 낸다.
type ChunkIterator struct {
	r         *breader
	tsDec     tsDecoder
	valDec    valDecoder
	remaining int

	t   int64
	v   float64
	err error
}

func (it *ChunkIterator) Next() bool {
	if it.err != nil || it.remaining == 0 {
		return false
	}
	t, err := it.tsDec.next(it.r)
	if err != nil {
		it.err = err
		return false
	}
	v, err := it.valDec.next(it.r)
	if err != nil {
		it.err = err
		return false
	}
	it.t, it.v = t, v
	it.remaining--
	return true
}

func (it *ChunkIterator) At() (int64, float64) { return it.t, it.v }
func (it *ChunkIterator) Err() error           { return it.err }
