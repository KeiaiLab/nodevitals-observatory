// Package auth 는 nodevitals-observatory 웹 콘솔의 인증을 구현한다: PBKDF2
// 비밀번호 해시, 인메모리 세션 스토어, 로그인 상태를 강제하는 HTTP 미들웨어,
// 로그인/로그아웃 핸들러. 단일 admin 계정을 전제한다(m4-constraints.md "인증"
// 절, m4-design.md §1). 외부 의존 0 — stdlib 전용(crypto/pbkdf2·sha256·rand·
// subtle, encoding/base64·json, net/http, sync, time, strings, strconv,
// errors, fmt).
//
// import 방향은 단방향이다: apiserver → auth. auth 는 tsdb·webui 어느 쪽도
// 의존하지 않는다.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- §1.1 PBKDF2 비밀번호 해시 (m4-design.md §1.1) ----

const (
	// pbkdf2NewHashIterations 는 신규 해시 발급(HashPassword) 시 사용하는
	// 반복수 — OWASP Password Storage Cheat Sheet(2023) PBKDF2-HMAC-SHA256
	// 권고치. (설계 초안의 상수 210_000 은 PBKDF2-HMAC-SHA512 권고치를
	// SHA-256 값으로 오귀속한 것이었다 — m4-design.md 보안 적대검토 F-SEC-1
	// 정정: SHA-256 실제 권고는 600_000이다.)
	pbkdf2NewHashIterations = 600_000
	// pbkdf2MinIterations 는 검증(verifyPassword) 시 허용하는 반복수 하한이다.
	// 사전 주입된 해시가 이 미만이면 다운그레이드 공격으로 간주해 거부한다.
	// 신규 발급은 항상 pbkdf2NewHashIterations 를 쓰므로 이 하한에 걸리지
	// 않는다(발급 반복수와 검증 거부 하한을 분리 — F-SEC-1 조치).
	pbkdf2MinIterations = 210_000

	pbkdf2SaltLen = 16 // salt 바이트 길이
	pbkdf2KeyLen  = 32 // 유도 키 바이트 길이

	phcPrefix = "$pbkdf2-sha256$"
)

// HashPassword 는 평문 비밀번호를 PHC 유사 포맷 문자열로 해시한다:
//
//	$pbkdf2-sha256$i=600000$<b64url(salt16)>$<b64url(dk32)>
//
// salt 는 매 호출 crypto/rand 로 새로 뽑는다(16B). 반환값은 그대로 저장·주입
// 가능한 자기서술 포맷이다 — NewAuthenticator 의 사전해시 주입 경로가 이
// 포맷을 그대로 받아들인다.
func HashPassword(plain string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt 생성 실패: %w", err)
	}
	dk, err := pbkdf2.Key(sha256.New, plain, salt, pbkdf2NewHashIterations, pbkdf2KeyLen)
	if err != nil {
		// 반복수·키길이가 상수라 정상 경로에서는 발생하지 않는다 — 발생하면
		// 프로그래밍 오류 수준이다(m4-design.md §1.1: FIPS 파라미터 검증용 에러).
		return "", fmt.Errorf("auth: pbkdf2 유도 실패: %w", err)
	}
	return fmt.Sprintf("%si=%d$%s$%s", phcPrefix, pbkdf2NewHashIterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(dk)), nil
}

// verifyPassword 는 encoded(HashPassword 산출 포맷)를 파싱해 동일 파라미터로
// plain 을 재유도한 뒤 subtle.ConstantTimeCompare 로 비교한다. 파싱 실패·
// 반복수 하한 미달·유도 실패는 모두 false 로 뭉뚱그린다 — encoded 원문을
// 에러 메시지에 실어 시크릿을 노출하는 경로를 만들지 않기 위함이다
// (m4-design.md §1.1).
func verifyPassword(encoded, plain string) bool {
	iter, salt, want, ok := parsePHC(encoded)
	if !ok || iter < pbkdf2MinIterations {
		return false
	}
	// 유도 키 길이를 want 와 동일하게 맞춰 ConstantTimeCompare 가 항상
	// 동일길이 슬라이스를 비교하게 한다(길이 불일치 조기반환 경로를 아예
	// 만들지 않아 실질적 상수시간을 지킨다).
	got, err := pbkdf2.Key(sha256.New, plain, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parsePHC 는 "$pbkdf2-sha256$i=<iter>$<b64url salt>$<b64url dk>" 를 분해한다.
// 형식이 어긋나면 ok=false.
func parsePHC(encoded string) (iter int, salt, dk []byte, ok bool) {
	rest, hasPrefix := strings.CutPrefix(encoded, phcPrefix)
	if !hasPrefix {
		return 0, nil, nil, false
	}
	parts := strings.Split(rest, "$")
	if len(parts) != 3 {
		return 0, nil, nil, false
	}
	iterStr, hasIterPrefix := strings.CutPrefix(parts[0], "i=")
	if !hasIterPrefix {
		return 0, nil, nil, false
	}
	iter, err := strconv.Atoi(iterStr)
	if err != nil || iter <= 0 {
		return 0, nil, nil, false
	}
	salt, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, nil, nil, false
	}
	dk, err = base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, nil, nil, false
	}
	return iter, salt, dk, true
}

// randomBase64 는 crypto/rand n 바이트를 base64.RawURLEncoding 문자열로 낸다
// (세션 토큰·decoy 시드 공용 helper).
func randomBase64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ---- §1.3 SessionStore (m4-design.md §1.3) ----

const (
	idleTTL     = 8 * time.Hour      // 유휴 만료 — 이 시간 접근 없으면 무효
	absoluteTTL = 7 * 24 * time.Hour // 절대 만료 — 유휴 갱신과 무관하게 강제 만료
	tokenBytes  = 32                 // 세션 토큰 원본 바이트 수(256bit)

	// sweepThreshold 초과 시 Validate 진입 시점에 만료 항목을 1회 전체
	// 정리한다. 별도 janitor goroutine 을 두지 않는 것에 대한 성장 상한
	// 안전장치 — 실질 방어선은 PBKDF2 비용이다(단일 admin 이라 Create 는
	// 로그인 성공에만 호출된다, m4-design.md §1.3).
	sweepThreshold = 1024
)

// session 은 SessionStore 가 토큰별로 보관하는 메타데이터다.
type session struct {
	createdAt time.Time // 절대 만료 기준
	lastSeen  time.Time // 유휴 만료 기준 — Validate 성공마다 now 로 갱신(sliding)
}

// SessionStore 는 토큰→세션을 인메모리로 관장한다(map + mutex, 별도 goroutine
// 없음 — lazy expiry, m4-design.md §1.3). 시계는 now 로 주입해 만료 테스트를
// 결정론화한다(scrape.Options.Now 관례 미러).
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
	now      func() time.Time
}

// NewSessionStore 는 now 가 nil 이면 time.Now 를 쓴다. 테스트는 반드시 고정
// 시계를 주입해야 한다(m4-design.md D4 — sleep 동기화 금지).
func NewSessionStore(now func() time.Time) *SessionStore {
	if now == nil {
		now = time.Now
	}
	return &SessionStore{sessions: make(map[string]session), now: now}
}

// Create 는 crypto/rand 32B 를 base64.RawURLEncoding 으로 인코딩한 토큰(43자)을
// 발급하고 신규 세션을 등록한다. rand 실패는 error 로 반환한다 — 무작위성 없는
// 토큰 발급은 절대 금지(m4-design.md §1.3).
func (s *SessionStore) Create() (string, error) {
	token, err := randomBase64(tokenBytes)
	if err != nil {
		return "", fmt.Errorf("auth: 세션 토큰 생성 실패: %w", err)
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session{createdAt: now, lastSeen: now}
	return token, nil
}

// Validate 는 토큰이 존재하고 유휴/절대 만료를 모두 통과하면 true 를 반환하며
// lastSeen 을 now 로 갱신한다(sliding 유휴 만료). 만료된 항목은 이 자리에서
// 즉시 삭제한다(lazy expiry). 세션 수가 sweepThreshold 를 넘으면 이번 호출에서
// 만료분 전체를 한 번 쓸어낸다(m4-design.md §1.3).
func (s *SessionStore) Validate(token string) bool {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) > sweepThreshold {
		s.sweepExpiredLocked(now)
	}

	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	if expired(sess, now) {
		delete(s.sessions, token)
		return false
	}
	sess.lastSeen = now
	s.sessions[token] = sess
	return true
}

// Delete 는 토큰의 세션을 즉시 제거한다. 존재하지 않는 토큰은 no-op(멱등) —
// 로그아웃 이중 호출·이미 만료된 토큰 모두 안전하다.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// expired 는 sess 가 now 시점 기준 유휴 또는 절대 만료를 초과했는지 본다.
func expired(sess session, now time.Time) bool {
	return now.Sub(sess.lastSeen) >= idleTTL || now.Sub(sess.createdAt) >= absoluteTTL
}

// sweepExpiredLocked 는 만료된 세션을 전량 제거한다. 호출자가 s.mu 를 이미
// 쥐고 있어야 한다. range 도중 삭제는 Go map 스펙상 안전하다(신규 추가 없이
// 삭제만 하므로).
func (s *SessionStore) sweepExpiredLocked(now time.Time) {
	for token, sess := range s.sessions {
		if expired(sess, now) {
			delete(s.sessions, token)
		}
	}
}

// ---- §1.2 Authenticator (m4-design.md §1.2) ----

// ErrInvalidCredentials 는 Login 실패의 유일한 사유값이다 — 계정 존재 여부를
// 구분해 노출하지 않는다(m4-constraints.md "로그인 실패는 계정존재 노출 안
// 하는 단일 메시지").
var ErrInvalidCredentials = errors.New("인증 실패")

// Authenticator 는 단일 admin 계정 + 세션을 관장한다.
type Authenticator struct {
	user      string // admin username
	passHash  string // PHC 포맷(§1.1)
	decoyHash string // 타이밍 균등화용 더미 해시 — 생성자에서 1회 산출
	sessions  *SessionStore
	loginSem  chan struct{} // 동시 로그인 검증 상한 (PBKDF2 CPU 증폭 DoS 방어)
}

// authConfig 는 NewAuthenticator 의 Option 이 채우는 내부 설정이다.
type authConfig struct {
	now func() time.Time
}

// Option 은 NewAuthenticator 의 선택 설정이다.
type Option func(*authConfig)

// WithNow 는 내부 SessionStore 의 시계를 주입한다(만료 테스트 결정론화,
// m4-design.md D4). 지정하지 않으면 time.Now.
func WithNow(now func() time.Time) Option {
	return func(c *authConfig) { c.now = now }
}

// NewAuthenticator 는 passHashOrPlain 이 "$pbkdf2-sha256$" 프리픽스면 해시로
// 그대로 채택하고, 아니면 평문으로 보고 내부에서 HashPassword 를 1회
// 수행한다 — env 로 평문이 오는 운영 기본 경로와 사전해시 주입 경로를 모두
// 허용한다(m4-design.md §1.2).
func NewAuthenticator(user, passHashOrPlain string, opts ...Option) (*Authenticator, error) {
	var cfg authConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	passHash := passHashOrPlain
	if !strings.HasPrefix(passHashOrPlain, phcPrefix) {
		h, err := HashPassword(passHashOrPlain)
		if err != nil {
			return nil, fmt.Errorf("auth: admin 비밀번호 해시 실패: %w", err)
		}
		passHash = h
	}

	// decoyHash 는 실사용되지 않는 무작위 평문의 해시다 — Login 에서 username
	// 이 불일치할 때도 이 해시에 대해 verifyPassword 를 수행해 PBKDF2 비용을
	// 균등화한다(계정 존재 타이밍 비노출, m4-design.md §1.2). 생성자에서 1회만
	// 산출해 매 Login 호출마다 재계산하지 않는다.
	decoySeed, err := randomBase64(pbkdf2SaltLen)
	if err != nil {
		return nil, fmt.Errorf("auth: decoy 시드 생성 실패: %w", err)
	}
	decoyHash, err := HashPassword(decoySeed)
	if err != nil {
		return nil, fmt.Errorf("auth: decoy 해시 생성 실패: %w", err)
	}

	return &Authenticator{
		user:      user,
		passHash:  passHash,
		decoyHash: decoyHash,
		sessions:  NewSessionStore(cfg.now),
		loginSem:  make(chan struct{}, maxConcurrentLogins),
	}, nil
}

// Login 은 user/pass 가 admin 계정과 일치하면 신규 세션 토큰을 발급한다
// (세션 고정 방지 — 로그인 성공 시에만 토큰 발급, m4-design.md D2).
// username 비교는 subtle.ConstantTimeCompare, 불일치 시에도 decoyHash 에 대해
// verifyPassword 를 정확히 1회 수행해 PBKDF2 비용을 균등화한다(계정 존재
// 타이밍 비노출). 실패 사유는 구분 없이 ErrInvalidCredentials 하나뿐이다.
func (a *Authenticator) Login(user, pass string) (string, error) {
	userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1

	targetHash := a.decoyHash
	if userMatch {
		targetHash = a.passHash
	}
	passOK := verifyPassword(targetHash, pass)

	if !userMatch || !passOK {
		return "", ErrInvalidCredentials
	}

	token, err := a.sessions.Create()
	if err != nil {
		return "", fmt.Errorf("auth: 세션 발급 실패: %w", err)
	}
	return token, nil
}

// Logout 은 세션을 즉시 삭제한다. 존재하지 않는 토큰은 no-op(멱등).
func (a *Authenticator) Logout(token string) {
	a.sessions.Delete(token)
}

// Validate 는 토큰 유효 여부를 판정하며 성공 시 유휴 타임스탬프를 갱신한다
// (SessionStore.Validate 의 sliding 유휴 만료를 그대로 위임).
func (a *Authenticator) Validate(token string) bool {
	return a.sessions.Validate(token)
}

// ---- §1.4 쿠키 helper (m4-design.md §1.4) ----

// CookieName 은 세션 쿠키 이름이다.
const CookieName = "observatory_session"

// setSessionCookie 는 HttpOnly+Secure+SameSite=Lax+Path=/+MaxAge=absoluteTTL(초)
// 로 세션 쿠키를 심는다. Secure 는 항상 켠다 — 라이브는 keiailab-edge TLS
// 종단 뒤이고, 로컬 검증은 curl/httptest(플래그 무시) 또는 localhost(브라우저가
// secure context 로 취급)라 무해하다(m4-design.md §1.4).
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(absoluteTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie 는 동일 속성 + MaxAge=-1 + 빈 값으로 쿠키를 말소한다.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ---- §1.5 Middleware + 핸들러 (m4-design.md §1.5) ----

// maxLoginBodyBytes 는 로그인 요청 바디 상한이다(m4-design.md §1.5 — 1KiB
// 초과 400, http.MaxBytesReader 로 강제).
const maxLoginBodyBytes = 1024

// maxConcurrentLogins 는 동시에 진행 중인 로그인 검증(각각 600k-iter PBKDF2)의
// 상한이다. 미인증 로그인 폭주가 CPU 를 고갈시키는 증폭 DoS 를 막는다 — 초과분은
// PBKDF2 를 수행하지 않고 즉시 429 로 거절한다(NetBird-gate 위의 심층 방어).
const maxConcurrentLogins = 4

// authErrorResponse 는 apiserver.errorResponse 와 동일 스키마를 auth 패키지가
// 자체 보유한다(문자열 3필드) — apiserver → auth 단방향 의존을 지키기 위한
// import 순환 회피(m4-design.md §1.5).
type authErrorResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

// writeUnauthorizedJSON 은 M2 에러 응답과 동일 스키마의 401 JSON 을 낸다.
func writeUnauthorizedJSON(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(authErrorResponse{
		Status: "error", ErrorType: "unauthorized", Error: msg,
	})
}

// Middleware 는 next 를 세션 검증 뒤로 감싼다. 미인증 시:
//   - r.URL.Path 가 "/api/" 프리픽스 → 401 + M2 에러 JSON 포맷
//   - 그 외(콘솔 경로) → http.Redirect(303, "/") — SPA 가 로그인 화면을
//     클라이언트 렌더한다(D1). M4 배선상 Middleware 는 API 경로에만 걸리지만
//     계약상 dual 동작을 보유한다(m4-design.md §1.5).
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err == nil && a.Validate(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeUnauthorizedJSON(w, "인증이 필요하다")
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}

// loginRequest 는 LoginHandler 요청 바디 스키마다.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginHandler 는 POST /api/v1/auth/login 을 처리한다.
//   - Content-Type 이 application/json 프리픽스가 아니면 415.
//   - 바디가 maxLoginBodyBytes(1KiB) 초과 또는 JSON 파싱 실패면 400.
//   - 자격 불일치는 401 단일 메시지(ErrInvalidCredentials 텍스트).
//   - 세션 발급 자체의 내부 장애(rand 실패 등, 자격과 무관)는 500.
//   - 성공은 Set-Cookie + 204(m4-design.md §1.5).
func (a *Authenticator) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 동시 로그인 검증 상한 — 초과분은 PBKDF2 를 수행하지 않고 즉시 429 로
		// 거절해 CPU 증폭 DoS 를 막는다.
		select {
		case a.loginSem <- struct{}{}:
			defer func() { <-a.loginSem }()
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		token, err := a.Login(req.Username, req.Password)
		switch {
		case err == nil:
			setSessionCookie(w, token)
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrInvalidCredentials):
			writeUnauthorizedJSON(w, err.Error())
		default:
			// crypto/rand 실패 등 자격과 무관한 내부 장애 — 원본 에러 텍스트를
			// 클라이언트에 노출하지 않는다(D5).
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
}

// LogoutHandler 는 POST /api/v1/auth/logout 을 처리한다. 쿠키의 세션을
// 삭제하고 쿠키를 말소한 뒤 204 를 낸다 — 쿠키가 없어도 204(멱등, 인증 불요.
// webui 의 로그아웃 fetch 는 본문·Content-Type 없이 호출하므로 여기서는
// LoginHandler 와 달리 Content-Type 을 강제하지 않는다, m4-design.md §1.5/§2).
func (a *Authenticator) LogoutHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(CookieName); err == nil {
			a.Logout(cookie.Value)
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}
