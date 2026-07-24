package tsdb

import (
	"reflect"
	"testing"
)

func TestMemPostings_라벨로_시리즈를_찾는다(t *testing.T) {
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}, Label{"tier", "core"}))
	p.Add(2, NewLabels(Label{"node", "e102"}, Label{"tier", "core"}))
	p.Add(3, NewLabels(Label{"node", "e101"}, Label{"tier", "gpu"}))

	if got := p.Get("node", "e101"); !reflect.DeepEqual(got, []uint64{1, 3}) {
		t.Fatalf("node=e101: got %v, want [1 3]", got)
	}
	if got := p.Get("tier", "core"); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("tier=core: got %v, want [1 2]", got)
	}
	if got := p.Get("node", "없음"); len(got) != 0 {
		t.Fatalf("없는 값은 빈 결과여야 한다: %v", got)
	}
	if got := p.All(); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("All: got %v, want [1 2 3]", got)
	}
}

func TestMemPostings_라벨_이름과_값을_열거한다(t *testing.T) {
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}, Label{"tier", "core"}))
	p.Add(2, NewLabels(Label{"node", "e102"}))

	if got := p.LabelNames(); !reflect.DeepEqual(got, []string{"node", "tier"}) {
		t.Fatalf("LabelNames: got %v", got)
	}
	if got := p.LabelValues("node"); !reflect.DeepEqual(got, []string{"e101", "e102"}) {
		t.Fatalf("LabelValues(node): got %v", got)
	}
}

func TestPostings_집합연산(t *testing.T) {
	a := []uint64{1, 3, 5, 7, 9}
	b := []uint64{3, 4, 5, 6, 9, 10}

	if got := intersect(a, b); !reflect.DeepEqual(got, []uint64{3, 5, 9}) {
		t.Fatalf("intersect: got %v", got)
	}
	if got := union(a, b); !reflect.DeepEqual(got, []uint64{1, 3, 4, 5, 6, 7, 9, 10}) {
		t.Fatalf("union: got %v", got)
	}
	if got := without(a, b); !reflect.DeepEqual(got, []uint64{1, 7}) {
		t.Fatalf("without: got %v", got)
	}
	if got := intersect(a, nil); len(got) != 0 {
		t.Fatalf("빈 집합과의 교집합은 비어야 한다: %v", got)
	}
}

func TestSelectRefs_색인으로_후보를_좁힌다(t *testing.T) {
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}, Label{"tier", "core"}))
	p.Add(2, NewLabels(Label{"node", "e102"}, Label{"tier", "core"}))
	p.Add(3, NewLabels(Label{"node", "e101"}, Label{"tier", "gpu"}))

	eq, _ := NewMatcher(MatchEqual, "node", "e101")
	if got := selectRefs(p, []*Matcher{eq}); !reflect.DeepEqual(got, []uint64{1, 3}) {
		t.Fatalf("node=e101: got %v, want [1 3]", got)
	}

	re, _ := NewMatcher(MatchRegexp, "tier", "core|gpu")
	if got := selectRefs(p, []*Matcher{re}); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("tier=~core|gpu: got %v", got)
	}

	tierEq, _ := NewMatcher(MatchEqual, "tier", "gpu")
	if got := selectRefs(p, []*Matcher{eq, tierEq}); !reflect.DeepEqual(got, []uint64{3}) {
		t.Fatalf("교집합: got %v, want [3]", got)
	}
}

func TestSelectRefs_색인으로_좁힐_수_없으면_전체를_준다(t *testing.T) {
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}))
	p.Add(2, NewLabels(Label{"node", "e102"}, Label{"tier", "gpu"}))

	// 부정 매처는 "그 라벨이 아예 없는 시리즈"도 만족시키므로 색인으로
	// 좁힐 수 없다 — 전체에서 시작해야 한다.
	ne, _ := NewMatcher(MatchNotEqual, "tier", "gpu")
	if got := selectRefs(p, []*Matcher{ne}); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("tier!=gpu 후보: got %v, want [1 2]", got)
	}

	// 빈 값 같음-매처(tier="")도 마찬가지다. 색인에는 빈 값 posting 이
	// 없으므로 시드로 쓰면 결과가 통째로 사라진다.
	empty, _ := NewMatcher(MatchEqual, "tier", "")
	if got := selectRefs(p, []*Matcher{empty}); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf(`tier="" 후보: got %v, want [1 2]`, got)
	}

	// 매처가 없으면 전체.
	if got := selectRefs(p, nil); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("무매처: got %v", got)
	}
}

func TestMatchesAll(t *testing.T) {
	ls := NewLabels(Label{"node", "e101"}, Label{"tier", "core"})

	eq, _ := NewMatcher(MatchEqual, "node", "e101")
	ne, _ := NewMatcher(MatchNotEqual, "tier", "gpu")
	if !matchesAll(ls, []*Matcher{eq, ne}) {
		t.Fatal("둘 다 만족해야 한다")
	}

	bad, _ := NewMatcher(MatchEqual, "node", "e102")
	if matchesAll(ls, []*Matcher{eq, bad}) {
		t.Fatal("하나라도 어긋나면 false 여야 한다")
	}

	// 없는 라벨은 빈 문자열로 취급된다.
	emptyEq, _ := NewMatcher(MatchEqual, "device", "")
	if !matchesAll(ls, []*Matcher{emptyEq}) {
		t.Fatal(`없는 라벨은 device="" 를 만족해야 한다`)
	}
}

func TestMemPostings_Get_반환_슬라이스_복사본(t *testing.T) {
	// 함정: Get이 내부 슬라이스의 참조를 돌려주면
	// 호출자가 변형할 때 색인이 오염된다
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}))

	orig := p.Get("node", "e101")
	if len(orig) != 1 || orig[0] != 1 {
		t.Fatalf("원본: got %v, want [1]", orig)
	}

	// 호출자가 반환된 슬라이스를 변형
	orig[0] = 999

	// 색인은 안 바뀌었어야 한다
	second := p.Get("node", "e101")
	if len(second) != 1 || second[0] != 1 {
		t.Fatalf("Get이 복사본을 돌려줬나요? second: %v, want [1]", second)
	}
}

func TestPostings_union_한쪽이_빈_경우(t *testing.T) {
	// 함정: union이 한쪽이 nil/empty일 때 올바른가?
	a := []uint64{1, 2, 3}

	if got := union(a, nil); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("union(a, nil): got %v, want [1 2 3]", got)
	}
	if got := union(nil, a); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("union(nil, a): got %v, want [1 2 3]", got)
	}
}

func TestPostings_without_한쪽이_빈_경우(t *testing.T) {
	// 함정: without(a, nil)은 a 전부여야 한다
	a := []uint64{1, 2, 3}

	if got := without(a, nil); !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("without(a, nil): got %v, want [1 2 3]", got)
	}
	// without(nil, a)는 빈 결과
	if got := without(nil, a); len(got) != 0 {
		t.Fatalf("without(nil, a): got %v, want []", got)
	}
}

func TestSelectRefs_정규식_아무것도_안_맞기(t *testing.T) {
	// 함정: 정규식 매처가 어떤 값과도 안 맞으면 후보가 비어야 한다
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}))
	p.Add(2, NewLabels(Label{"node", "e102"}))

	// "z.*"는 둘 다 안 맞음
	re, _ := NewMatcher(MatchRegexp, "node", "z.*")
	if got := selectRefs(p, []*Matcher{re}); len(got) != 0 {
		t.Fatalf("정규식 미매치: got %v, want []", got)
	}
}

func TestSelectRefs_부정_정규식은_색인으로_좁힐_수_없음(t *testing.T) {
	// 함정: 부정 정규식도 색인으로 좁힐 수 없다 (부정 매처처럼)
	p := newMemPostings()
	p.Add(1, NewLabels(Label{"node", "e101"}))
	p.Add(2, NewLabels(Label{"node", "e102"}, Label{"tier", "gpu"}))

	// tier!~"core" — core와 안 맞는 모든 것 (라벨 부재도 포함)
	notRe, _ := NewMatcher(MatchNotRegexp, "tier", "core")
	if got := selectRefs(p, []*Matcher{notRe}); !reflect.DeepEqual(got, []uint64{1, 2}) {
		t.Fatalf("tier!~core: got %v, want [1 2]", got)
	}
}
