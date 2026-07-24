package tsdb

import (
	"fmt"
	"math"
	"sync"
)

// memSeries 는 head 안의 시리즈 하나다. 열린 청크에 append 하다가 청크가
// 차면 새 청크를 연다.
type memSeries struct {
	mtx sync.Mutex

	ref    uint64
	lset   Labels
	chunks []*Chunk

	minT, maxT int64
}

func (s *memSeries) append(t int64, v float64) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if len(s.chunks) == 0 {
		s.chunks = append(s.chunks, NewChunk())
		s.minT = t
	}
	c := s.chunks[len(s.chunks)-1]
	if c.Full() {
		c = NewChunk()
		s.chunks = append(s.chunks, c)
	}
	if err := c.Append(t, v); err != nil {
		return err
	}
	if t > s.maxT {
		s.maxT = t
	}
	return nil
}

// Head 는 최근 구간을 메모리에 들고 있는 쓰기 대상이다. 디스크 표현은
// 없다 — 내구성은 WAL(Task 9)이, 영속성은 블록(Task 11)이 담당한다.
type Head struct {
	mtx sync.RWMutex

	series   map[uint64]*memSeries   // ref → 시리즈
	hashes   map[uint64][]*memSeries // 라벨 해시 → 시리즈들(해시 충돌 대비)
	postings *memPostings

	lastRef    uint64
	minT, maxT int64
}

func NewHead() *Head {
	h := &Head{}
	h.Reset()
	return h
}

func (h *Head) Reset() {
	h.mtx.Lock()
	defer h.mtx.Unlock()
	h.series = map[uint64]*memSeries{}
	h.hashes = map[uint64][]*memSeries{}
	h.postings = newMemPostings()
	h.lastRef = 0
	h.minT = math.MaxInt64
	h.maxT = math.MinInt64
}

// getOrCreate 는 라벨셋에 해당하는 시리즈를 찾거나 만든다. 해시가 같아도
// 라벨셋 원본을 비교해 충돌을 걸러낸다.
func (h *Head) getOrCreate(lset Labels) *memSeries {
	hash := lset.Hash()

	h.mtx.RLock()
	for _, s := range h.hashes[hash] {
		if s.lset.Equal(lset) {
			h.mtx.RUnlock()
			return s
		}
	}
	h.mtx.RUnlock()

	h.mtx.Lock()
	defer h.mtx.Unlock()
	// 잠금을 놓았던 사이에 다른 고루틴이 만들었을 수 있다.
	for _, s := range h.hashes[hash] {
		if s.lset.Equal(lset) {
			return s
		}
	}
	h.lastRef++
	s := &memSeries{ref: h.lastRef, lset: lset.Copy()}
	h.series[s.ref] = s
	h.hashes[hash] = append(h.hashes[hash], s)
	h.postings.Add(s.ref, s.lset)
	return s
}

// GetOrCreateWithRef 는 WAL 재생 전용이다. 기록된 ref 를 그대로 되살려
// 이후 recSamples 레코드가 같은 ref 로 붙을 수 있게 한다.
func (h *Head) GetOrCreateWithRef(ref uint64, lset Labels) *memSeries {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	if s, ok := h.series[ref]; ok {
		return s
	}
	s := &memSeries{ref: ref, lset: lset.Copy()}
	h.series[ref] = s
	h.hashes[lset.Hash()] = append(h.hashes[lset.Hash()], s)
	h.postings.Add(ref, s.lset)
	if ref > h.lastRef {
		h.lastRef = ref
	}
	return s
}

func (h *Head) Append(lset Labels, t int64, v float64) (uint64, error) {
	s := h.getOrCreate(lset)
	if err := s.append(t, v); err != nil {
		return 0, err
	}
	h.observe(t)
	return s.ref, nil
}

func (h *Head) AppendRef(ref uint64, t int64, v float64) error {
	h.mtx.RLock()
	s, ok := h.series[ref]
	h.mtx.RUnlock()
	if !ok {
		return fmt.Errorf("tsdb: 알 수 없는 시리즈 ref %d", ref)
	}
	if err := s.append(t, v); err != nil {
		return err
	}
	h.observe(t)
	return nil
}

func (h *Head) observe(t int64) {
	h.mtx.Lock()
	if t < h.minT {
		h.minT = t
	}
	if t > h.maxT {
		h.maxT = t
	}
	h.mtx.Unlock()
}

func (h *Head) Series(ref uint64) *memSeries {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	return h.series[ref]
}

func (h *Head) NumSeries() int {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	return len(h.series)
}

func (h *Head) MinTime() int64 {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	if h.minT == math.MaxInt64 {
		return 0
	}
	return h.minT
}

func (h *Head) MaxTime() int64 {
	h.mtx.RLock()
	defer h.mtx.RUnlock()
	if h.maxT == math.MinInt64 {
		return 0
	}
	return h.maxT
}

// Select 는 매처를 모두 만족하는 시리즈를 ref 오름차순으로 낸다. 후보
// 선별은 selectRefs 가, 최종 판정은 matchesAll 이 한다 — 블록(Task 11)도
// 같은 두 함수를 쓰므로 head 와 블록의 질의 의미론이 갈라지지 않는다.
// 매처를 하나도 주지 않으면 전체 시리즈가 나온다.
func (h *Head) Select(ms ...*Matcher) []*memSeries {
	h.mtx.RLock()
	defer h.mtx.RUnlock()

	refs := selectRefs(h.postings, ms)
	out := make([]*memSeries, 0, len(refs))
	for _, id := range refs {
		s := h.series[id]
		if s != nil && matchesAll(s.lset, ms) {
			out = append(out, s)
		}
	}
	return out
}
