package labels

import "testing"

func TestMatcher_네_종류가_모두_동작한다(t *testing.T) {
	cases := []struct {
		mt    MatchType
		value string
		input string
		want  bool
	}{
		{MatchEqual, "e101", "e101", true},
		{MatchEqual, "e101", "e102", false},
		{MatchNotEqual, "e101", "e102", true},
		{MatchNotEqual, "e101", "e101", false},
		{MatchRegexp, "e10.", "e101", true},
		{MatchRegexp, "e10.", "e201", false},
		{MatchNotRegexp, "e10.", "e201", true},
		{MatchNotRegexp, "e10.", "e101", false},
		// 정규식은 완전 일치로 앵커된다 — 부분 일치를 허용하지 않는다.
		{MatchRegexp, "e10", "e101", false},
		{MatchRegexp, "e10.*", "e101", true},
	}
	for _, c := range cases {
		m, err := NewMatcher(c.mt, "node", c.value)
		if err != nil {
			t.Fatalf("NewMatcher(%v,%q): %v", c.mt, c.value, err)
		}
		if got := m.Matches(c.input); got != c.want {
			t.Fatalf("%s.Matches(%q): got %v, want %v", m, c.input, got, c.want)
		}
	}
}

func TestMatcher_잘못된_정규식은_에러(t *testing.T) {
	if _, err := NewMatcher(MatchRegexp, "node", "e10("); err == nil {
		t.Fatal("컴파일 불가한 정규식은 에러여야 한다")
	}
}

func TestMatcher_String(t *testing.T) {
	m, _ := NewMatcher(MatchNotRegexp, "tier", "gpu|smart")
	if got, want := m.String(), `tier!~"gpu|smart"`; got != want {
		t.Fatalf("String: got %q, want %q", got, want)
	}
}
