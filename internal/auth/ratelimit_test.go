package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 로그인 세마포어가 가득 차면 검증(PBKDF2) 없이 즉시 429 로 거절하는지 —
// 미인증 로그인 폭주가 PBKDF2 비용을 증폭시키는 CPU 고갈 DoS 방어가 실제로
// 작동함을 고정한다. 세마포어 가드를 제거하면 이 테스트는 204 로 실패한다.
func TestLoginHandler_동시상한_초과시_429(t *testing.T) {
	a, err := NewAuthenticator("admin", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	// 세마포어를 용량까지 채운다(진행 중인 검증을 흉내).
	for i := 0; i < cap(a.loginSem); i++ {
		a.loginSem <- struct{}{}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"correct-horse-battery"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("세마포어 소진 시 429 여야 한다: got %d", rec.Code)
	}
	// 검증 자체를 안 했으므로 세션 쿠키가 발급되면 안 된다.
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("429 인데 세션 쿠키가 발급됐다")
	}
}

// 세마포어에 여유가 있으면 정상 로그인은 통과하고, 검증 후 세마포어가 해제되어
// 상한이 정상 경로를 막지 않는다. defer 해제를 빠뜨리면 len 이 1 로 실패한다.
func TestLoginHandler_여유있으면_통과하고_세마포어_해제(t *testing.T) {
	a, err := NewAuthenticator("admin", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"correct-horse-battery"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("정상 로그인은 204 여야 한다: got %d", rec.Code)
	}
	if len(a.loginSem) != 0 {
		t.Fatalf("로그인 후 세마포어가 해제되지 않았다: len=%d", len(a.loginSem))
	}
}
