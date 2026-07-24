package tsdb

import (
	"sort"
	"sync"
)

// memPostings 는 라벨 → 시리즈 ID 역색인이다. 시리즈 수가 수만 규모라
// roaring bitmap 대신 정렬된 슬라이스로 충분하다 — 교집합이 선형 병합이고
// 메모리 지역성도 좋다.
type memPostings struct {
	mtx sync.RWMutex
	m   map[string]map[string][]uint64
	all []uint64
}

func newMemPostings() *memPostings {
	return &memPostings{m: map[string]map[string][]uint64{}}
}

// Add 는 시리즈를 색인에 넣는다. ID 는 단조 증가로 발급되므로 append 만으로
// 정렬 상태가 유지된다 — 상위 계층(Head)이 그 불변식을 지킨다.
func (p *memPostings) Add(id uint64, ls Labels) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	for _, l := range ls {
		vals, ok := p.m[l.Name]
		if !ok {
			vals = map[string][]uint64{}
			p.m[l.Name] = vals
		}
		vals[l.Value] = append(vals[l.Value], id)
	}
	p.all = append(p.all, id)
}

func (p *memPostings) Get(name, value string) []uint64 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return append([]uint64(nil), p.m[name][value]...)
}

func (p *memPostings) All() []uint64 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return append([]uint64(nil), p.all...)
}

func (p *memPostings) LabelNames() []string {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	out := make([]string, 0, len(p.m))
	for name := range p.m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (p *memPostings) LabelValues(name string) []string {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	out := make([]string, 0, len(p.m[name]))
	for v := range p.m[name] {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// 아래 세 함수는 정렬된 ID 슬라이스에 대한 집합 연산이다. 입력이 정렬돼
// 있다는 전제가 깨지면 조용히 틀린 답을 내므로, 정렬을 만드는 쪽(Add)과
// 쓰는 쪽(Select)을 같은 패키지에 묶어 둔다.

func intersect(a, b []uint64) []uint64 {
	out := []uint64{}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

func union(a, b []uint64) []uint64 {
	out := []uint64{}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}

func without(a, b []uint64) []uint64 {
	out := []uint64{}
	i, j := 0, 0
	for i < len(a) {
		switch {
		case j >= len(b) || a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			j++
		default:
			i++
			j++
		}
	}
	return out
}

// selectRefs 는 매처들로 후보 시리즈 ID 를 좁힌다. head 와 블록이 같은
// 질의 의미론을 갖도록 두 곳이 이 함수를 공유한다.
//
// 색인으로 좁힐 수 있는 것은 **값이 비지 않은** 같음·정규식 매처뿐이다:
//   - 부정 매처(!=, !~)는 "그 라벨이 아예 없는 시리즈"도 만족시킨다
//   - 빈 값 같음-매처(foo="")도 마찬가지다. 색인에는 빈 값 posting 이
//     없으므로 시드로 쓰면 결과가 통째로 사라진다
//
// 그런 매처만 있으면 전체를 후보로 돌려주고, 최종 판정은 호출자가
// matchesAll 로 한다.
func selectRefs(p *memPostings, ms []*Matcher) []uint64 {
	var candidates []uint64
	seeded := false

	for _, m := range ms {
		if m.Value == "" {
			continue
		}
		var ids []uint64
		switch m.Type {
		case MatchEqual:
			ids = p.Get(m.Name, m.Value)
		case MatchRegexp:
			for _, v := range p.LabelValues(m.Name) {
				if m.Matches(v) {
					ids = union(ids, p.Get(m.Name, v))
				}
			}
		default:
			continue
		}
		if !seeded {
			candidates, seeded = ids, true
		} else {
			candidates = intersect(candidates, ids)
		}
	}
	if !seeded {
		return p.All()
	}
	return candidates
}

// matchesAll 은 라벨셋이 매처 전부를 만족하는지 본다. 없는 라벨은 빈
// 문자열로 취급되므로 foo="" 매처가 "foo 라벨이 없음"을 뜻하게 된다.
func matchesAll(ls Labels, ms []*Matcher) bool {
	for _, m := range ms {
		if !m.Matches(ls.Get(m.Name)) {
			return false
		}
	}
	return true
}
