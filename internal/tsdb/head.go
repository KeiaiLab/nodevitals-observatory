package tsdb

import (
	"errors"
	"fmt"
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"math"
	"sync"
)

// ErrUnknownRef 는 등록되지 않은 시리즈 ref 로 append 를 시도했을 때 난다.
// WAL 재생 중 시리즈 레코드가 손상돼 사라진 경우가 대표적이다.
var ErrUnknownRef = errors.New("tsdb: 알 수 없는 시리즈 ref")

// memSeries 는 head 안의 시리즈 하나다. 열린 청크에 append 하다가 청크가
// 차면 새 청크를 연다.
type memSeries struct {
	mtx sync.Mutex

	ref    uint64
	lset   labels.Labels
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

// register 는 이미 h.mtx 를 쥔 상태에서 호출한다 — 시리즈를 만들어 세 자료구조
// (series / hashes / postings)에 한꺼번에 등록한다. 등록처가 늘거나 바뀔 때
// 한 곳만 고치면 되도록 두 생성 경로가 이 함수를 공유한다.
func (h *Head) register(ref uint64, lset labels.Labels) *memSeries {
	s := &memSeries{ref: ref, lset: lset.Copy()}
	h.series[ref] = s
	hash := s.lset.Hash()
	h.hashes[hash] = append(h.hashes[hash], s)
	h.postings.Add(ref, s.lset)
	return s
}

func NewHead() *Head {
	h := &Head{}
	h.Reset()
	return h
}

// Reset 은 head 를 비운다.
//
// **호출자는 진행 중인 Append/AppendRef 가 없음을 보장해야 한다.** Head 자체는
// 그 배타성을 강제하지 않는다 — getOrCreate 가 시리즈 포인터를 돌려준 뒤에는
// h.mtx 를 놓으므로, 그 사이 Reset 이 끼어들면 호출자는 색인에서 사라진
// 시리즈에 계속 쓰게 되고 이어진 observe 가 새로 리셋된 시간 경계를 오염시킨다.
// 실제 사용에서는 DB(Task 14)가 자신의 잠금 안에서 Compact→Reset 을 수행해
// 이 조건을 만족시킨다.
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
func (h *Head) getOrCreate(lset labels.Labels) *memSeries {
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
	return h.register(h.lastRef, lset)
}

// GetOrCreateWithRef 는 WAL 재생 전용이다. 기록된 ref 를 그대로 되살려
// 이후 recSamples 레코드가 같은 ref 로 붙을 수 있게 한다.
func (h *Head) GetOrCreateWithRef(ref uint64, lset labels.Labels) *memSeries {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	if s, ok := h.series[ref]; ok {
		return s
	}
	s := h.register(ref, lset)
	if ref > h.lastRef {
		h.lastRef = ref
	}
	return s
}

func (h *Head) Append(lset labels.Labels, t int64, v float64) (uint64, error) {
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
		return fmt.Errorf("%w: %d", ErrUnknownRef, ref)
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
func (h *Head) Select(ms ...*labels.Matcher) []*memSeries {
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
