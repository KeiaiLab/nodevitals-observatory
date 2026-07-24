package tsdb

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

// MetricName 은 메트릭 이름을 담는 예약 라벨이다 (Prometheus 관례).
const MetricName = "__name__"

type Label struct {
	Name  string
	Value string
}

// Labels 는 항상 Name 오름차순으로 정렬된 라벨 집합이다. 이 불변식이
// 해시 안정성과 Equal 의 O(n) 비교를 보장하므로, 정렬되지 않은 Labels 를
// 만드는 경로를 두지 않는다 — 생성은 NewLabels/LabelsFromMap 만 쓴다.
type Labels []Label

func NewLabels(ls ...Label) Labels {
	out := make(Labels, len(ls))
	copy(out, ls)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func LabelsFromMap(m map[string]string) Labels {
	out := make(Labels, 0, len(m))
	for k, v := range m {
		out = append(out, Label{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (ls Labels) Get(name string) string {
	for _, l := range ls {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

// Hash 는 라벨셋의 안정적 해시다. 이름과 값 사이에 0xff 구분자를 넣어
// {a="bc"} 와 {ab="c"} 가 충돌하지 않게 한다. 해시 충돌 자체는 상위 계층
// (Head)이 라벨셋 원본 비교로 해소하므로 암호학적 강도는 필요 없다.
func (ls Labels) Hash() uint64 {
	h := fnv.New64a()
	for _, l := range ls {
		_, _ = h.Write([]byte(l.Name))
		_, _ = h.Write([]byte{0xff})
		_, _ = h.Write([]byte(l.Value))
		_, _ = h.Write([]byte{0xff})
	}
	return h.Sum64()
}

func (ls Labels) Equal(o Labels) bool {
	if len(ls) != len(o) {
		return false
	}
	for i := range ls {
		if ls[i] != o[i] {
			return false
		}
	}
	return true
}

func (ls Labels) String() string {
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range ls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(l.Value))
	}
	b.WriteByte('}')
	return b.String()
}

func (ls Labels) Copy() Labels {
	out := make(Labels, len(ls))
	copy(out, ls)
	return out
}
