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

// Labels 는 Name 오름차순으로 정렬된 라벨 집합이다. 이 정렬은 성능 최적화가
// 아니라 **정확성의 전제**다 — Equal 은 위치별로 비교하고 Hash 는 슬라이스
// 순서 그대로 해시하므로, 정렬이 깨진 값은 논리적으로 같은 라벨셋과 Equal 이
// false 를 내고 Hash 도 달라진다. 그러면 같은 시리즈가 둘로 갈라진다.
//
// 생성은 반드시 NewLabels 또는 LabelsFromMap 을 쓴다. Labels 는 []Label 의
// named type 이라 Go 타입 시스템이 `Labels{{"b","1"},{"a","2"}}` 같은 리터럴
// 생성이나 `ls[i].Name` 직접 수정을 막지 못한다 — 그런 코드를 작성하지 말 것.
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

// MapKey 는 라벨셋을 맵 키로 쓰기 위한 직렬화다. 각 이름·값 앞에 길이를
// 붙이므로 서로 다른 라벨셋이 같은 키를 낼 수 없다.
//
// String() 을 맵 키로 쓰면 안 된다 — 그쪽은 PromQL 표기를 그대로 내는 사람용
// 표현이라 이름을 이스케이프하지 않고, 이름에 `="` 나 `, ` 가 들어가면 다른
// 라벨셋과 충돌한다(예: {a="b", c="d"} 와 이름이 `a="b", c` 인 단일 라벨).
func (ls Labels) MapKey() string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(strconv.Itoa(len(l.Name)))
		b.WriteByte(':')
		b.WriteString(l.Name)
		b.WriteString(strconv.Itoa(len(l.Value)))
		b.WriteByte(':')
		b.WriteString(l.Value)
	}
	return b.String()
}

func (ls Labels) Copy() Labels {
	out := make(Labels, len(ls))
	copy(out, ls)
	return out
}
