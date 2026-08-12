package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 룰 파일은 Prometheus 형식(YAML)이다. Go 표준 라이브러리에 YAML 파서가 없고
// 이 제품은 외부 의존성을 두지 않으므로 직접 읽는다.
//
// 일반 YAML 파서를 만들지는 않는다 — 스키마가 고정돼 있으므로 그것만 아는
// 리더가 훨씬 작고, 지원하지 않는 문법을 조용히 삼킬 위험도 없다. 앵커·
// 멀티라인 블록·중첩 시퀀스는 룰 파일에 쓰이지 않으므로 지원하지 않고,
// 만나면 에러를 낸다.

// Group 은 같은 주기로 평가되는 룰 묶음이다.
type Group struct {
	Name     string
	Interval time.Duration
	Rules    []*Rule
}

// LoadDir 은 디렉터리의 *.yaml / *.yml 을 모두 읽어 그룹 목록을 만든다.
// 파일 하나라도 잘못되면 전체가 실패한다 — 절반만 로드되면 어떤 알림이 살아
// 있는지 아무도 모른다.
func LoadDir(dir string) ([]*Group, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("rules: 룰 디렉터리 %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".yaml" || ext == ".yml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 로드 순서를 파일시스템에 맡기지 않는다

	var out []*Group
	for _, n := range names {
		body, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, fmt.Errorf("rules: %s: %w", n, err)
		}
		gs, err := parseRuleFile(string(body))
		if err != nil {
			return nil, fmt.Errorf("rules: %s: %w", n, err)
		}
		out = append(out, gs...)
	}
	return out, nil
}

// rawRule 은 파싱 중간 형태다. 식 파싱은 모든 필드를 모은 뒤에 한다 — for 나
// labels 가 뒤에 와도 되게.
type rawRule struct {
	alert  string
	record string
	expr   string
	forStr string
	labels map[string]string
	anns   map[string]string
	line   int
}

func parseRuleFile(src string) ([]*Group, error) {
	var groups []*Group
	var cur *Group
	var rr *rawRule
	// mapTarget 은 지금 labels/annotations 블록 안인지, 그 블록의 들여쓰기가
	// 얼마인지를 기억한다.
	var mapTarget map[string]string
	mapIndent := -1

	flush := func() error {
		if rr == nil {
			return nil
		}
		r, err := buildRule(rr)
		if err != nil {
			return err
		}
		cur.Rules = append(cur.Rules, r)
		rr = nil
		return nil
	}

	for i, raw := range strings.Split(src, "\n") {
		lineNo := i + 1
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		body := strings.TrimSpace(line)

		// labels/annotations 블록 안이면, 더 깊은 들여쓰기는 그 맵의 항목이다.
		if mapTarget != nil {
			if indent > mapIndent {
				k, v, ok := splitKV(body)
				if !ok {
					return nil, fmt.Errorf("%d 행: 맵 항목이 아니다: %q", lineNo, body)
				}
				mapTarget[k] = v
				continue
			}
			mapTarget, mapIndent = nil, -1
		}

		switch {
		case body == "groups:":
			continue

		case strings.HasPrefix(body, "- "):
			item := strings.TrimSpace(body[2:])
			k, v, ok := splitKV(item)
			if !ok {
				return nil, fmt.Errorf("%d 행: 목록 항목에 키가 없다: %q", lineNo, item)
			}
			switch k {
			case "name": // 새 그룹
				if err := flush(); err != nil {
					return nil, err
				}
				cur = &Group{Name: v}
				groups = append(groups, cur)
			case "alert", "record": // 새 룰
				if cur == nil {
					return nil, fmt.Errorf("%d 행: 그룹 밖에 룰이 있다", lineNo)
				}
				if err := flush(); err != nil {
					return nil, err
				}
				rr = &rawRule{labels: map[string]string{}, anns: map[string]string{}, line: lineNo}
				if k == "alert" {
					rr.alert = v
				} else {
					rr.record = v
				}
			default:
				return nil, fmt.Errorf("%d 행: 목록 항목이 name/alert/record 로 시작하지 않는다: %q", lineNo, k)
			}
			continue
		}

		k, v, ok := splitKV(body)
		if !ok {
			return nil, fmt.Errorf("%d 행: 해석할 수 없다: %q", lineNo, body)
		}

		if rr != nil {
			switch k {
			case "expr":
				rr.expr = v
			case "for":
				rr.forStr = v
			case "labels":
				mapTarget, mapIndent = rr.labels, indent
			case "annotations":
				mapTarget, mapIndent = rr.anns, indent
			case "alert":
				rr.alert = v
			case "record":
				rr.record = v
			default:
				return nil, fmt.Errorf("%d 행: 룰에 없는 필드 %q", lineNo, k)
			}
			continue
		}

		if cur != nil {
			switch k {
			case "interval":
				d, err := parseDuration(v)
				if err != nil {
					return nil, fmt.Errorf("%d 행: interval: %w", lineNo, err)
				}
				cur.Interval = d
			case "rules":
				// 다음 목록 항목이 룰이다.
			case "name":
				cur.Name = v
			default:
				return nil, fmt.Errorf("%d 행: 그룹에 없는 필드 %q", lineNo, k)
			}
			continue
		}
		return nil, fmt.Errorf("%d 행: groups 밖의 내용: %q", lineNo, body)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return groups, nil
}

func buildRule(rr *rawRule) (*Rule, error) {
	if rr.record != "" {
		return nil, fmt.Errorf("%d 행: record 룰은 지원하지 않는다(알림 룰만 받는다)", rr.line)
	}
	if rr.alert == "" {
		return nil, fmt.Errorf("%d 행: alert 이름이 없다", rr.line)
	}
	if rr.expr == "" {
		return nil, fmt.Errorf("%d 행: 룰 %q 에 expr 이 없다", rr.line, rr.alert)
	}
	var forDur time.Duration
	if rr.forStr != "" {
		d, err := parseDuration(rr.forStr)
		if err != nil {
			return nil, fmt.Errorf("%d 행: 룰 %q 의 for: %w", rr.line, rr.alert, err)
		}
		forDur = d
	}
	return NewRule(rr.alert, rr.expr, forDur, rr.labels, rr.anns)
}

// splitKV 는 첫 콜론으로 키와 값을 나눈다. 값에 콜론이 더 있어도 그대로 둔다
// — PromQL 식에는 콜론이 흔하다.
func splitKV(s string) (string, string, bool) {
	i := strings.Index(s, ":")
	if i < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(s[:i])
	v := strings.TrimSpace(s[i+1:])
	if k == "" {
		return "", "", false
	}
	return k, unquote(v), true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripComment 는 따옴표 **밖**의 # 부터를 지운다. 안쪽까지 지우면
// summary: "번호 #42" 가 잘린다.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return strings.TrimRight(line[:i], " ")
		}
	}
	return strings.TrimRight(line, " ")
}

// parseDuration 은 30s·5m·2h 같은 Prometheus duration 을 읽는다. Go 의
// time.ParseDuration 은 d·w·y 를 모른다.
func parseDuration(s string) (time.Duration, error) {
	units := []struct {
		suffix string
		d      time.Duration
	}{
		{"ms", time.Millisecond}, {"s", time.Second}, {"m", time.Minute},
		{"h", time.Hour}, {"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour},
		{"y", 365 * 24 * time.Hour},
	}
	for _, u := range units {
		if len(s) > len(u.suffix) && strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				continue
			}
			return time.Duration(n * float64(u.d)), nil
		}
	}
	return 0, fmt.Errorf("%q 는 duration 형식이 아니다(예: 30s, 5m)", s)
}
