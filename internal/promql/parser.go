package promql

import (
	"fmt"
	"strconv"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
)

// aggregators 는 집계 연산자다. 함수와 문법이 달라(by/without modifier) 따로 둔다.
var aggregators = map[string]bool{
	"sum": true, "avg": true, "min": true, "max": true, "count": true,
}

// unsupported 는 문법은 알지만 이번 범위에서 구현하지 않은 것들이다. 이름을
// 밝히며 거절하기 위해 목록을 유지한다 — "모르는 식별자" 로 뭉뚱그리면
// 사용자는 오타인지 미지원인지 구분할 수 없다.
var unsupported = map[string]bool{
	"topk": true, "bottomk": true,
	"histogram_quantile": true,
	"label_replace":      true, "label_join": true,
	"predict_linear": true, "deriv": true,
	"holt_winters": true, "timestamp": true,
	"quantile": true, "stddev": true, "stdvar": true,
	"count_values": true, "group": true,
}

type parser struct {
	toks []token
	pos  int
}

// Parse 는 PromQL 식을 AST 로 바꾼다.
func Parse(input string) (Node, error) {
	toks, err := lex(input)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseBinary(0)
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("promql: %q 뒤에 해석되지 않은 입력이 남았다", p.cur().val)
	}
	return n, nil
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) expect(k tokenKind, what string) (token, error) {
	if p.cur().kind != k {
		return token{}, fmt.Errorf("promql: %s 자리에 %q 가 왔다", what, p.cur().val)
	}
	return p.next(), nil
}

// precedence 는 이항 연산자 우선순위다. 큰 값이 먼저 묶인다.
func precedence(op string) int {
	switch op {
	case "*", "/", "%":
		return 4
	case "+", "-":
		return 3
	case "==", "!=", ">", "<", ">=", "<=":
		return 2
	case "&&", "||":
		return 1
	}
	return 0
}

// parseBinary 는 우선순위 등반(precedence climbing)으로 이항식을 만든다.
func (p *parser) parseBinary(minPrec int) (Node, error) {
	lhs, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.cur()
		if t.kind != tOp {
			return lhs, nil
		}
		prec := precedence(t.val)
		if prec == 0 || prec < minPrec {
			return lhs, nil
		}
		p.next()

		returnBool := false
		if p.cur().kind == tIdent && p.cur().val == "bool" {
			p.next()
			returnBool = true
		}

		rhs, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		lhs = &BinaryExpr{Op: t.val, LHS: lhs, RHS: rhs, ReturnBool: returnBool}
	}
}

func (p *parser) parseUnary() (Node, error) {
	if p.cur().kind == tOp && p.cur().val == "-" {
		p.next()
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &BinaryExpr{Op: "-", LHS: &NumberLiteral{Val: 0}, RHS: n}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	t := p.cur()
	switch t.kind {
	case tNumber:
		p.next()
		v, err := strconv.ParseFloat(t.val, 64)
		if err != nil {
			return nil, fmt.Errorf("promql: 숫자 %q 를 읽을 수 없다: %w", t.val, err)
		}
		return &NumberLiteral{Val: v}, nil

	case tLParen:
		p.next()
		n, err := p.parseBinary(0)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "닫는 괄호"); err != nil {
			return nil, err
		}
		return n, nil

	case tIdent:
		if unsupported[t.val] {
			return nil, fmt.Errorf("promql: %s 는 이번 범위에서 지원하지 않는다", t.val)
		}
		if aggregators[t.val] {
			return p.parseAggregation()
		}
		if p.toks[p.pos+1].kind == tLParen {
			return p.parseCall()
		}
		return p.parseSelectorOrMatrix()

	case tLBrace:
		return p.parseSelectorOrMatrix()
	}
	return nil, fmt.Errorf("promql: %q 로 시작하는 식을 해석할 수 없다", t.val)
}

func (p *parser) parseAggregation() (Node, error) {
	op := p.next().val

	by, grouping := false, []string(nil)
	if p.cur().kind == tIdent && (p.cur().val == "by" || p.cur().val == "without") {
		by = p.next().val == "by"
		g, err := p.parseLabelList()
		if err != nil {
			return nil, err
		}
		grouping = g
	}

	if _, err := p.expect(tLParen, "집계 인자의 여는 괄호"); err != nil {
		return nil, err
	}
	expr, err := p.parseBinary(0)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tRParen, "집계 인자의 닫는 괄호"); err != nil {
		return nil, err
	}

	// `sum(expr) by (node)` 처럼 뒤에 오는 형태도 받는다.
	if grouping == nil && p.cur().kind == tIdent && (p.cur().val == "by" || p.cur().val == "without") {
		by = p.next().val == "by"
		g, err := p.parseLabelList()
		if err != nil {
			return nil, err
		}
		grouping = g
	}

	return &Aggregation{Op: op, Expr: expr, By: by, Grouping: grouping}, nil
}

func (p *parser) parseLabelList() ([]string, error) {
	if _, err := p.expect(tLParen, "라벨 목록의 여는 괄호"); err != nil {
		return nil, err
	}
	var names []string
	for p.cur().kind != tRParen {
		id, err := p.expect(tIdent, "라벨 이름")
		if err != nil {
			return nil, err
		}
		names = append(names, id.val)
		if p.cur().kind == tComma {
			p.next()
		}
	}
	p.next() // )
	return names, nil
}

func (p *parser) parseCall() (Node, error) {
	name := p.next().val
	p.next() // (

	var args []Node
	for p.cur().kind != tRParen {
		a, err := p.parseBinary(0)
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.cur().kind == tComma {
			p.next()
		}
	}
	p.next() // )
	return &Call{Func: name, Args: args}, nil
}

func (p *parser) parseSelectorOrMatrix() (Node, error) {
	var name string
	if p.cur().kind == tIdent {
		name = p.next().val
	}

	var matchers []*labels.Matcher
	if name != "" {
		m, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, name)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
	}

	if p.cur().kind == tLBrace {
		p.next()
		for p.cur().kind != tRBrace {
			lm, err := p.parseMatcher()
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, lm)
			if p.cur().kind == tComma {
				p.next()
			}
		}
		p.next() // }
	}

	if len(matchers) == 0 {
		return nil, fmt.Errorf("promql: 빈 셀렉터는 허용하지 않는다")
	}
	vs := &VectorSelector{Name: name, Matchers: matchers}

	if p.cur().kind == tLBracket {
		p.next()
		d, err := p.expect(tDuration, "range 구간")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRBracket, "range 의 닫는 대괄호"); err != nil {
			return nil, err
		}
		dur, err := parseDuration(d.val)
		if err != nil {
			return nil, err
		}
		return &MatrixSelector{VS: vs, Range: dur}, nil
	}

	if p.cur().kind == tIdent && p.cur().val == "offset" {
		return nil, fmt.Errorf("promql: offset 은 이번 범위에서 지원하지 않는다")
	}
	return vs, nil
}

func (p *parser) parseMatcher() (*labels.Matcher, error) {
	name, err := p.expect(tIdent, "라벨 이름")
	if err != nil {
		return nil, err
	}
	op, err := p.expect(tOp, "매처 연산자")
	if err != nil {
		return nil, err
	}
	val, err := p.expect(tString, "매처 값")
	if err != nil {
		return nil, err
	}

	var mt labels.MatchType
	switch op.val {
	case "=":
		mt = labels.MatchEqual
	case "!=":
		mt = labels.MatchNotEqual
	case "=~":
		mt = labels.MatchRegexp
	case "!~":
		mt = labels.MatchNotRegexp
	default:
		return nil, fmt.Errorf("promql: %q 는 매처 연산자가 아니다", op.val)
	}
	return labels.NewMatcher(mt, name.val, val.val)
}

// parseDuration 은 5m·30s·2h 같은 PromQL duration 을 읽는다. Go 의
// time.ParseDuration 은 d·w·y 를 모르므로 직접 처리한다.
func parseDuration(s string) (time.Duration, error) {
	units := map[string]time.Duration{
		"ms": time.Millisecond,
		"s":  time.Second,
		"m":  time.Minute,
		"h":  time.Hour,
		"d":  24 * time.Hour,
		"w":  7 * 24 * time.Hour,
		"y":  365 * 24 * time.Hour,
	}
	for _, u := range []string{"ms", "s", "m", "h", "d", "w", "y"} {
		if len(s) > len(u) && s[len(s)-len(u):] == u {
			n, err := strconv.ParseFloat(s[:len(s)-len(u)], 64)
			if err != nil {
				continue
			}
			return time.Duration(n * float64(units[u])), nil
		}
	}
	return 0, fmt.Errorf("promql: %q 는 duration 형식이 아니다", s)
}
