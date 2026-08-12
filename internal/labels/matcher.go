package labels

import (
	"fmt"
	"regexp"
	"strconv"
)

type MatchType int

const (
	MatchEqual MatchType = iota
	MatchNotEqual
	MatchRegexp
	MatchNotRegexp
)

func (t MatchType) String() string {
	switch t {
	case MatchEqual:
		return "="
	case MatchNotEqual:
		return "!="
	case MatchRegexp:
		return "=~"
	case MatchNotRegexp:
		return "!~"
	}
	return "?"
}

// Matcher 는 라벨 하나에 대한 조건이다.
type Matcher struct {
	Type  MatchType
	Name  string
	Value string

	re *regexp.Regexp
}

// NewMatcher 는 매처를 만든다. 정규식은 PromQL 관례대로 **완전 일치**로
// 앵커한다 — `node=~"e10"` 이 `e101` 에 걸리면 사용자가 의도하지 않은
// 시리즈가 조용히 딸려 온다.
func NewMatcher(t MatchType, name, value string) (*Matcher, error) {
	m := &Matcher{Type: t, Name: name, Value: value}
	if t == MatchRegexp || t == MatchNotRegexp {
		re, err := regexp.Compile("^(?:" + value + ")$")
		if err != nil {
			return nil, fmt.Errorf("tsdb: 정규식 컴파일 실패 %q: %w", value, err)
		}
		m.re = re
	}
	return m, nil
}

func (m *Matcher) Matches(s string) bool {
	switch m.Type {
	case MatchEqual:
		return s == m.Value
	case MatchNotEqual:
		return s != m.Value
	case MatchRegexp:
		return m.re.MatchString(s)
	case MatchNotRegexp:
		return !m.re.MatchString(s)
	}
	return false
}

func (m *Matcher) String() string {
	return m.Name + m.Type.String() + strconv.Quote(m.Value)
}
