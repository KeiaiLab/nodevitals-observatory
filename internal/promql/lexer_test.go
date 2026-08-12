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
