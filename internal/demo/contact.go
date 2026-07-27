package demo

// 문의 접수 — 공개 데모를 본 방문자가 남긴 문의를 서버가 받아 보관한다.
//
// 보관은 링버퍼(알림·감사 로그와 같은 관행)라 파드 재시작으로 휘발되므로,
// 접수 시점에 구조화 로그를 함께 남긴다 — 로그는 남는다.
//
// 이 엔드포인트는 public 데모에서 *무인증*으로 열린다. 그래서 두 가지가
// 방어 대상이다: ① 거대 바디(핸들러의 바이트 상한이 1차, 여기 필드 길이가
// 2차) ② 폭주 — 상한이 없으면 봇이 링버퍼 200칸을 채워 진짜 문의를 밀어낸다.

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

const (
	maxContacts        = 200    // 링버퍼 보관 건수 (scenario.go 의 maxAlerts/maxAudit 관행)
	maxContactsPerMin  = 20     // 분당 접수 상한 — 초과분은 거절(429)
	maxContactField    = 200    // 이름·소속·이메일 각각의 길이 상한
	maxContactBodyText = 4000   // 문의 본문 길이 상한
	contactWindowMS    = 60_000 // 접수 상한 창(1분)
)

// ErrContactRateLimited 는 분당 접수 상한 초과다 — 핸들러가 429 로 옮긴다.
// 다른 검증 실패(400)와 구분되어야 하므로 별도 sentinel 이다.
var ErrContactRateLimited = errors.New("문의 접수가 일시적으로 몰렸다 — 잠시 후 다시 시도")

// ContactMessage 는 접수된 문의 1건이다.
type ContactMessage struct {
	ID      int64  `json:"id"`
	At      int64  `json:"at"`
	Name    string `json:"name"`
	Org     string `json:"org,omitempty"`
	Email   string `json:"email,omitempty"`
	Message string `json:"message"`
}

// SubmitContact 는 문의를 검증하고 보관한다. 검증 실패는 error(400), 접수
// 상한 초과는 ErrContactRateLimited(429) 다.
func (e *Engine) SubmitContact(name, org, email, message string) (ContactMessage, error) {
	name = strings.TrimSpace(name)
	org = strings.TrimSpace(org)
	email = strings.TrimSpace(email)
	message = strings.TrimSpace(message)

	if name == "" {
		return ContactMessage{}, errors.New("이름을 입력해 주세요")
	}
	if message == "" {
		return ContactMessage{}, errors.New("문의 내용을 입력해 주세요")
	}
	for label, v := range map[string]string{"이름": name, "소속": org, "이메일": email} {
		if len([]rune(v)) > maxContactField {
			return ContactMessage{}, fmt.Errorf("%s는 %d자를 넘을 수 없습니다", label, maxContactField)
		}
	}
	if len([]rune(message)) > maxContactBodyText {
		return ContactMessage{}, fmt.Errorf("문의 내용은 %d자를 넘을 수 없습니다", maxContactBodyText)
	}
	// 이메일은 선택 입력이지만, 넣었다면 회신 가능한 형태여야 한다. 정규식으로
	// 엄밀히 판정하지 않는다 — 실제 유효성은 회신해 봐야 알고, 과한 검증은
	// 멀쩡한 주소를 거절한다.
	if email != "" && !plausibleEmail(email) {
		return ContactMessage{}, errors.New("이메일 형식을 확인해 주세요")
	}

	now := e.nowFn()

	e.mu.Lock()
	defer e.mu.Unlock()

	if now-e.contactWindowStart >= contactWindowMS {
		e.contactWindowStart, e.contactWindowCount = now, 0
	}
	if e.contactWindowCount >= maxContactsPerMin {
		return ContactMessage{}, ErrContactRateLimited
	}
	e.contactWindowCount++

	e.contactSeq++
	msg := ContactMessage{ID: e.contactSeq, At: now, Name: name, Org: org, Email: email, Message: message}
	e.contacts = append(e.contacts, msg)
	if len(e.contacts) > maxContacts {
		e.contacts = e.contacts[len(e.contacts)-maxContacts:]
	}

	// 링버퍼는 재시작으로 사라지므로 로그가 실질 보관처다.
	slog.Info("데모 문의 접수",
		"id", msg.ID, "name", msg.Name, "org", msg.Org, "email", msg.Email, "message", msg.Message)
	return msg, nil
}

// Contacts 는 보관 중인 문의를 최신순으로 복사해 낸다.
//
// 반환값이 인증된 조회 경로에서만 쓰인다는 전제가 중요하다 — 방문자 이름·
// 이메일이라 Snapshot(무인증 /demo/state)에는 절대 싣지 않는다.
func (e *Engine) Contacts() []ContactMessage {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]ContactMessage, 0, len(e.contacts))
	for i := len(e.contacts) - 1; i >= 0; i-- {
		out = append(out, e.contacts[i])
	}
	return out
}

// plausibleEmail 은 "회신해 볼 만한 형태"인지만 본다 — local@domain.tld 골격.
func plausibleEmail(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" {
		return false
	}
	if strings.Contains(domain, "@") {
		return false
	}
	host, tld, ok := strings.Cut(domain, ".")
	return ok && host != "" && tld != ""
}
