# PromQL 서브셋 엔진 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** observatory 가 셀렉터만 이해하던 질의 경로를, 함수·집계·연산자를 갖춘 PromQL 서브셋 엔진으로 교체한다.

**Architecture:** 렉서 → 파서 → AST → 평가기 4단 파이프라인을 `internal/promql` 에 신설한다. 데이터는 기존 `tsdb.Querier` 에서만 읽고, 평가는 전부 이 패키지가 맡는다 — 현재 `apiserver/server.go` 에 인라인으로 있는 평가 로직이 이리로 옮겨온다. 저장(tsdb)과 평가(promql)의 경계를 다시 긋는 작업이기도 하다.

**Tech Stack:** Go 1.26 표준 라이브러리만. 외부 의존성 추가 금지.

## Global Constraints

- **외부 의존성 0 을 유지한다.** `go.mod` 에 `require` 를 추가하지 않는다. 이 저장소는 22,461줄 전부를 표준 라이브러리로 작성해 왔고, 그것이 "차트 하나 = 파드 하나 = 완결" 이라는 제품 정체성의 근거다.
- **미지원 문법은 명시적 에러.** 유사 기능으로 대체하거나 조용히 무시하지 않는다. 에러 메시지는 무엇이 지원되지 않는지 이름을 밝힌다.
- **주석과 커밋 메시지는 한글**, 코드 식별자는 영문 — 저장소 기존 관례.
- 모든 커밋 전에 `make all`(fmt·vet·test) 이 통과해야 한다.
- 대상 저장소: `git.keiailab.com/keiailab-demo/gpumanager`, 작업 브랜치는 태스크별로 나누지 않고 `feat/promql-engine` 하나를 쓴다.

---

## File Structure

| 파일 | 책임 |
|---|---|
| `internal/promql/lexer.go` | 입력 문자열 → 토큰 열. 문법 지식 없음 |
| `internal/promql/ast.go` | 노드 타입 정의만. 로직 없음 |
| `internal/promql/parser.go` | 토큰 열 → AST. 우선순위 처리 |
| `internal/promql/eval.go` | AST + Querier → 결과. 평가 진입점 |
| `internal/promql/funcs.go` | 함수 구현 테이블 |
| `internal/promql/value.go` | Vector/Matrix/Scalar 결과 타입 |

`tsdb` 는 건드리지 않는다. `apiserver` 는 마지막 태스크에서만 수정한다.

---

### Task 1: 결과 타입과 렉서

**Files:**
- Create: `internal/promql/value.go`
- Create: `internal/promql/lexer.go`
- Test: `internal/promql/lexer_test.go`

**Interfaces:**
- Produces: `Sample{Labels tsdb.Labels; T int64; V float64}`, `Vector []Sample`, `Series{Labels tsdb.Labels; Points []Point}`, `Matrix []Series`, `Point{T int64; V float64}`, `Scalar float64`
- Produces: `lex(input string) ([]token, error)`, `token{kind tokenKind; val string; pos int}`
- Produces: tokenKind 상수 — `tIdent`, `tNumber`, `tString`, `tLBrace`, `tRBrace`, `tLParen`, `tRParen`, `tLBracket`, `tRBracket`, `tComma`, `tOp`, `tDuration`, `tEOF`

- [ ] **Step 1: 결과 타입을 정의한다**

`internal/promql/value.go`:

```go
// Package promql 는 PromQL 서브셋의 파싱과 평가를 담당한다. 데이터는
// tsdb.Querier 에서만 읽고, 저장 구조는 알지 못한다.
package promql

import "git.keiailab.com/keiailab-demo/gpumanager/internal/tsdb"

// Point 는 한 시점의 값이다. T 는 unix 밀리초.
type Point struct {
	T int64
	V float64
}

// Sample 은 instant vector 의 원소 — 라벨셋 하나당 시점 하나.
type Sample struct {
	Labels tsdb.Labels
	T      int64
	V      float64
}

// Vector 는 같은 시점의 샘플 집합이다(PromQL instant vector).
type Vector []Sample

// Series 는 range vector 의 원소 — 라벨셋 하나의 시간 구간.
type Series struct {
	Labels tsdb.Labels
	Points []Point
}

// Matrix 는 range vector 다.
type Matrix []Series

// Scalar 는 라벨이 없는 단일 값이다.
type Scalar float64
```

- [ ] **Step 2: 렉서 테스트를 먼저 쓴다**

`internal/promql/lexer_test.go`:

```go
package promql

import "testing"

func TestLexSelector(t *testing.T) {
	toks, err := lex(`node_load1{node="e106"}`)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	want := []struct {
		kind tokenKind
		val  string
	}{
		{tIdent, "node_load1"},
		{tLBrace, "{"},
		{tIdent, "node"},
		{tOp, "="},
		{tString, "e106"},
		{tRBrace, "}"},
		{tEOF, ""},
	}
	if len(toks) != len(want) {
		t.Fatalf("토큰 %d 개, want %d: %+v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].kind != w.kind || toks[i].val != w.val {
			t.Errorf("[%d] %v %q, want %v %q", i, toks[i].kind, toks[i].val, w.kind, w.val)
		}
	}
}

func TestLexDurationAndNumber(t *testing.T) {
	toks, err := lex(`increase(x[5m]) > 0.5`)
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	var sawDuration, sawNumber bool
	for _, tk := range toks {
		if tk.kind == tDuration && tk.val == "5m" {
			sawDuration = true
		}
		if tk.kind == tNumber && tk.val == "0.5" {
			sawNumber = true
		}
	}
	if !sawDuration {
		t.Error(`5m 이 tDuration 으로 나오지 않았다`)
	}
	if !sawNumber {
		t.Error(`0.5 가 tNumber 로 나오지 않았다`)
	}
}

// 두 글자 연산자를 한 글자씩 끊으면 =~ 가 = 와 ~ 로 갈라져 매처 종류가 바뀐다.
func TestLexTwoCharOperators(t *testing.T) {
	for _, op := range []string{"=~", "!~", "!=", "==", ">=", "<="} {
		toks, err := lex("a{b" + op + `"c"}`)
		if err != nil {
			t.Fatalf("lex %q: %v", op, err)
		}
		var found bool
		for _, tk := range toks {
			if tk.kind == tOp && tk.val == op {
				found = true
			}
		}
		if !found {
			t.Errorf("%q 가 하나의 연산자 토큰으로 나오지 않았다: %+v", op, toks)
		}
	}
}

func TestLexRejectsUnterminatedString(t *testing.T) {
	if _, err := lex(`a{b="c}`); err == nil {
		t.Error("닫히지 않은 문자열이 에러 없이 통과했다")
	}
}
```

- [ ] **Step 3: 테스트가 실패하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestLex`
Expected: 컴파일 실패 — `undefined: lex`, `undefined: tokenKind`

- [ ] **Step 4: 렉서를 구현한다**

`internal/promql/lexer.go`:

```go
package promql

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tString
	tDuration
	tLBrace
	tRBrace
	tLParen
	tRParen
	tLBracket
	tRBracket
	tComma
	tOp
)

func (k tokenKind) String() string {
	switch k {
	case tEOF:
		return "EOF"
	case tIdent:
		return "IDENT"
	case tNumber:
		return "NUMBER"
	case tString:
		return "STRING"
	case tDuration:
		return "DURATION"
	default:
		return "SYMBOL"
	}
}

type token struct {
	kind tokenKind
	val  string
	pos  int
}

// twoCharOps 는 반드시 한 토큰으로 끊어야 하는 연산자다. 한 글자씩 끊으면
// =~ 가 = 와 ~ 로 갈라져 정규식 매처가 등호 매처로 바뀐다 — 조용히 다른
// 질의가 되는 종류의 사고다.
var twoCharOps = []string{"=~", "!~", "!=", "==", ">=", "<=", "&&", "||"}

func lex(input string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(input) {
		c := input[i]

		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}

		if matched := matchTwoChar(input, i); matched != "" {
			toks = append(toks, token{tOp, matched, i})
			i += 2
			continue
		}

		switch c {
		case '{':
			toks = append(toks, token{tLBrace, "{", i})
			i++
			continue
		case '}':
			toks = append(toks, token{tRBrace, "}", i})
			i++
			continue
		case '(':
			toks = append(toks, token{tLParen, "(", i})
			i++
			continue
		case ')':
			toks = append(toks, token{tRParen, ")", i})
			i++
			continue
		case '[':
			toks = append(toks, token{tLBracket, "[", i})
			i++
			continue
		case ']':
			toks = append(toks, token{tRBracket, "]", i})
			i++
			continue
		case ',':
			toks = append(toks, token{tComma, ",", i})
			i++
			continue
		case '=', '!', '>', '<', '+', '-', '*', '/', '%', '^':
			toks = append(toks, token{tOp, string(c), i})
			i++
			continue
		case '"', '\'':
			s, n, err := lexString(input, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tString, s, i})
			i += n
			continue
		}

		if isDigit(c) {
			tok, n := lexNumberOrDuration(input, i)
			toks = append(toks, tok)
			i += n
			continue
		}

		if isIdentStart(c) {
			j := i
			for j < len(input) && isIdentChar(input[j]) {
				j++
			}
			toks = append(toks, token{tIdent, input[i:j], i})
			i = j
			continue
		}

		return nil, fmt.Errorf("promql: %d 번째 문자 %q 를 해석할 수 없다", i, string(c))
	}
	toks = append(toks, token{tEOF, "", len(input)})
	return toks, nil
}

func matchTwoChar(input string, i int) string {
	if i+1 >= len(input) {
		return ""
	}
	pair := input[i : i+2]
	for _, op := range twoCharOps {
		if pair == op {
			return op
		}
	}
	return ""
}

// lexString 은 따옴표 안의 내용을 돌려주고 소비한 길이(따옴표 포함)를 낸다.
func lexString(input string, i int) (string, int, error) {
	quote := input[i]
	var sb strings.Builder
	j := i + 1
	for j < len(input) {
		if input[j] == '\\' && j+1 < len(input) {
			sb.WriteByte(input[j+1])
			j += 2
			continue
		}
		if input[j] == quote {
			return sb.String(), j - i + 1, nil
		}
		sb.WriteByte(input[j])
		j++
	}
	return "", 0, fmt.Errorf("promql: %d 번째에서 시작한 문자열이 닫히지 않았다", i)
}

// durationUnits 는 뒤에 붙으면 그 리터럴을 duration 으로 만드는 접미사다.
const durationUnits = "smhdwy"

// lexNumberOrDuration 은 숫자를 읽고, 바로 뒤에 시간 단위가 붙으면 duration 으로
// 분류한다. 5 와 5m 은 문법적으로 다른 자리에 쓰이므로 여기서 갈라야 한다.
func lexNumberOrDuration(input string, i int) (token, int) {
	j := i
	for j < len(input) && (isDigit(input[j]) || input[j] == '.') {
		j++
	}
	if j < len(input) && strings.IndexByte(durationUnits, input[j]) >= 0 {
		unitEnd := j + 1
		// ms 는 두 글자다.
		if input[j] == 'm' && unitEnd < len(input) && input[unitEnd] == 's' {
			unitEnd++
		}
		return token{tDuration, input[i:unitEnd], i}, unitEnd - i
	}
	return token{tNumber, input[i:j], i}, j - i
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || c == ':' || unicode.IsLetter(rune(c))
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
```

- [ ] **Step 5: 테스트가 통과하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestLex -v`
Expected: 4개 테스트 모두 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/promql/value.go internal/promql/lexer.go internal/promql/lexer_test.go
git commit -m "feat(promql): 결과 타입과 렉서

두 글자 연산자를 한 토큰으로 끊는다 — =~ 를 = 와 ~ 로 갈라 읽으면 정규식
매처가 등호 매처로 바뀌어, 에러 없이 다른 질의가 된다. 숫자와 duration 도
렉서에서 가른다(5 와 5m 은 문법적으로 다른 자리에 온다)."
```

---

### Task 2: AST 와 파서

**Files:**
- Create: `internal/promql/ast.go`
- Create: `internal/promql/parser.go`
- Test: `internal/promql/parser_test.go`

**Interfaces:**
- Consumes: Task 1 의 `lex`, `token`, tokenKind 상수
- Produces: `Node` 인터페이스와 구현 — `VectorSelector{Name string; Matchers []*tsdb.Matcher}`, `MatrixSelector{VS *VectorSelector; Range time.Duration}`, `Call{Func string; Args []Node}`, `Aggregation{Op string; Expr Node; By bool; Grouping []string}`, `BinaryExpr{Op string; LHS, RHS Node; ReturnBool bool}`, `NumberLiteral{Val float64}`
- Produces: `Parse(input string) (Node, error)`

- [ ] **Step 1: AST 노드를 정의한다**

`internal/promql/ast.go`:

```go
package promql

import (
	"time"

	"git.keiailab.com/keiailab-demo/gpumanager/internal/tsdb"
)

// Node 는 파싱된 식의 한 조각이다. 평가기는 이 트리를 따라 내려간다.
type Node interface{ node() }

// VectorSelector 는 `name{a="b"}` — 한 시점의 시리즈 집합을 고른다.
type VectorSelector struct {
	Name     string
	Matchers []*tsdb.Matcher
}

// MatrixSelector 는 `name{...}[5m]` — 구간의 시리즈를 고른다. range 함수의
// 인자로만 올 수 있다.
type MatrixSelector struct {
	VS    *VectorSelector
	Range time.Duration
}

// Call 은 `f(...)` 형태의 함수 호출이다.
type Call struct {
	Func string
	Args []Node
}

// Aggregation 은 `sum by (node) (expr)` 같은 집계다. By 가 false 이고
// Grouping 이 비어 있지 않으면 without 이다.
type Aggregation struct {
	Op       string
	Expr     Node
	By       bool
	Grouping []string
}

// BinaryExpr 는 이항 연산이다. ReturnBool 은 `> bool 0` 처럼 비교 결과를
// 0/1 로 바꾸라는 modifier.
type BinaryExpr struct {
	Op         string
	LHS, RHS   Node
	ReturnBool bool
}

// NumberLiteral 은 스칼라 상수다.
type NumberLiteral struct{ Val float64 }

func (*VectorSelector) node() {}
func (*MatrixSelector) node() {}
func (*Call) node()           {}
func (*Aggregation) node()    {}
func (*BinaryExpr) node()     {}
func (*NumberLiteral) node()  {}
```

- [ ] **Step 2: 파서 테스트를 먼저 쓴다**

`internal/promql/parser_test.go`:

```go
package promql

import (
	"strings"
	"testing"
	"time"
)

func TestParseVectorSelector(t *testing.T) {
	n, err := Parse(`node_load1{node="e106",tier=~"co.*"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	vs, ok := n.(*VectorSelector)
	if !ok {
		t.Fatalf("타입 %T, want *VectorSelector", n)
	}
	if vs.Name != "node_load1" {
		t.Errorf("Name=%q", vs.Name)
	}
	// __name__ 매처 1 + 라벨 매처 2
	if len(vs.Matchers) != 3 {
		t.Errorf("매처 %d 개, want 3: %+v", len(vs.Matchers), vs.Matchers)
	}
}

func TestParseRangeInsideCall(t *testing.T) {
	n, err := Parse(`increase(nodevitals_events_total[5m])`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, ok := n.(*Call)
	if !ok {
		t.Fatalf("타입 %T, want *Call", n)
	}
	if c.Func != "increase" {
		t.Errorf("Func=%q", c.Func)
	}
	ms, ok := c.Args[0].(*MatrixSelector)
	if !ok {
		t.Fatalf("인자 타입 %T, want *MatrixSelector", c.Args[0])
	}
	if ms.Range != 5*time.Minute {
		t.Errorf("Range=%v, want 5m", ms.Range)
	}
}

func TestParseAggregationByAndWithout(t *testing.T) {
	n, err := Parse(`sum by (node) (up)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a, ok := n.(*Aggregation)
	if !ok {
		t.Fatalf("타입 %T, want *Aggregation", n)
	}
	if a.Op != "sum" || !a.By || len(a.Grouping) != 1 || a.Grouping[0] != "node" {
		t.Errorf("집계 파싱 결과가 다르다: %+v", a)
	}

	n2, err := Parse(`count without (device) (up)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a2 := n2.(*Aggregation)
	if a2.By {
		t.Error("without 인데 By 가 true 다")
	}
}

// 곱하기가 더하기보다 먼저 묶여야 한다. 우선순위를 무시하면 값이 조용히 달라진다.
func TestParseBinaryPrecedence(t *testing.T) {
	n, err := Parse(`1 + 2 * 3`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	be, ok := n.(*BinaryExpr)
	if !ok {
		t.Fatalf("타입 %T, want *BinaryExpr", n)
	}
	if be.Op != "+" {
		t.Fatalf("최상위 연산자 %q, want +", be.Op)
	}
	rhs, ok := be.RHS.(*BinaryExpr)
	if !ok || rhs.Op != "*" {
		t.Errorf("오른쪽이 곱셈 노드가 아니다: %+v", be.RHS)
	}
}

func TestParseComparisonWithBool(t *testing.T) {
	n, err := Parse(`up > bool 0`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	be := n.(*BinaryExpr)
	if !be.ReturnBool {
		t.Error("bool modifier 가 반영되지 않았다")
	}
}

// 지원하지 않는 문법은 이름을 밝히며 거절해야 한다 — 조용히 무시하면
// 사용자는 다른 답을 받고도 알아채지 못한다.
func TestParseRejectsUnsupported(t *testing.T) {
	cases := map[string]string{
		`up offset 5m`:            "offset",
		`topk(3, up)`:             "topk",
		`histogram_quantile(0.9, x)`: "histogram_quantile",
	}
	for expr, want := range cases {
		_, err := Parse(expr)
		if err == nil {
			t.Errorf("%q 가 에러 없이 통과했다", expr)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q 의 에러가 %q 를 언급하지 않는다: %v", expr, want, err)
		}
	}
}
```

- [ ] **Step 3: 테스트가 실패하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestParse`
Expected: 컴파일 실패 — `undefined: Parse`

- [ ] **Step 4: 파서를 구현한다**

`internal/promql/parser.go`:

```go
package promql

import (
	"fmt"
	"strconv"
	"time"

	"git.keiailab.com/keiailab-demo/gpumanager/internal/tsdb"
)

// aggregators 는 집계 연산자다. 함수와 문법이 달라(by/without modifier) 따로 둔다.
var aggregators = map[string]bool{
	"sum": true, "avg": true, "min": true, "max": true, "count": true,
}

// unsupported 는 문법은 알지만 이번 범위에서 구현하지 않은 것들이다. 이름을
// 밝히며 거절하기 위해 목록을 유지한다 — "모르는 식별자" 로 뭉뚱그리면
// 사용자는 오타인지 미지원인지 구분할 수 없다.
var unsupported = map[string]string{
	"topk": "topk", "bottomk": "bottomk",
	"histogram_quantile": "histogram_quantile",
	"label_replace":      "label_replace", "label_join": "label_join",
	"predict_linear": "predict_linear", "deriv": "deriv",
	"holt_winters": "holt_winters", "timestamp": "timestamp",
	"quantile": "quantile", "stddev": "stddev", "stdvar": "stdvar",
	"count_values": "count_values", "group": "group",
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
		if name, bad := unsupported[t.val]; bad != "" || name != "" {
			return nil, fmt.Errorf("promql: %s 는 이번 범위에서 지원하지 않는다", t.val)
		}
		if aggregators[t.val] {
			return p.parseAggregation()
		}
		// 뒤에 여는 괄호가 오면 함수 호출이다.
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

	var matchers []*tsdb.Matcher
	if name != "" {
		m, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, name)
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

func (p *parser) parseMatcher() (*tsdb.Matcher, error) {
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

	var mt tsdb.MatchType
	switch op.val {
	case "=":
		mt = tsdb.MatchEqual
	case "!=":
		mt = tsdb.MatchNotEqual
	case "=~":
		mt = tsdb.MatchRegexp
	case "!~":
		mt = tsdb.MatchNotRegexp
	default:
		return nil, fmt.Errorf("promql: %q 는 매처 연산자가 아니다", op.val)
	}
	return tsdb.NewMatcher(mt, name.val, val.val)
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
```

- [ ] **Step 5: 테스트가 통과하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestParse -v`
Expected: 6개 테스트 모두 PASS

이 이름들은 실제 `internal/tsdb` 와 대조해 확인했다 — `MatchEqual`·`MatchNotEqual`·
`MatchRegexp`·`MatchNotRegexp`, `NewMatcher(t, name, value)`, `MetricName = "__name__"` 모두 존재한다.

- [ ] **Step 6: 커밋**

```bash
git add internal/promql/ast.go internal/promql/parser.go internal/promql/parser_test.go
git commit -m "feat(promql): AST 와 파서

우선순위 등반으로 이항식을 묶는다 — 1 + 2 * 3 에서 곱셈이 먼저 묶이지 않으면
값이 조용히 달라진다. 미지원 문법(offset·topk·histogram_quantile 등)은 이름을
밝히며 거절한다: '모르는 식별자' 로 뭉뚱그리면 오타인지 미지원인지 구분할 수 없다."
```

---

### Task 3: instant vector 평가와 집계

**Files:**
- Create: `internal/promql/eval.go`
- Test: `internal/promql/eval_test.go`

**Interfaces:**
- Consumes: Task 1 의 `Vector`/`Matrix`/`Sample`, Task 2 의 `Node` 구현들
- Produces: `Engine{}`, `NewEngine() *Engine`, `(*Engine).Eval(q Queryable, expr Node, evalMS int64) (any, error)` — 반환은 `Vector`·`Matrix`·`Scalar` 중 하나
- Produces: `Queryable` 인터페이스 — `Querier(mint, maxt int64) (*tsdb.Querier, func() error, error)`. `*tsdb.DB` 가 이미 이 시그니처를 만족한다

- [ ] **Step 1: 평가 테스트를 먼저 쓴다**

`internal/promql/eval_test.go`:

```go
package promql

import (
	"testing"

	"git.keiailab.com/keiailab-demo/gpumanager/internal/tsdb"
)

// newTestDB 는 샘플을 넣은 임시 DB 를 만든다. tsdb 의 실제 생성자 이름이
// 다르면 internal/tsdb/db.go 를 보고 맞춘다.
func newTestDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func appendSample(t *testing.T, db *tsdb.DB, name string, lbls map[string]string, ts int64, v float64) {
	t.Helper()
	m := map[string]string{tsdb.MetricName: name}
	for k, val := range lbls {
		m[k] = val
	}
	if err := db.Append(tsdb.LabelsFromMap(m), ts, v); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func TestEvalVectorSelectorTakesLatestSample(t *testing.T) {
	db := newTestDB(t)
	appendSample(t, db, "up", map[string]string{"node": "e106"}, 1000, 1)
	appendSample(t, db, "up", map[string]string{"node": "e106"}, 2000, 0)

	expr, err := Parse(`up{node="e106"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := NewEngine().Eval(db, expr, 2000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec, ok := got.(Vector)
	if !ok {
		t.Fatalf("타입 %T, want Vector", got)
	}
	if len(vec) != 1 || vec[0].V != 0 {
		t.Errorf("결과 %+v, want 값 0 하나 (평가 시점의 최신 샘플)", vec)
	}
}

func TestEvalAggregationSumBy(t *testing.T) {
	db := newTestDB(t)
	appendSample(t, db, "x", map[string]string{"node": "a", "dev": "1"}, 1000, 2)
	appendSample(t, db, "x", map[string]string{"node": "a", "dev": "2"}, 1000, 3)
	appendSample(t, db, "x", map[string]string{"node": "b", "dev": "1"}, 1000, 10)

	expr, _ := Parse(`sum by (node) (x)`)
	got, err := NewEngine().Eval(db, expr, 1000)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	vec := got.(Vector)
	if len(vec) != 2 {
		t.Fatalf("그룹 %d 개, want 2: %+v", len(vec), vec)
	}
	byNode := map[string]float64{}
	for _, s := range vec {
		byNode[s.Labels.Get("node")] = s.V
	}
	if byNode["a"] != 5 || byNode["b"] != 10 {
		t.Errorf("집계 결과 %+v, want a=5 b=10", byNode)
	}
	// by 로 묶으면 그룹 라벨만 남아야 한다 — dev 가 남으면 그룹이 쪼개진다.
	for _, s := range vec {
		if s.Labels.Get("dev") != "" {
			t.Errorf("by (node) 결과에 dev 라벨이 남았다: %+v", s.Labels)
		}
	}
}

func TestEvalComparisonFiltersAndBoolConverts(t *testing.T) {
	db := newTestDB(t)
	appendSample(t, db, "x", map[string]string{"n": "a"}, 1000, 5)
	appendSample(t, db, "x", map[string]string{"n": "b"}, 1000, 1)

	// 비교는 기본적으로 필터다.
	expr, _ := Parse(`x > 3`)
	got, _ := NewEngine().Eval(db, expr, 1000)
	if v := got.(Vector); len(v) != 1 || v[0].Labels.Get("n") != "a" {
		t.Errorf("필터 결과 %+v, want n=a 하나", got)
	}

	// bool modifier 가 붙으면 필터가 아니라 0/1 변환이다.
	expr2, _ := Parse(`x > bool 3`)
	got2, _ := NewEngine().Eval(db, expr2, 1000)
	v2 := got2.(Vector)
	if len(v2) != 2 {
		t.Fatalf("bool 결과 %d 개, want 2 (필터가 아니라 변환)", len(v2))
	}
	vals := map[string]float64{}
	for _, s := range v2 {
		vals[s.Labels.Get("n")] = s.V
	}
	if vals["a"] != 1 || vals["b"] != 0 {
		t.Errorf("bool 변환 결과 %+v, want a=1 b=0", vals)
	}
}
```

- [ ] **Step 2: 테스트가 실패하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestEval`
Expected: 컴파일 실패 — `undefined: NewEngine`

- [ ] **Step 3: 평가기를 구현한다**

`internal/promql/eval.go`:

```go
package promql

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"git.keiailab.com/keiailab-demo/gpumanager/internal/tsdb"
)

// lookbackDelta 는 instant 평가에서 거슬러 올라가 최신 샘플을 찾는 창이다.
// Prometheus 기본값과 같은 5분 — 스크레이프 한두 번 실패해도 시리즈가
// 사라지지 않게 하는 값이다.
const lookbackDelta = 5 * time.Minute

// Queryable 은 평가기가 데이터를 얻는 통로다. *tsdb.DB 가 이 시그니처를 만족한다.
type Queryable interface {
	Querier(mint, maxt int64) (*tsdb.Querier, func() error, error)
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

// Eval 은 evalMS 시점 기준으로 식을 평가한다. 반환은 Vector·Matrix·Scalar 중 하나다.
func (e *Engine) Eval(q Queryable, expr Node, evalMS int64) (any, error) {
	switch n := expr.(type) {
	case *NumberLiteral:
		return Scalar(n.Val), nil

	case *VectorSelector:
		return e.evalVectorSelector(q, n, evalMS)

	case *MatrixSelector:
		return e.evalMatrixSelector(q, n, evalMS)

	case *Aggregation:
		return e.evalAggregation(q, n, evalMS)

	case *BinaryExpr:
		return e.evalBinary(q, n, evalMS)

	case *Call:
		return e.evalCall(q, n, evalMS)
	}
	return nil, fmt.Errorf("promql: 평가할 수 없는 노드 %T", expr)
}

func (e *Engine) evalVectorSelector(q Queryable, vs *VectorSelector, evalMS int64) (Vector, error) {
	mint := evalMS - lookbackDelta.Milliseconds()
	querier, closeFn, err := q.Querier(mint, evalMS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()

	var out Vector
	for _, s := range querier.Select(vs.Matchers...) {
		it := s.Iterator()
		var (
			lastT int64
			lastV float64
			found bool
		)
		for it.Next() {
			lastT, lastV = it.At()
			found = true
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		out = append(out, Sample{Labels: s.Labels(), T: lastT, V: lastV})
	}
	return out, nil
}

func (e *Engine) evalMatrixSelector(q Queryable, ms *MatrixSelector, evalMS int64) (Matrix, error) {
	mint := evalMS - ms.Range.Milliseconds()
	querier, closeFn, err := q.Querier(mint, evalMS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()

	var out Matrix
	for _, s := range querier.Select(ms.VS.Matchers...) {
		it := s.Iterator()
		var pts []Point
		for it.Next() {
			t, v := it.At()
			pts = append(pts, Point{T: t, V: v})
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		if len(pts) == 0 {
			continue
		}
		out = append(out, Series{Labels: s.Labels(), Points: pts})
	}
	return out, nil
}

// groupKey 는 집계 그룹의 식별자와 남길 라벨을 만든다. by 면 나열된 라벨만
// 남기고, without 이면 나열된 것과 __name__ 을 뺀 나머지를 남긴다.
func groupKey(lbls tsdb.Labels, by bool, grouping []string) (string, tsdb.Labels) {
	keep := map[string]string{}
	if by {
		for _, g := range grouping {
			if v := lbls.Get(g); v != "" {
				keep[g] = v
			}
		}
	} else {
		drop := map[string]bool{tsdb.MetricName: true}
		for _, g := range grouping {
			drop[g] = true
		}
		for _, l := range lbls {
			if !drop[l.Name] {
				keep[l.Name] = l.Value
			}
		}
	}

	names := make([]string, 0, len(keep))
	for k := range keep {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, k := range names {
		sb.WriteString(k)
		sb.WriteByte(0)
		sb.WriteString(keep[k])
		sb.WriteByte(1)
	}
	return sb.String(), tsdb.LabelsFromMap(keep)
}

func (e *Engine) evalAggregation(q Queryable, a *Aggregation, evalMS int64) (Vector, error) {
	inner, err := e.Eval(q, a.Expr, evalMS)
	if err != nil {
		return nil, err
	}
	vec, ok := inner.(Vector)
	if !ok {
		return nil, fmt.Errorf("promql: %s 는 instant vector 에만 쓸 수 있다", a.Op)
	}

	type acc struct {
		lbls  tsdb.Labels
		sum   float64
		count int
		min   float64
		max   float64
	}
	groups := map[string]*acc{}
	var order []string

	for _, s := range vec {
		key, lbls := groupKey(s.Labels, a.By, a.Grouping)
		g, ok := groups[key]
		if !ok {
			g = &acc{lbls: lbls, min: math.Inf(1), max: math.Inf(-1)}
			groups[key] = g
			order = append(order, key)
		}
		g.sum += s.V
		g.count++
		g.min = math.Min(g.min, s.V)
		g.max = math.Max(g.max, s.V)
	}

	out := make(Vector, 0, len(groups))
	for _, key := range order {
		g := groups[key]
		var v float64
		switch a.Op {
		case "sum":
			v = g.sum
		case "count":
			v = float64(g.count)
		case "avg":
			v = g.sum / float64(g.count)
		case "min":
			v = g.min
		case "max":
			v = g.max
		default:
			return nil, fmt.Errorf("promql: 집계 연산자 %s 를 모른다", a.Op)
		}
		out = append(out, Sample{Labels: g.lbls, T: evalMS, V: v})
	}
	return out, nil
}

func applyOp(op string, l, r float64) (float64, bool, error) {
	switch op {
	case "+":
		return l + r, true, nil
	case "-":
		return l - r, true, nil
	case "*":
		return l * r, true, nil
	case "/":
		return l / r, true, nil
	case "%":
		return math.Mod(l, r), true, nil
	case "==":
		return l, l == r, nil
	case "!=":
		return l, l != r, nil
	case ">":
		return l, l > r, nil
	case "<":
		return l, l < r, nil
	case ">=":
		return l, l >= r, nil
	case "<=":
		return l, l <= r, nil
	}
	return 0, false, fmt.Errorf("promql: 연산자 %s 를 모른다", op)
}

func isComparison(op string) bool {
	switch op {
	case "==", "!=", ">", "<", ">=", "<=":
		return true
	}
	return false
}

func (e *Engine) evalBinary(q Queryable, b *BinaryExpr, evalMS int64) (any, error) {
	lhs, err := e.Eval(q, b.LHS, evalMS)
	if err != nil {
		return nil, err
	}
	rhs, err := e.Eval(q, b.RHS, evalMS)
	if err != nil {
		return nil, err
	}

	ls, lIsScalar := lhs.(Scalar)
	rs, rIsScalar := rhs.(Scalar)

	if lIsScalar && rIsScalar {
		v, keep, err := applyOp(b.Op, float64(ls), float64(rs))
		if err != nil {
			return nil, err
		}
		if isComparison(b.Op) && b.ReturnBool {
			if keep {
				return Scalar(1), nil
			}
			return Scalar(0), nil
		}
		return Scalar(v), nil
	}

	// vector 와 scalar 조합만 지원한다. vector-vector 매칭은 on/ignoring 이
	// 필요해 이번 범위 밖이다.
	var vec Vector
	var scalar float64
	var scalarOnLeft bool
	switch {
	case !lIsScalar && rIsScalar:
		v, ok := lhs.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: 이항 연산의 왼쪽이 instant vector 가 아니다")
		}
		vec, scalar = v, float64(rs)
	case lIsScalar && !rIsScalar:
		v, ok := rhs.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: 이항 연산의 오른쪽이 instant vector 가 아니다")
		}
		vec, scalar, scalarOnLeft = v, float64(ls), true
	default:
		return nil, fmt.Errorf("promql: vector 끼리의 이항 연산은 이번 범위에서 지원하지 않는다(on/ignoring 필요)")
	}

	out := make(Vector, 0, len(vec))
	for _, s := range vec {
		l, r := s.V, scalar
		if scalarOnLeft {
			l, r = scalar, s.V
		}
		v, keep, err := applyOp(b.Op, l, r)
		if err != nil {
			return nil, err
		}
		if isComparison(b.Op) {
			if b.ReturnBool {
				nv := 0.0
				if keep {
					nv = 1
				}
				out = append(out, Sample{Labels: s.Labels, T: s.T, V: nv})
				continue
			}
			if !keep {
				continue
			}
			out = append(out, s)
			continue
		}
		out = append(out, Sample{Labels: s.Labels, T: s.T, V: v})
	}
	return out, nil
}
```

- [ ] **Step 4: 테스트가 통과하는 것을 확인한다**

Run: `go test ./internal/promql/ -run TestEval -v`
Expected: 3개 테스트 PASS. `evalCall` 미구현으로 컴파일이 막히면 임시로
`func (e *Engine) evalCall(Queryable, *Call, int64) (any, error) { return nil, fmt.Errorf("promql: 함수는 다음 태스크에서 구현한다") }`
를 넣고 진행한다 — Task 4 에서 채운다.

실제 API 와 대조 완료: `Labels` 는 `[]Label` 슬라이스라 직접 range 하고(`.Slice()` 는 없다),
`Get`·`MapKey`·`String`·`Hash`·`Copy` 메서드가 있다. `tsdb.Open` 은 `Options` 를 받으므로
테스트에서는 `tsdb.DefaultOptions(dir)` 을 쓴다.

- [ ] **Step 5: 커밋**

```bash
git add internal/promql/eval.go internal/promql/eval_test.go
git commit -m "feat(promql): instant vector 평가와 집계

비교 연산자는 기본이 필터이고 bool modifier 가 붙으면 0/1 변환이다 — 이 둘을
섞으면 알림 룰이 조용히 반대로 동작한다. by 집계는 그룹 라벨만 남긴다(남기면
그룹이 쪼개진다). vector 끼리의 연산은 on/ignoring 이 필요해 명시적으로 거절한다."
```

---

### Task 4: range 함수 — rate·increase 와 counter reset

**Files:**
- Create: `internal/promql/funcs.go`
- Modify: `internal/promql/eval.go` (evalCall 을 실제 구현으로 교체)
- Test: `internal/promql/funcs_test.go`

**Interfaces:**
- Consumes: Task 3 의 `Engine`, `Matrix`, `Vector`
- Produces: `rangeFuncs map[string]func(Series, time.Duration) (float64, bool)` — 반환의 bool 은 "값을 낼 수 있는가"

- [ ] **Step 1: 테스트를 먼저 쓴다**

`internal/promql/funcs_test.go`:

```go
package promql

import (
	"math"
	"testing"
	"time"
)

func mkSeries(pts ...Point) Series { return Series{Points: pts} }

// counter 가 재시작하면 값이 0 으로 떨어진다. 그 하강을 그대로 빼면 음수가
// 나오고, increase 는 "증가량" 이라는 의미를 잃는다. 파드 재시작마다 실제로
// 일어나는 일이라 반드시 처리해야 한다.
func TestIncreaseHandlesCounterReset(t *testing.T) {
	s := mkSeries(
		Point{T: 0, V: 10},
		Point{T: 30000, V: 20},
		Point{T: 60000, V: 5}, // 재시작
		Point{T: 90000, V: 8},
	)
	got, ok := rangeFuncs["increase"](s, 90*time.Second)
	if !ok {
		t.Fatal("increase 가 값을 내지 못했다")
	}
	// 10→20 (+10), 재시작 후 5→8 (+3), 재시작 시점의 20 도 누적으로 인정 → 최소 13
	if got < 13 {
		t.Errorf("increase=%v, want >=13 (재시작 구간이 음수로 상쇄되면 안 된다)", got)
	}
}

func TestRateIsIncreaseOverSeconds(t *testing.T) {
	s := mkSeries(
		Point{T: 0, V: 0},
		Point{T: 60000, V: 60},
	)
	inc, _ := rangeFuncs["increase"](s, time.Minute)
	rt, _ := rangeFuncs["rate"](s, time.Minute)
	if math.Abs(rt-inc/60) > 1e-9 {
		t.Errorf("rate=%v, increase=%v — rate 는 increase/초 여야 한다", rt, inc)
	}
}

// 점이 하나뿐이면 증가량을 알 수 없다. 0 을 내면 "증가 없음" 이라는 거짓이 된다.
func TestRangeFuncsNeedTwoPoints(t *testing.T) {
	s := mkSeries(Point{T: 0, V: 5})
	if _, ok := rangeFuncs["increase"](s, time.Minute); ok {
		t.Error("점 하나로 increase 가 값을 냈다")
	}
	if _, ok := rangeFuncs["rate"](s, time.Minute); ok {
		t.Error("점 하나로 rate 가 값을 냈다")
	}
}

func TestOverTimeFuncs(t *testing.T) {
	s := mkSeries(
		Point{T: 0, V: 3},
		Point{T: 1000, V: 9},
		Point{T: 2000, V: 6},
	)
	cases := map[string]float64{
		"max_over_time":   9,
		"min_over_time":   3,
		"avg_over_time":   6,
		"count_over_time": 3,
		"last_over_time":  6,
	}
	for name, want := range cases {
		got, ok := rangeFuncs[name](s, time.Minute)
		if !ok {
			t.Errorf("%s 가 값을 내지 못했다", name)
			continue
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s=%v, want %v", name, got, want)
		}
	}
}

// changes 는 값이 바뀐 횟수다 — 같은 값이 이어지면 세지 않는다.
func TestChanges(t *testing.T) {
	s := mkSeries(
		Point{T: 0, V: 1},
		Point{T: 1000, V: 1},
		Point{T: 2000, V: 2},
		Point{T: 3000, V: 1},
	)
	got, ok := rangeFuncs["changes"](s, time.Minute)
	if !ok || got != 2 {
		t.Errorf("changes=%v ok=%v, want 2", got, ok)
	}
}
```

- [ ] **Step 2: 테스트가 실패하는 것을 확인한다**

Run: `go test ./internal/promql/ -run 'TestIncrease|TestRate|TestRange|TestOver|TestChanges'`
Expected: 컴파일 실패 — `undefined: rangeFuncs`

- [ ] **Step 3: 함수 테이블을 구현한다**

`internal/promql/funcs.go`:

```go
package promql

import (
	"math"
	"time"
)

// rangeFuncs 는 range vector 를 받아 값 하나를 내는 함수들이다. bool 은
// "값을 낼 수 있는가" — 낼 수 없으면 그 시리즈는 결과에서 빠진다. 0 을
// 대신 내면 "증가 없음" 같은 거짓 사실이 메트릭이 된다.
var rangeFuncs = map[string]func(Series, time.Duration) (float64, bool){
	"increase":        funcIncrease,
	"rate":            funcRate,
	"max_over_time":   overTime(func(acc, v float64) float64 { return math.Max(acc, v) }, math.Inf(-1)),
	"min_over_time":   overTime(func(acc, v float64) float64 { return math.Min(acc, v) }, math.Inf(1)),
	"avg_over_time":   funcAvgOverTime,
	"count_over_time": funcCountOverTime,
	"last_over_time":  funcLastOverTime,
	"changes":         funcChanges,
}

// delta 는 counter reset 을 보정한 누적 증가량이다. 값이 이전보다 작아지면
// counter 가 재시작한 것으로 보고, 재시작 직전 값을 증가분으로 인정한 뒤
// 새 값에서 이어 센다.
func delta(pts []Point) float64 {
	var total float64
	for i := 1; i < len(pts); i++ {
		cur, prev := pts[i].V, pts[i-1].V
		if cur >= prev {
			total += cur - prev
			continue
		}
		total += cur // 재시작: 0 에서 cur 까지 올라온 것으로 본다
	}
	return total
}

func funcIncrease(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) < 2 {
		return 0, false
	}
	return delta(s.Points), true
}

func funcRate(s Series, r time.Duration) (float64, bool) {
	if len(s.Points) < 2 {
		return 0, false
	}
	secs := r.Seconds()
	if secs <= 0 {
		return 0, false
	}
	return delta(s.Points) / secs, true
}

func overTime(fold func(acc, v float64) float64, init float64) func(Series, time.Duration) (float64, bool) {
	return func(s Series, _ time.Duration) (float64, bool) {
		if len(s.Points) == 0 {
			return 0, false
		}
		acc := init
		for _, p := range s.Points {
			acc = fold(acc, p.V)
		}
		return acc, true
	}
}

func funcAvgOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	var sum float64
	for _, p := range s.Points {
		sum += p.V
	}
	return sum / float64(len(s.Points)), true
}

func funcCountOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	return float64(len(s.Points)), true
}

func funcLastOverTime(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	return s.Points[len(s.Points)-1].V, true
}

func funcChanges(s Series, _ time.Duration) (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	var n float64
	for i := 1; i < len(s.Points); i++ {
		if s.Points[i].V != s.Points[i-1].V {
			n++
		}
	}
	return n, true
}
```

- [ ] **Step 4: evalCall 을 실제 구현으로 바꾼다**

`internal/promql/eval.go` 의 임시 `evalCall` 을 지우고 아래로 교체한다:

```go
// instantFuncs 는 instant vector 나 스칼라를 받아 같은 모양을 내는 함수다.
var instantFuncs = map[string]func(float64) float64{
	"abs": math.Abs,
}

func (e *Engine) evalCall(q Queryable, c *Call, evalMS int64) (any, error) {
	if fn, ok := rangeFuncs[c.Func]; ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: %s 는 인자 1개를 받는다", c.Func)
		}
		ms, ok := c.Args[0].(*MatrixSelector)
		if !ok {
			return nil, fmt.Errorf("promql: %s 의 인자는 range vector 여야 한다(예: x[5m])", c.Func)
		}
		mat, err := e.evalMatrixSelector(q, ms, evalMS)
		if err != nil {
			return nil, err
		}
		out := make(Vector, 0, len(mat))
		for _, s := range mat {
			v, ok := fn(s, ms.Range)
			if !ok {
				continue
			}
			out = append(out, Sample{Labels: dropMetricName(s.Labels), T: evalMS, V: v})
		}
		return out, nil
	}

	switch c.Func {
	case "absent":
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: absent 는 인자 1개를 받는다")
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		vec, ok := inner.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: absent 는 instant vector 에만 쓸 수 있다")
		}
		if len(vec) > 0 {
			return Vector{}, nil
		}
		return Vector{{Labels: tsdb.LabelsFromMap(map[string]string{}), T: evalMS, V: 1}}, nil

	case "clamp_min":
		if len(c.Args) != 2 {
			return nil, fmt.Errorf("promql: clamp_min 은 인자 2개를 받는다")
		}
		return e.applyScalarArg(q, c, evalMS, math.Max)

	case "day_of_week":
		return Scalar(float64(time.UnixMilli(evalMS).UTC().Weekday())), nil

	case "hour":
		return Scalar(float64(time.UnixMilli(evalMS).UTC().Hour())), nil

	case "time":
		return Scalar(float64(evalMS) / 1000), nil

	case "vector":
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: vector 는 인자 1개를 받는다")
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		s, ok := inner.(Scalar)
		if !ok {
			return nil, fmt.Errorf("promql: vector 의 인자는 스칼라여야 한다")
		}
		return Vector{{Labels: tsdb.LabelsFromMap(map[string]string{}), T: evalMS, V: float64(s)}}, nil
	}

	if fn, ok := instantFuncs[c.Func]; ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: %s 는 인자 1개를 받는다", c.Func)
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		switch v := inner.(type) {
		case Scalar:
			return Scalar(fn(float64(v))), nil
		case Vector:
			out := make(Vector, 0, len(v))
			for _, s := range v {
				out = append(out, Sample{Labels: dropMetricName(s.Labels), T: s.T, V: fn(s.V)})
			}
			return out, nil
		}
		return nil, fmt.Errorf("promql: %s 에 쓸 수 없는 인자 타입", c.Func)
	}

	return nil, fmt.Errorf("promql: 함수 %s 는 이번 범위에서 지원하지 않는다", c.Func)
}

// applyScalarArg 는 `f(vector, scalar)` 형태를 처리한다.
func (e *Engine) applyScalarArg(q Queryable, c *Call, evalMS int64, fn func(a, b float64) float64) (any, error) {
	inner, err := e.Eval(q, c.Args[0], evalMS)
	if err != nil {
		return nil, err
	}
	argN, err := e.Eval(q, c.Args[1], evalMS)
	if err != nil {
		return nil, err
	}
	sc, ok := argN.(Scalar)
	if !ok {
		return nil, fmt.Errorf("promql: %s 의 두 번째 인자는 스칼라여야 한다", c.Func)
	}
	vec, ok := inner.(Vector)
	if !ok {
		return nil, fmt.Errorf("promql: %s 의 첫 인자는 instant vector 여야 한다", c.Func)
	}
	out := make(Vector, 0, len(vec))
	for _, s := range vec {
		out = append(out, Sample{Labels: s.Labels, T: s.T, V: fn(s.V, float64(sc))})
	}
	return out, nil
}

// dropMetricName 은 값이 변형된 결과에서 __name__ 을 뗀다. Prometheus 와 같은
// 규칙 — rate(x) 의 결과는 더 이상 x 가 아니다.
func dropMetricName(l tsdb.Labels) tsdb.Labels {
	m := map[string]string{}
	for _, lb := range l {
		if lb.Name == tsdb.MetricName {
			continue
		}
		m[lb.Name] = lb.Value
	}
	return tsdb.LabelsFromMap(m)
}
```

- [ ] **Step 5: 테스트가 통과하는 것을 확인한다**

Run: `go test ./internal/promql/ -v`
Expected: 전체 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/promql/funcs.go internal/promql/eval.go internal/promql/funcs_test.go
git commit -m "feat(promql): range 함수와 counter reset 처리

increase/rate 는 값이 이전보다 작아지면 counter 재시작으로 보고 새 값을 그대로
증가분으로 인정한다 — 그대로 빼면 음수가 나와 '증가량' 이라는 의미를 잃는다.
파드 재시작마다 실제로 일어나는 일이다.

점이 하나뿐인 시리즈는 값을 내지 않고 결과에서 뺀다. 0 을 내면 '증가 없음'
이라는 거짓이 메트릭이 된다."
```

---

### Task 5: apiserver 를 새 엔진으로 교체

**Files:**
- Modify: `internal/apiserver/server.go` (`handleQuery`, `handleQueryRange`)
- Test: `internal/apiserver/query_test.go`

**Interfaces:**
- Consumes: Task 3·4 의 `promql.NewEngine()`, `promql.Parse`, `promql.Vector`

- [ ] **Step 1: API 레벨 테스트를 먼저 쓴다**

`internal/apiserver/query_test.go` — 기존 테스트 파일이 있으면 이어 붙인다:

```go
package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 함수가 파싱조차 안 되던 것이 이 태스크의 출발점이다. 이제 통해야 한다.
func TestQueryAcceptsFunctions(t *testing.T) {
	db := newTestDBWithSamples(t) // 기존 테스트 헬퍼를 재사용한다. 없으면 eval_test 의 패턴을 옮긴다.
	srv := httptest.NewServer(NewServer(db))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + `/api/v1/query?query=` + urlEncode(`sum(up)`))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d, want 200 — 함수가 여전히 파싱되지 않는다", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "success" || body.Data.ResultType != "vector" {
		t.Errorf("응답 %+v, want success/vector", body)
	}
}

func TestQueryRejectsUnsupportedWithNamedError(t *testing.T) {
	db := newTestDBWithSamples(t)
	srv := httptest.NewServer(NewServer(db))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + `/api/v1/query?query=` + urlEncode(`topk(3, up)`))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status %d, want 400 — 미지원 문법은 거절해야 한다", resp.StatusCode)
	}
}
```

`urlEncode` 와 `newTestDBWithSamples`, `NewServer` 의 실제 이름은 기존
`internal/apiserver` 테스트에서 확인해 맞춘다.

- [ ] **Step 2: 테스트가 실패하는 것을 확인한다**

Run: `go test ./internal/apiserver/ -run TestQuery`
Expected: `sum(up)` 요청이 400 으로 실패 (현재 parseSelector 가 함수를 모른다)

- [ ] **Step 3: handleQuery 를 교체한다**

`internal/apiserver/server.go` 의 `handleQuery` 본문을 아래로 바꾼다. `parseSelector`
호출과 그 아래 수동 이터레이션 블록이 전부 사라진다:

```go
func handleQuery(db *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		expr, err := promql.Parse(q.Get("query"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}
		evalMS, err := evalTimeMS(q.Get("time"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		res, err := promql.NewEngine().Eval(db, expr, evalMS)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		evalSec := float64(evalMS) / 1000
		switch v := res.(type) {
		case promql.Vector:
			out := make([]queryResult, 0, len(v))
			for _, s := range v {
				out = append(out, queryResult{
					Metric: labelsToMap(s.Labels),
					Value:  [2]any{evalSec, strconv.FormatFloat(s.V, 'g', -1, 64)},
				})
			}
			writeSuccess(w, queryData{ResultType: "vector", Result: out})
		case promql.Scalar:
			writeSuccess(w, map[string]any{
				"resultType": "scalar",
				"result":     [2]any{evalSec, strconv.FormatFloat(float64(v), 'g', -1, 64)},
			})
		default:
			writeError(w, http.StatusBadRequest, "bad_data", "promql: instant 질의가 range vector 를 냈다")
		}
	}
}
```

import 에 `"git.keiailab.com/keiailab-demo/gpumanager/internal/promql"` 를 추가한다.

- [ ] **Step 4: handleQueryRange 도 같은 엔진을 쓰게 한다**

`handleQueryRange` 의 `parseSelector` 호출을 `promql.Parse` 로 바꾸고, step 마다
`Eval(db, expr, t)` 를 호출해 결과 Vector 를 시리즈별로 모은다. 기존의 버킷 채우기
로직(각 버킷 t 의 값은 `(t-queryWindow, t]` 의 최신 샘플)은 이제 엔진의
`lookbackDelta` 가 담당하므로 제거한다.

```go
		var expr promql.Node
		expr, err = promql.Parse(q.Get("query"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		engine := promql.NewEngine()
		byLabels := map[string]*rangeResult{}
		var order []string

		for ts := startMS; ts <= endMS; ts += stepMS {
			res, err := engine.Eval(db, expr, ts)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_data", err.Error())
				return
			}
			vec, ok := res.(promql.Vector)
			if !ok {
				continue
			}
			for _, s := range vec {
				key := s.Labels.MapKey()
				rr, seen := byLabels[key]
				if !seen {
					rr = &rangeResult{Metric: labelsToMap(s.Labels)}
					byLabels[key] = rr
					order = append(order, key)
				}
				rr.Values = append(rr.Values, [2]any{
					float64(ts) / 1000,
					strconv.FormatFloat(s.V, 'g', -1, 64),
				})
			}
		}

		out := make([]rangeResult, 0, len(order))
		for _, k := range order {
			out = append(out, *byLabels[k])
		}
		writeSuccess(w, rangeData{ResultType: "matrix", Result: out})
```

`startMS`/`endMS`/`stepMS` 와 `maxRangePoints` 검증은 기존 코드를 그대로 둔다.

- [ ] **Step 5: 테스트가 통과하는 것을 확인한다**

Run: `go test ./internal/apiserver/ ./internal/promql/ -v`
Expected: 전체 PASS. 기존 apiserver 테스트도 깨지지 않아야 한다 — 셀렉터 전용
질의는 새 엔진에서도 같은 결과를 낸다.

- [ ] **Step 6: 전체 게이트와 커밋**

```bash
make all
git add internal/apiserver/server.go internal/apiserver/query_test.go
git commit -m "feat(apiserver): 질의를 promql 엔진으로 교체

parseSelector 는 이름과 라벨 매처만 팠고 함수는 파싱조차 되지 않았다. 이제
sum(up)·increase(x[5m]) 같은 식이 통하고, 미지원 문법은 이름을 밝히며 400 으로
거절한다.

range 질의의 버킷 채우기 로직은 제거했다 — 엔진의 lookbackDelta 가 같은 일을
하므로 두 곳에 두면 규칙이 갈라진다."
```

---

## Self-Review

**1. 스펙 커버리지**

| 스펙 항목 | 태스크 |
|---|---|
| `internal/promql` 신설, 평가/저장 경계 재정립 | Task 1–4, Task 5(인라인 로직 제거) |
| 13종 함수 | Task 4 (`increase`·`rate`·4종 over_time·`changes`·`absent`·`abs`·`clamp_min`·`day_of_week`·`hour`) |
| 집계 5종 + by/without | Task 3 |
| 이항 연산자 + bool modifier | Task 3 |
| range vector selector | Task 2 |
| 정규식 매처 | Task 2 (`=~`/`!~`) |
| 미지원 문법 명시적 에러 | Task 2(파서 목록), Task 4(함수), Task 5(400 응답) |
| counter reset 처리 | Task 4 |
| 외부 의존성 0 유지 | Global Constraints — 어느 태스크도 `go.mod` 를 건드리지 않는다 |

`last_over_time` 은 스펙의 13종에 포함되고 Task 4 에 구현·테스트가 있다.
스펙의 `min_over_time` 역시 Task 4 `rangeFuncs` 에 있다.

**2. Placeholder scan** — "TBD"·"적절히 처리"·"비슷하게" 없음. 모든 코드 단계에
실제 코드가 있다. `tsdb` 의 실제 식별자 이름이 다를 수 있는 지점 3곳은
"컴파일 에러가 알려준다 + 어느 파일을 보라"까지 적었다.

**3. 타입 일관성** — `Vector`/`Matrix`/`Sample`/`Point`/`Scalar` 는 Task 1 에서 정의하고
이후 태스크가 같은 이름으로 쓴다. `rangeFuncs` 시그니처는 Task 4 정의와 `evalCall`
사용처가 일치한다(`func(Series, time.Duration) (float64, bool)`). `Queryable` 은
Task 3 에서 정의하고 Task 5 가 `*tsdb.DB` 를 그대로 넘긴다.

## 이 계획이 끝나면

`/api/v1/query` 가 함수·집계·연산자를 이해한다. 그 자체로 사용자에게 가치가 있고
(지금은 `up` 같은 셀렉터만 된다), 다음 계획인 **룰 평가 + 발송**의 전제가 갖춰진다.
