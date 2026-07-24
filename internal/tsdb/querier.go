package tsdb

import "sort"

// Iterator 는 한 시리즈의 샘플을 시간 오름차순으로 낸다.
type Iterator interface {
	Next() bool
	At() (int64, float64)
	Err() error
}

// Series 는 라벨셋 하나에 대응하는 샘플 열이다.
type Series interface {
	Labels() Labels
	Iterator() Iterator
}

// chunkSource 는 청크를 지연 생성한다 — 블록 시리즈는 질의가 실제로
// 이터레이션할 때만 디스크를 읽는다.
type chunkSource func() (*Chunk, error)

type chainSeries struct {
	lset   Labels
	chunks []chunkSource
	mint   int64
	maxt   int64
}

func (s *chainSeries) Labels() Labels { return s.lset }

func (s *chainSeries) Iterator() Iterator {
	return &chainIterator{srcs: s.chunks, mint: s.mint, maxt: s.maxt}
}

// chainIterator 는 청크 여러 개를 순서대로 이어 순회하며 [mint,maxt] 밖
// 샘플을 걸러낸다. 청크는 시간순으로 배치돼 있다고 전제한다.
//
// 청크 "안"은 이미 시간 오름차순(Chunk.Append 의 OutOfOrder 가드)이지만,
// 청크 "경계"(예: 블록 마지막 청크 ↔ head 첫 청크)에서는 같은 타임스탬프가
// 겹칠 수 있다 — Compact 가 WriteBlock 의 블록 rename 을 끝낸 뒤
// wal.Truncate 전에 크래시하면, 정식 블록(.tmp 아님)과 WAL 이 같은 샘플을
// 함께 durable 로 갖게 되고 재오픈 시 head 가 그 WAL 을 재생해 블록과 head
// 가 같은 [minT,maxT] 를 갖는다. lastT/hasLast 로 직전에 "방출한"(Next 가
// true 를 반환하며 넘긴) 타임스탬프를 기억해 그 경계에서만 dedup 한다.
type chainIterator struct {
	srcs []chunkSource
	idx  int
	cur  *ChunkIterator

	mint, maxt int64
	t          int64
	v          float64
	err        error

	lastT   int64 // 직전에 방출한 샘플의 타임스탬프 (hasLast==true 일 때만 유효)
	hasLast bool  // 아직 하나도 방출 안 했으면 false — 첫 샘플은 무조건 통과
}

func (it *chainIterator) Next() bool {
	for {
		if it.err != nil {
			return false
		}
		if it.cur == nil {
			if it.idx >= len(it.srcs) {
				return false
			}
			c, err := it.srcs[it.idx]()
			it.idx++
			if err != nil {
				it.err = err
				return false
			}
			it.cur = c.Iterator()
		}
		if !it.cur.Next() {
			if err := it.cur.Err(); err != nil {
				it.err = err
				return false
			}
			it.cur = nil
			continue
		}
		t, v := it.cur.At()
		if t < it.mint {
			continue
		}
		if t > it.maxt {
			// 청크는 시간순이라 이후 샘플도 전부 범위 밖이지만, 다음
			// 청크가 더 이른 구간일 가능성은 없으므로 여기서 끝낸다.
			return false
		}
		// 단조 dedup: 직전에 "방출한" t 이하는 건너뛴다. mint 필터에 걸려
		// continue 한 샘플은 애초에 방출되지 않았으므로 lastT 를 건드리지
		// 않는다 — 방출 직전에만 갱신한다. Prometheus 의 chain iterator 와
		// 같은 의미론이다.
		if it.hasLast && t <= it.lastT {
			continue
		}
		it.t, it.v = t, v
		it.lastT, it.hasLast = t, true
		return true
	}
}

func (it *chainIterator) At() (int64, float64) { return it.t, it.v }
func (it *chainIterator) Err() error           { return it.err }

// Querier 는 head 와 블록들을 한 시간 창으로 함께 조회한다.
type Querier struct {
	mint, maxt int64
	head       *Head
	blocks     []*Block
}

func NewQuerier(mint, maxt int64, head *Head, blocks []*Block) *Querier {
	return &Querier{mint: mint, maxt: maxt, head: head, blocks: blocks}
}

// Select 는 매처를 만족하는 시리즈를 라벨셋 오름차순으로 낸다. 같은 라벨셋이
// 블록과 head 양쪽에 있으면 하나로 합친다 — 블록이 과거, head 가 최근이므로
// 블록 청크를 먼저 잇는다.
func (q *Querier) Select(ms ...*Matcher) []Series {
	merged := map[string]*chainSeries{}
	order := []string{}

	get := func(lset Labels) *chainSeries {
		// MapKey 를 쓴다 — String() 은 이름을 이스케이프하지 않아 서로 다른
		// 라벨셋이 같은 문자열을 낼 수 있고, 그러면 시리즈가 조용히 병합된다.
		key := lset.MapKey()
		s, ok := merged[key]
		if !ok {
			s = &chainSeries{lset: lset, mint: q.mint, maxt: q.maxt}
			merged[key] = s
			order = append(order, key)
		}
		return s
	}

	// 블록 먼저 — 오래된 블록부터.
	blocks := append([]*Block(nil), q.blocks...)
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].meta.MinTime < blocks[j].meta.MinTime
	})
	for _, b := range blocks {
		if b.meta.MaxTime < q.mint || b.meta.MinTime > q.maxt {
			continue
		}
		for _, bs := range b.Select(ms...) {
			cs := get(bs.Lset)
			for _, cr := range bs.Chunks {
				if cr.MaxT < q.mint || cr.MinT > q.maxt {
					continue
				}
				blk, ref := b, cr
				cs.chunks = append(cs.chunks, func() (*Chunk, error) {
					return blk.Chunk(ref)
				})
			}
		}
	}

	// head 는 그 뒤에 붙는다.
	if q.head != nil {
		for _, s := range q.head.Select(ms...) {
			cs := get(s.lset)
			// head 청크는 가변이다 — Append 가 마지막 청크의 bstream 을 늘리는
			// 중일 수 있으므로, s.mtx 아래에서 불변 스냅샷을 떠 담는다. 블록과
			// 달리 head 는 메모리에 있어 지연 읽기의 이점이 없다.
			s.mtx.Lock()
			for _, c := range s.chunks {
				if c.MaxTime() < q.mint || c.MinTime() > q.maxt {
					continue
				}
				snap, err := ChunkFromBytes(c.Bytes())
				if err != nil {
					// 자기 Bytes 를 자기 ChunkFromBytes 하는 경로라 정상적으로는
					// 나지 않는다. 나면 그 청크만 건너뛴다(부분 결과 > 패닉).
					continue
				}
				cs.chunks = append(cs.chunks, func() (*Chunk, error) {
					return snap, nil
				})
			}
			s.mtx.Unlock()
		}
	}

	sort.Strings(order)
	out := make([]Series, 0, len(order))
	for _, k := range order {
		s := merged[k]
		if len(s.chunks) == 0 {
			continue
		}
		out = append(out, s)
	}
	return out
}

// LabelNames 는 head 와 블록 전체에서 라벨 이름을 모은다.
//
// head 쪽은 q.head.postings 를 직접 읽지 않는다 — Compact 가 부르는
// head.Reset() 이 h.mtx.Lock 아래에서 h.postings 필드 자체를 새 memPostings
// 로 스왑하므로, 잠금 없이 그 필드를 읽으면 (memPostings 내부 mutex 와
// 무관하게) 필드 읽기/쓰기 race 가 된다. 대신 h.mtx 를 RLock 으로 지키는
// head.Select()(매처 없이 부르면 전체 시리즈, Task 계약)로 시리즈를 받아
// 그 lset 에서 모은다 — lset 은 등록 후 불변이라 잠금 밖에서 읽어도
// 안전하다(Select 안의 기존 병합 로직과 같은 전제). 블록은 OpenBlock 이후
// postings 필드가 다시 스왑되지 않는 불변 구조라 이 race 가 없어 그대로
// 둔다.
func (q *Querier) LabelNames() []string {
	set := map[string]struct{}{}
	if q.head != nil {
		for _, s := range q.head.Select() {
			for _, l := range s.lset {
				set[l.Name] = struct{}{}
			}
		}
	}
	for _, b := range q.blocks {
		for _, n := range b.postings.LabelNames() {
			set[n] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// LabelValues 는 LabelNames 와 같은 이유로 head.Select() 를 거친다.
func (q *Querier) LabelValues(name string) []string {
	set := map[string]struct{}{}
	if q.head != nil {
		for _, s := range q.head.Select() {
			if v := s.lset.Get(name); v != "" {
				set[v] = struct{}{}
			}
		}
	}
	for _, b := range q.blocks {
		for _, v := range b.postings.LabelValues(name) {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
