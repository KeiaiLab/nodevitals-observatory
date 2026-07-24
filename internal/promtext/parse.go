// Package promtext 는 Prometheus text exposition format(v0.0.4)을 파싱한다.
//
// stdlib 만 사용하고 정규식은 쓰지 않는다 — 3800 시리즈 × 10 타겟 × 15s 스크레이프
// 주기를 감당해야 하므로, bufio.Scanner 로 줄을 읽은 뒤 수동 문자 스캔으로
// 이름 / 라벨블록 / 값 / 옵션 타임스탬프를 읽는다. 라벨 블록 안은 콤마로
// split 하지 않는다 — 라벨 값 자체에 콤마가 올 수 있어 split 이 값을 깨뜨린다.
package promtext

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxLineBytes 는 bufio.Scanner 가 허용하는 한 줄의 최대 바이트 수다.
// 비정상적으로 긴 줄이 무제한 메모리를 잡아먹지 않도록 상한을 둔다.
const maxLineBytes = 1 << 20 // 1MiB

// Series 는 exposition 한 줄이다. TimestampMS==0 은 "타임스탬프 없음"을 뜻한다.
type Series struct {
	Name        string
	Labels      map[string]string // 라벨 없으면 nil 또는 빈 맵 — 호출자는 len 으로만 판단
	Value       float64
	TimestampMS int64
}

// Parse 는 r 이 담은 Prometheus text exposition format 을 파싱해 Series 목록을
// 낸다.
//   - `# HELP` / `# TYPE` / 기타 `#` 로 시작하는 주석 줄, 빈 줄은 무시한다.
//   - 라벨 값 이스케이프 3종(\\, \", \n)을 해석한다.
//   - 값은 strconv.ParseFloat 이 받아들이는 전부(NaN/±Inf 대소문자 무관 포함)를 받는다.
//   - 옵션 타임스탬프(밀리초 정수)가 없으면 TimestampMS=0.
//   - 형식이 어긋난 줄을 만나면 즉시 줄번호를 포함한 에러를 반환한다 — 조용히
//     건너뛰지 않는다.
func Parse(r io.Reader) ([]Series, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var out []Series
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}
		s, err := parseLine(line, lineNo)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("promtext: 스캔 실패: %w", err)
	}
	return out, nil
}

// parseLine 은 데이터 한 줄(이미 주석·빈줄이 아님이 확인됨)을
// 이름 → [라벨블록] → 값 → [타임스탬프] 순서로 스캔한다.
func parseLine(line string, lineNo int) (Series, error) {
	p := &lineScanner{s: line, lineNo: lineNo}
	p.skipSpaces() // 관례상 선행 공백은 없지만 방어적으로 허용

	name := p.scanName()
	if name == "" {
		return Series{}, p.errorf("메트릭 이름이 비어 있다")
	}
	p.skipSpaces()

	var labels map[string]string
	if p.peek() == '{' {
		var err error
		labels, err = p.scanLabelBlock()
		if err != nil {
			return Series{}, err
		}
		p.skipSpaces()
	}

	valTok := p.scanToken()
	if valTok == "" {
		return Series{}, p.errorf("값이 없다")
	}
	val, err := strconv.ParseFloat(valTok, 64)
	if err != nil {
		return Series{}, p.errorf("값 파싱 실패 %q: %v", valTok, err)
	}
	p.skipSpaces()

	var tsMS int64
	if !p.atEnd() {
		tsTok := p.scanToken()
		tsMS, err = strconv.ParseInt(tsTok, 10, 64)
		if err != nil {
			return Series{}, p.errorf("타임스탬프 파싱 실패 %q: %v", tsTok, err)
		}
		p.skipSpaces()
		if !p.atEnd() {
			return Series{}, p.errorf("줄 끝에 처리되지 않은 내용이 남아 있다: %q", p.s[p.pos:])
		}
	}

	return Series{Name: name, Labels: labels, Value: val, TimestampMS: tsMS}, nil
}

// lineScanner 는 한 줄을 바이트 위치 기준으로 훑는 최소 상태 스캐너다.
// UTF-8 멀티바이트 문자의 continuation byte 는 항상 0x80 이상이라, 아래에서
// 비교하는 ASCII 구두점(`{` `}` `"` `\` `,` `=` 공백)과 절대 충돌하지 않는다 —
// 바이트 단위로 스캔해도 UTF-8 라벨 값을 안전하게 그대로 복사할 수 있다.
type lineScanner struct {
	s      string
	pos    int
	lineNo int
}

func (p *lineScanner) errorf(format string, args ...any) error {
	return fmt.Errorf("promtext: %d번째 줄: %s", p.lineNo, fmt.Sprintf(format, args...))
}

func (p *lineScanner) atEnd() bool { return p.pos >= len(p.s) }

func (p *lineScanner) peek() byte {
	if p.atEnd() {
		return 0
	}
	return p.s[p.pos]
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' }

func (p *lineScanner) skipSpaces() {
	for !p.atEnd() && isSpace(p.s[p.pos]) {
		p.pos++
	}
}

// scanName 은 공백 또는 '{' 전까지를 메트릭 이름으로 읽는다.
func (p *lineScanner) scanName() string {
	start := p.pos
	for !p.atEnd() && !isSpace(p.s[p.pos]) && p.s[p.pos] != '{' {
		p.pos++
	}
	return p.s[start:p.pos]
}

// scanToken 은 공백으로 구분되는 다음 토큰(값 또는 타임스탬프)을 읽는다.
func (p *lineScanner) scanToken() string {
	start := p.pos
	for !p.atEnd() && !isSpace(p.s[p.pos]) {
		p.pos++
	}
	return p.s[start:p.pos]
}

// scanLabelBlock 은 이미 '{' 를 확인한 상태에서 호출되어 라벨 블록 전체를
// 문자 단위로 스캔한다. 콤마로 split 하지 않는다 — 라벨 값 안에 콤마가 올 수
// 있어(이스케이프 대상이 아님) split 은 값을 깨뜨린다.
func (p *lineScanner) scanLabelBlock() (map[string]string, error) {
	p.pos++ // consume '{'
	labels := map[string]string{}

	p.skipSpaces()
	if p.peek() == '}' {
		p.pos++
		return labels, nil
	}

	for {
		p.skipSpaces()
		nameStart := p.pos
		for !p.atEnd() && p.s[p.pos] != '=' && p.s[p.pos] != '}' && p.s[p.pos] != ',' && !isSpace(p.s[p.pos]) {
			p.pos++
		}
		labelName := p.s[nameStart:p.pos]
		if labelName == "" {
			return nil, p.errorf("라벨 이름이 비어 있다")
		}
		p.skipSpaces()
		if p.atEnd() || p.s[p.pos] != '=' {
			return nil, p.errorf("라벨 %q 뒤에 '=' 이 없다", labelName)
		}
		p.pos++ // consume '='
		p.skipSpaces()
		if p.atEnd() || p.s[p.pos] != '"' {
			return nil, p.errorf("라벨 %q 값이 큰따옴표로 시작하지 않는다", labelName)
		}
		p.pos++ // consume opening '"'

		val, err := p.scanQuotedValue(labelName)
		if err != nil {
			return nil, err
		}
		labels[labelName] = val

		p.skipSpaces()
		if p.atEnd() {
			return nil, p.errorf("라벨 블록이 '}' 로 닫히지 않았다")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
			// Prometheus 명세상 라벨 리스트의 trailing comma(`m{a="1",}`)는
			// 유효하다 — 콤마 소비 직후 곧바로 '}' 면 새 라벨을 기대하지 않고
			// 블록을 닫는다. 이 검사가 없으면 다음 루프의 라벨이름 스캔이
			// '}' 를 종료문자로만 취급해 0바이트를 읽고 "라벨 이름이 비어
			// 있다" 로 오분류한다.
			p.skipSpaces()
			if p.peek() == '}' {
				p.pos++
				return labels, nil
			}
			continue
		case '}':
			p.pos++
			return labels, nil
		default:
			return nil, p.errorf("라벨 %q 값 뒤에 예기치 않은 문자 %q", labelName, p.s[p.pos])
		}
	}
}

// scanQuotedValue 는 여는 '"' 를 이미 소비한 상태에서 시작해 닫는 '"' 까지
// Prometheus 명세의 이스케이프 3종(\\, \", \n)을 해석하며 문자 단위로 스캔한다.
func (p *lineScanner) scanQuotedValue(labelName string) (string, error) {
	var b strings.Builder
	for {
		if p.atEnd() {
			return "", p.errorf("라벨 %q 값의 큰따옴표가 닫히지 않았다", labelName)
		}
		c := p.s[p.pos]
		switch c {
		case '"':
			p.pos++
			return b.String(), nil
		case '\\':
			p.pos++
			if p.atEnd() {
				return "", p.errorf("라벨 %q 값이 이스케이프 문자로 끝났다", labelName)
			}
			switch p.s[p.pos] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			default:
				return "", p.errorf("라벨 %q 값에 알 수 없는 이스케이프 시퀀스 \\%c", labelName, p.s[p.pos])
			}
			p.pos++
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}
