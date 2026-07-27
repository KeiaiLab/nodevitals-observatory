package demo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// mustJSON 은 값을 직렬화해 문자열로 낸다 — "응답에 이 문자열이 없어야 한다"
// 류의 노출 검증용.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("직렬화 실패: %v", err)
	}
	return string(b)
}

func newContactEngine(t *testing.T, now *int64) *Engine {
	t.Helper()
	return NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:32"), func() int64 { return *now })
}

func TestSubmitContact_필수값과_길이를_검증한다(t *testing.T) {
	now := t0
	e := newContactEngine(t, &now)

	cases := []struct {
		name               string
		n, org, email, msg string
		wantErr            bool
	}{
		{"정상", "홍길동", "LG U+", "hong@example.com", "도입 문의드립니다", false},
		{"이메일 생략 허용", "홍길동", "", "", "문의", false},
		{"이름 누락", "  ", "", "", "문의", true},
		{"본문 누락", "홍길동", "", "", "   ", true},
		{"이름 초과", strings.Repeat("가", maxContactField+1), "", "", "문의", true},
		{"본문 초과", "홍길동", "", "", strings.Repeat("가", maxContactBodyText+1), true},
		{"이메일 @없음", "홍길동", "", "example.com", "문의", true},
		{"이메일 도메인 없음", "홍길동", "", "hong@", "문의", true},
		{"이메일 tld 없음", "홍길동", "", "hong@example", "문의", true},
		{"이메일 공백 포함", "홍길동", "", "ho ng@example.com", "문의", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now += contactWindowMS // 사례마다 창을 넘겨 접수 상한과 무관하게 검증한다
			_, err := e.SubmitContact(c.n, c.org, c.email, c.msg)
			if c.wantErr && err == nil {
				t.Fatalf("거절돼야 하는데 통과했다")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("통과해야 하는데 거절됐다: %v", err)
			}
		})
	}
}

func TestSubmitContact_앞뒤_공백을_제거하고_최신순으로_낸다(t *testing.T) {
	now := t0
	e := newContactEngine(t, &now)

	if _, err := e.SubmitContact(" 첫째 ", " 소속 ", " a@b.co ", " 내용1 "); err != nil {
		t.Fatalf("접수 실패: %v", err)
	}
	now += 1000
	if _, err := e.SubmitContact("둘째", "", "", "내용2"); err != nil {
		t.Fatalf("접수 실패: %v", err)
	}

	got := e.Contacts()
	if len(got) != 2 {
		t.Fatalf("문의 %d건, want 2", len(got))
	}
	// 최신순 — 운영자가 새 문의를 먼저 본다.
	if got[0].Name != "둘째" {
		t.Fatalf("첫 항목 = %q, want 둘째(최신순)", got[0].Name)
	}
	if got[1].Name != "첫째" || got[1].Org != "소속" || got[1].Email != "a@b.co" || got[1].Message != "내용1" {
		t.Fatalf("공백이 제거되지 않았다: %+v", got[1])
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("ID 가 중복이다: %d", got[0].ID)
	}
}

func TestSubmitContact_분당_상한을_넘으면_거절한다(t *testing.T) {
	now := t0
	e := newContactEngine(t, &now)

	for i := 0; i < maxContactsPerMin; i++ {
		if _, err := e.SubmitContact("홍길동", "", "", "문의"); err != nil {
			t.Fatalf("%d번째 접수가 실패했다: %v", i+1, err)
		}
	}
	// 무인증 공개 엔드포인트다 — 상한이 없으면 봇이 링버퍼를 채워 진짜 문의를
	// 밀어낸다. 상한 초과는 검증 실패(400)와 구분되는 sentinel 이어야 한다.
	_, err := e.SubmitContact("홍길동", "", "", "문의")
	if !errors.Is(err, ErrContactRateLimited) {
		t.Fatalf("상한 초과 err = %v, want ErrContactRateLimited", err)
	}

	// 창이 지나면 다시 받는다 — 영구 차단이면 정상 방문자가 막힌다.
	now += contactWindowMS
	if _, err := e.SubmitContact("홍길동", "", "", "문의"); err != nil {
		t.Fatalf("창이 지난 뒤에도 거절됐다: %v", err)
	}
}

func TestSubmitContact_링버퍼가_상한에서_트림된다(t *testing.T) {
	now := t0
	e := newContactEngine(t, &now)

	total := maxContacts + 30
	for i := 0; i < total; i++ {
		if i%maxContactsPerMin == 0 {
			now += contactWindowMS // 접수 상한 창을 넘겨 트림만 검증한다
		}
		if _, err := e.SubmitContact("홍길동", "", "", "문의"); err != nil {
			t.Fatalf("%d번째 접수가 실패했다: %v", i+1, err)
		}
	}

	got := e.Contacts()
	if len(got) != maxContacts {
		t.Fatalf("보관 %d건, want %d(링버퍼 상한)", len(got), maxContacts)
	}
	// 오래된 것부터 버린다 — 최신 문의가 밀려나면 보관의 의미가 없다.
	if got[0].ID != int64(total) {
		t.Fatalf("최신 ID = %d, want %d", got[0].ID, total)
	}
	if got[len(got)-1].ID != int64(total-maxContacts+1) {
		t.Fatalf("가장 오래된 ID = %d, want %d", got[len(got)-1].ID, total-maxContacts+1)
	}
}

func TestSnapshot_문의를_싣지_않는다(t *testing.T) {
	now := t0
	e := newContactEngine(t, &now)
	if _, err := e.SubmitContact("홍길동", "LG U+", "hong@example.com", "문의"); err != nil {
		t.Fatalf("접수 실패: %v", err)
	}

	// /demo/state 는 public 데모에서 무인증으로 열린다. 스냅샷 JSON 어디에도
	// 방문자 개인정보가 들어가면 안 된다 — 직렬화 결과로 직접 확인한다.
	raw := mustJSON(t, e.Snapshot())
	for _, needle := range []string{"홍길동", "hong@example.com"} {
		if strings.Contains(raw, needle) {
			t.Fatalf("무인증 스냅샷에 문의 정보가 노출됐다: %q", needle)
		}
	}
}
