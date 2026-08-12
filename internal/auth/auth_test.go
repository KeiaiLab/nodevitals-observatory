package auth

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- HashPassword / verifyPassword (m4-design.md §1.1 + §6) ----

func TestHashPassword_포맷과_파라미터(t *testing.T) {
	encoded, err := HashPassword("s3cr3t-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	wantPrefix := fmt.Sprintf("%si=%d$", phcPrefix, pbkdf2NewHashIterations)
	if !strings.HasPrefix(encoded, wantPrefix) {
		t.Fatalf("encoded 접두부 = %q, want prefix %q", encoded, wantPrefix)
	}

	iter, salt, dk, ok := parsePHC(encoded)
	if !ok {
		t.Fatalf("parsePHC 실패: %q", encoded)
	}
	// 절대 하한 assert(F-SEC-1 회귀 방지) — 아래 "iter != pbkdf2NewHashIterations"
	// 비교는 상수 자체를 참조원으로 삼는 동어반복이라, pbkdf2NewHashIterations 가
	// 600000→210000(원래 F-SEC-1 버그였던 SHA-512 오귀속 값)으로 되돌아가도
	// 항상 통과한다. OWASP PBKDF2-HMAC-SHA256 최소 권고치를 리터럴로 못박아
	// 상수 회귀를 테스트가 실제로 검출하게 한다.
	if pbkdf2NewHashIterations < 600_000 {
		t.Errorf("pbkdf2NewHashIterations = %d, want >= 600000(OWASP PBKDF2-HMAC-SHA256 최소 권고, F-SEC-1 회귀 방지)",
			pbkdf2NewHashIterations)
	}
	if iter != pbkdf2NewHashIterations {
		t.Errorf("iter = %d, want %d", iter, pbkdf2NewHashIterations)
	}
	if len(salt) != pbkdf2SaltLen {
		t.Errorf("salt 길이 = %d, want %d", len(salt), pbkdf2SaltLen)
	}
	if len(dk) != pbkdf2KeyLen {
		t.Errorf("dk 길이 = %d, want %d", len(dk), pbkdf2KeyLen)
	}
}

func TestHashPassword_매호출마다_다른_salt(t *testing.T) {
	a, err := HashPassword("same-plaintext")
	if err != nil {
		t.Fatalf("HashPassword(1st): %v", err)
	}
	b, err := HashPassword("same-plaintext")
	if err != nil {
		t.Fatalf("HashPassword(2nd): %v", err)
	}
	if a == b {
		t.Fatalf("동일 평문의 두 해시가 동일하다 — salt 미적용 의심: %q", a)
	}
}

func TestVerifyPassword_정합과_오비번(t *testing.T) {
	const plain = "correct horse battery staple"
	encoded, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !verifyPassword(encoded, plain) {
		t.Error("올바른 비밀번호가 거부됐다")
	}
	if verifyPassword(encoded, "wrong password") {
		t.Error("잘못된 비밀번호가 수용됐다")
	}
}

// TestVerifyPassword_iter하한_미달_거부 는 반복수 210000 미만으로 수동
// 구성한 legacy 포맷을 거부하는지 본다(다운그레이드 공격 방지, §1.1).
func TestVerifyPassword_iter하한_미달_거부(t *testing.T) {
	const weakIter = 1000 // pbkdf2MinIterations(210000) 미만
	salt := make([]byte, pbkdf2SaltLen)
	dk, err := pbkdf2.Key(sha256.New, "legacy-pass", salt, weakIter, pbkdf2KeyLen)
	if err != nil {
		t.Fatalf("pbkdf2.Key: %v", err)
	}
	weak := fmt.Sprintf("%si=%d$%s$%s", phcPrefix, weakIter,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(dk))

	if verifyPassword(weak, "legacy-pass") {
		t.Error("반복수 하한(210000) 미만 해시가 수용됐다 — 다운그레이드 방지 실패")
	}
}

// TestVerifyPassword_손상된_포맷_거부 는 parsePHC 의 각 실패 분기를 개별
// 케이스로 명시 격리한다 — 하나의 뭉뚱그린 assert 로는 실제로 어느 분기가
// 실행됐는지 알 수 없어 회귀를 놓칠 수 있다(테스트 함정 자가점검).
func TestVerifyPassword_손상된_포맷_거부(t *testing.T) {
	cases := []struct {
		name    string
		encoded string
	}{
		{"완전빈문자열", ""},
		{"PHC프리픽스없음", "plaintext-not-phc"},
		{"필드개수부족", "$pbkdf2-sha256$i=600000$AAAA"},
		{"i프리픽스없음", "$pbkdf2-sha256$x=600000$AAAA$BBBB"},
		{"iter숫자아님", "$pbkdf2-sha256$i=not-a-number$AAAA$BBBB"},
		{"iter0이하", "$pbkdf2-sha256$i=0$AAAA$BBBB"},
		{"salt_base64아님", "$pbkdf2-sha256$i=600000$not-valid-base64!!$BBBB"},
		{"dk_base64아님", "$pbkdf2-sha256$i=600000$AAAA$not-valid-base64!!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if verifyPassword(c.encoded, "whatever") {
				t.Errorf("손상된 포맷이 수용됐다: %q", c.encoded)
			}
		})
	}
}

// TestVerifyPassword_dk빈값이면_pbkdf2유도실패_거부 는 parsePHC 자체는 통과
// 시키지만(dk 필드가 빈 문자열도 유효한 base64 디코드 결과다) 그로 인해
// keyLength=0 이 되어 pbkdf2.Key 호출 자체가 에러를 내는 경로를 겨냥한다
// (stdlib 계약: keyLength ∈ [1, ...]). verifyPassword 의 "유도 실패 → false"
// 분기가 실제로 실행되는지 확인한다(그 전까지는 도달 불가능한 죽은 코드처럼
// 보일 수 있었다).
func TestVerifyPassword_dk빈값이면_pbkdf2유도실패_거부(t *testing.T) {
	salt := base64.RawURLEncoding.EncodeToString(make([]byte, pbkdf2SaltLen))
	encoded := fmt.Sprintf("%si=%d$%s$", phcPrefix, pbkdf2NewHashIterations, salt)

	if verifyPassword(encoded, "whatever") {
		t.Errorf("dk 빈 값(keyLength=0)이 수용됐다: %q", encoded)
	}
}

// ---- Authenticator: NewAuthenticator / Login / Logout / Validate (§1.2) ----

func TestNewAuthenticator_평문경로_로그인성공(t *testing.T) {
	a, err := NewAuthenticator("admin", "hunter2")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	token, err := a.Login("admin", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !a.Validate(token) {
		t.Error("발급된 토큰이 유효하지 않다")
	}
}

// TestNewAuthenticator_사전해시_주입경로 는 HashPassword 산출물을
// NewAuthenticator 에 그대로 주입해도 평문 경로와 동일하게 로그인이
// 성공하는지 본다(m4-design.md §1.2 "사전해시 주입 경로").
func TestNewAuthenticator_사전해시_주입경로(t *testing.T) {
	const plain = "preHashedSecret!"
	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	a, err := NewAuthenticator("admin", hash)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	if _, err := a.Login("admin", plain); err != nil {
		t.Errorf("사전해시 주입 경로 로그인 실패: %v", err)
	}
}

func TestLogin_오비번_거부(t *testing.T) {
	a, err := NewAuthenticator("admin", "correct-pass")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	_, err = a.Login("admin", "wrong-pass")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
}

// TestLogin_잘못된_유저도_동일_에러 는 계정 존재 여부가 에러 값으로
// 노출되지 않는지 본다 — 잘못된 user·잘못된 password 모두 동일한
// ErrInvalidCredentials 값이어야 한다(m4-constraints.md, m4-design.md §1.2).
func TestLogin_잘못된_유저도_동일_에러(t *testing.T) {
	a, err := NewAuthenticator("admin", "correct-pass")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	_, errWrongUser := a.Login("no-such-user", "correct-pass")
	_, errWrongPass := a.Login("admin", "wrong-pass")

	if !errors.Is(errWrongUser, ErrInvalidCredentials) {
		t.Errorf("errWrongUser = %v, want ErrInvalidCredentials", errWrongUser)
	}
	if !errors.Is(errWrongPass, ErrInvalidCredentials) {
		t.Errorf("errWrongPass = %v, want ErrInvalidCredentials", errWrongPass)
	}
	if errWrongUser != errWrongPass {
		t.Errorf("에러 값이 다르다(계정 존재 노출 위험): %v vs %v", errWrongUser, errWrongPass)
	}
}

func TestLogout_후_토큰무효(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	token, err := a.Login("admin", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	a.Logout(token)

	if a.Validate(token) {
		t.Error("로그아웃 후에도 토큰이 유효하다")
	}
}

func TestLogout_존재하지않는토큰_noop(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	a.Logout("no-such-token-ever-issued") // 패닉·에러 없이 통과해야 한다(멱등)
}

// ---- SessionStore (m4-design.md §1.3 + §6) ----

func TestSessionStore_Create_토큰길이와_유일성(t *testing.T) {
	store := NewSessionStore(nil)

	const trials = 1000
	seen := make(map[string]bool, trials)
	for i := 0; i < trials; i++ {
		token, err := store.Create()
		if err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
		if len(token) != 43 {
			t.Fatalf("토큰 길이 = %d, want 43: %q", len(token), token)
		}
		if seen[token] {
			t.Fatalf("중복 토큰 발생(회차 %d): %q", i, token)
		}
		seen[token] = true
	}
}

func TestSessionStore_Validate_성공시_lastSeen_갱신_sliding유휴만료(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewSessionStore(func() time.Time { return now })

	token, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// idleTTL(8h) 이내로 전진 — 유휴 만료가 아직 걸리지 않아야 한다.
	now = now.Add(1 * time.Hour)
	if !store.Validate(token) {
		t.Fatal("1h 경과 후에도 유효해야 한다(idleTTL 8h 이내)")
	}

	// lastSeen 이 방금 갱신됐으므로, idleTTL 에 육박하지만 아직 넘지 않는
	// 만큼 다시 전진해도 유효해야 한다 — 갱신이 실제로 반영됐는지가 관건.
	// (갱신이 없었다면 최초 Create 시각 기준으로 이미 idleTTL 을 넘겨 만료됐을
	// 시점이다.)
	now = now.Add(idleTTL - time.Minute)
	if !store.Validate(token) {
		t.Fatal("lastSeen 갱신이 반영되지 않았다 — sliding 유휴 만료 실패")
	}
}

func TestSessionStore_유휴8시간초과_만료(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewSessionStore(func() time.Time { return now })

	token, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	now = now.Add(idleTTL + time.Second) // 접근 없이 유휴 만료 경계 초과
	if store.Validate(token) {
		t.Error("유휴 8h 초과 후에도 유효 판정됐다")
	}
}

// TestSessionStore_절대7일초과_유휴갱신반복해도_만료 는 idleTTL 미만 간격으로
// 반복 접근해 유휴 만료는 계속 회피하되, 누적 경과가 absoluteTTL(7d) 을
// 넘으면 그래도 만료돼야 함을 본다(m4-design.md §1.3 "유휴 갱신 반복해도 만료").
func TestSessionStore_절대7일초과_유휴갱신반복해도_만료(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start
	store := NewSessionStore(func() time.Time { return now })

	token, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	step := idleTTL - time.Minute // idleTTL 미만 간격 — 유휴 만료는 걸리지 않는다
	var lastValid bool
	steps := int(absoluteTTL/step) + 2
	for i := 0; i < steps; i++ {
		now = now.Add(step)
		lastValid = store.Validate(token)
	}

	if now.Sub(start) <= absoluteTTL {
		t.Fatalf("테스트 전제 실패 — 누적 경과(%v)가 absoluteTTL(%v)를 넘지 못했다",
			now.Sub(start), absoluteTTL)
	}
	if lastValid {
		t.Error("절대 만료(7d, createdAt 기준) 초과 후에도 유효 판정됐다")
	}
}

func TestSessionStore_Delete_후_무효(t *testing.T) {
	store := NewSessionStore(nil)
	token, err := store.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	store.Delete(token)

	if store.Validate(token) {
		t.Error("Delete 후에도 토큰이 유효하다")
	}
}

func TestSessionStore_Delete_존재하지않는토큰_noop(t *testing.T) {
	store := NewSessionStore(nil)
	store.Delete("no-such-token-ever-issued") // 패닉 없이 통과해야 한다
}

// TestSessionStore_Validate_발급된적없는토큰은_false 는 "존재하지 않는 토큰"
// (map 조회 자체가 미스) 분기를 "존재했으나 만료됨" 분기와 분리해 겨냥한다 —
// 위 유휴/절대 만료 테스트들은 전부 한 번 Create 된 토큰을 대상으로 하므로
// 이 분기(ok=false 즉시 반환)를 커버하지 못한다.
func TestSessionStore_Validate_발급된적없는토큰은_false(t *testing.T) {
	store := NewSessionStore(nil)
	if store.Validate("this-token-was-never-issued-by-Create") {
		t.Error("발급된 적 없는 토큰이 유효하다고 판정됐다")
	}
}

// TestSessionStore_sweep_1024초과시_만료분_제거 는 별도 janitor goroutine이
// 없는 대신 Validate 진입 시점에 세션 수가 sweepThreshold 를 넘으면 만료분을
// 전량 정리하는지 본다(m4-design.md §1.3).
func TestSessionStore_sweep_1024초과시_만료분_제거(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewSessionStore(func() time.Time { return now })

	const expiredCount = sweepThreshold + 100
	expiredTokens := make([]string, 0, expiredCount)
	for i := 0; i < expiredCount; i++ {
		token, err := store.Create()
		if err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
		expiredTokens = append(expiredTokens, token)
	}

	// 시계만 전진시켜 위 전량을 유휴 만료 상태로 만든다(Validate 호출 없이 —
	// lastSeen 갱신을 유발하지 않기 위함).
	now = now.Add(idleTTL + time.Second)

	// sweepThreshold 초과 상태에서 신규(미만료) 토큰 하나로 Validate 를 걸어
	// sweepExpiredLocked 트리거 조건(len > sweepThreshold)을 만족시킨다.
	fresh, err := store.Create()
	if err != nil {
		t.Fatalf("Create(fresh): %v", err)
	}

	if !store.Validate(fresh) {
		t.Fatal("갓 생성한 세션이 유효하지 않다고 판정됐다")
	}
	if got := len(store.sessions); got != 1 {
		t.Errorf("sweep 후 세션 수 = %d, want 1(fresh 만 생존)", got)
	}
	if store.Validate(expiredTokens[0]) {
		t.Error("만료된 세션이 sweep 후에도 유효 판정됐다")
	}
}

// TestSessionStore_동시접근_레이스없음 은 SessionStore 의 mutex 보호가
// -race 검사 하에서도 안전한지 본다(다중 HTTP 요청의 동시 Validate/Create
// 모사).
func TestSessionStore_동시접근_레이스없음(t *testing.T) {
	store := NewSessionStore(nil)

	const n = 50
	tokens := make([]string, n)
	for i := range tokens {
		token, err := store.Create()
		if err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
		tokens[i] = token
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			store.Validate(token)
		}(tokens[i])
	}
	wg.Wait()
}

// ---- Middleware (m4-design.md §1.5 + §6) ----

func TestMiddleware_미인증_API경로는_401_JSON(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	handler := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("미인증 요청이 next 핸들러까지 도달했다")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var body authErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("응답 디코드 실패: %v", err)
	}
	if body.Status != "error" || body.ErrorType != "unauthorized" {
		t.Errorf("body = %+v, want status=error errorType=unauthorized", body)
	}
}

func TestMiddleware_미인증_콘솔경로는_303_루트로(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	handler := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("미인증 요청이 next 핸들러까지 도달했다")
	}))

	req := httptest.NewRequest(http.MethodGet, "/console", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
}

func TestMiddleware_유효쿠키는_next에_도달(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	token, err := a.Login("admin", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	reached := false
	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("유효 쿠키인데 next 핸들러에 도달하지 못했다")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---- LoginHandler / LogoutHandler: httptest 실 HTTP 왕복 (m4-design.md §6) ----

// newAuthTestServer 는 login/logout/보호된 조회 라우트 하나씩을 얹은 실
// httptest.Server 를 띄운다(ResponseRecorder 가 아니라 실 TCP 왕복 —
// m4-constraints.md "httptest 로 실제 HTTP 왕복", apiserver/server_test.go 관례 미러).
func newAuthTestServer(t *testing.T, a *Authenticator) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/auth/login", a.LoginHandler())
	mux.Handle("POST /api/v1/auth/logout", a.LogoutHandler())
	mux.Handle("GET /api/v1/protected", a.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestLoginHandler_정상로그인_쿠키플래그_전수(t *testing.T) {
	a, err := NewAuthenticator("admin", "s3cret!")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	resp := postJSON(t, base+"/api/v1/auth/login", `{"username":"admin","password":"s3cret!"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie 개수 = %d, want 1: %+v", len(cookies), cookies)
	}
	c := cookies[0]
	if c.Name != CookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, CookieName)
	}
	if c.Value == "" {
		t.Error("cookie value 가 비어있다")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly 플래그 누락")
	}
	if !c.Secure {
		t.Error("Secure 플래그 누락")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax(%v)", c.SameSite, http.SameSiteLaxMode)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
}

func TestLoginHandler_오비번_401_단일메시지(t *testing.T) {
	a, err := NewAuthenticator("admin", "correct-pass")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	resp := postJSON(t, base+"/api/v1/auth/login", `{"username":"admin","password":"wrong"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var body authErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("디코드 실패: %v", err)
	}
	if body.Status != "error" || body.ErrorType != "unauthorized" || body.Error != ErrInvalidCredentials.Error() {
		t.Errorf("body = %+v, want status=error errorType=unauthorized error=%q",
			body, ErrInvalidCredentials.Error())
	}
}

func TestLoginHandler_ContentType_누락시_415(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	resp, err := http.Post(base+"/api/v1/auth/login", "text/plain",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

// TestLoginHandler_ContentType_완전누락시_415 는 위 테스트("잘못된 값")와
// 달리 Content-Type 헤더 자체를 아예 보내지 않는 경우를 겨냥한다 — 서버측
// 검사가 `ct != "application/json"` 류의 부주의한 비교라면 빈 문자열을
// 실수로 통과시킬 수 있어 별도 검증 가치가 있다.
func TestLoginHandler_ContentType_완전누락시_415(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"pw"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// Content-Type 헤더를 의도적으로 설정하지 않는다(net/http 클라이언트는
	// 요청 바디를 스니핑해 자동 설정하지 않으므로 헤더가 완전히 빠진다).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestLoginHandler_1KiB초과_body_400(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	huge := `{"username":"admin","password":"` + strings.Repeat("x", 2048) + `"}`
	resp := postJSON(t, base+"/api/v1/auth/login", huge)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestLogoutHandler_쿠키없이도_204(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	resp, err := http.Post(base+"/api/v1/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// TestLogoutHandler_로그아웃후_동일쿠키_401 은 로그인 → 보호 경로 접근 성공 →
// 로그아웃 → 동일 쿠키로 동일 경로 재접근 시 401 로 막히는지를 E2E 로 본다
// (m4-design.md §6 "로그아웃 후 해당 토큰 Validate false").
func TestLogoutHandler_로그아웃후_동일쿠키_401(t *testing.T) {
	a, err := NewAuthenticator("admin", "pw")
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	base := newAuthTestServer(t, a)

	loginResp := postJSON(t, base+"/api/v1/auth/login", `{"username":"admin","password":"pw"}`)
	defer loginResp.Body.Close()
	cookies := loginResp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie 개수 = %d, want 1", len(cookies))
	}
	sessionCookie := cookies[0]

	// 로그아웃 전: 보호 경로 접근 가능해야 한다.
	req1, err := http.NewRequest(http.MethodGet, base+"/api/v1/protected", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req1.AddCookie(sessionCookie)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("GET protected(로그인 상태): %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("로그아웃 전 protected status = %d, want 200", resp1.StatusCode)
	}

	logoutReq, err := http.NewRequest(http.MethodPost, base+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	logoutReq.AddCookie(sessionCookie)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp.StatusCode)
	}

	req2, err := http.NewRequest(http.MethodGet, base+"/api/v1/protected", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.AddCookie(sessionCookie)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET protected(로그아웃 후): %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("로그아웃 후 protected status = %d, want 401(동일 토큰이 여전히 유효하면 안 된다)",
			resp2.StatusCode)
	}
}
