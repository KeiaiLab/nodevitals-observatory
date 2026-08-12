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
