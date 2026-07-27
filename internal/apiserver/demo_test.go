// demo_test.go 는 데모 모드 API 배선을 검증한다 — status 공개(on/off 양쪽),
// state 의 인증 요구, off 시 라우트 미등록, 액션의 409/200 경로.
package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/demo"
)

const demoT0 = int64(1_700_000_000_000)

// newDemoEngine 은 소형 플릿 엔진을 만들어 Run 으로 ready 상태까지 올린다
// (백필 창 0 — 1틱만 emit). t.Cleanup 으로 루프를 취소한다.
func newDemoEngine(t *testing.T) *demo.Engine {
	t.Helper()
	specs, err := demo.ParseFleetSpec("a:클라우드 A:16,b:클라우드 B:8")
	if err != nil {
		t.Fatalf("플릿 스펙 파싱 실패: %v", err)
	}
	cfg := demo.Config{Seed: 7, Fleet: specs, TimeScale: 1.0}
	engine := demo.NewEngine(newTestDB(t), cfg, func() int64 { return demoT0 })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go engine.Run(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for !engine.Ready() {
		if time.Now().After(deadline) {
			t.Fatalf("데모 엔진이 ready 에 도달하지 못했다")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return engine
}

func TestDemoStatus_모드별(t *testing.T) {
	// off — 실서비스 배선(WithDemo 없음).
	offSrv := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t)))
	defer offSrv.Close()

	resp, err := http.Get(offSrv.URL + "/api/v1/demo/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	var off struct {
		Status string         `json:"status"`
		Data   demoStatusData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&off); err != nil {
		t.Fatalf("status 디코드: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || off.Data.Enabled {
		t.Fatalf("off status = %d enabled=%v, want 200/false", resp.StatusCode, off.Data.Enabled)
	}

	// off 에서 state 는 미등록 → 기존 JSON 404 계약.
	resp, err = http.Get(offSrv.URL + "/api/v1/demo/state")
	if err != nil {
		t.Fatalf("GET state(off): %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("off state status = %d, want 404", resp.StatusCode)
	}

	// on
	engine := newDemoEngine(t)
	onSrv := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t), WithDemo(engine)))
	defer onSrv.Close()

	resp, err = http.Get(onSrv.URL + "/api/v1/demo/status")
	if err != nil {
		t.Fatalf("GET status(on): %v", err)
	}
	var on struct {
		Data demoStatusData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&on); err != nil {
		t.Fatalf("status(on) 디코드: %v", err)
	}
	resp.Body.Close()
	if !on.Data.Enabled || !on.Data.Ready {
		t.Fatalf("on status enabled=%v ready=%v, want true/true", on.Data.Enabled, on.Data.Ready)
	}
}

// public demo — 조회 API 가 인증 없이 열리고, 데모 엔진이 없으면 그 옵션이
// 무시되어 실서비스 인증이 유지되는지(오설정 방어) 검증한다.
func TestDemoPublic_인증해제와_실서비스_방어(t *testing.T) {
	engine := newDemoEngine(t)
	pub := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t),
		WithDemo(engine), WithDemoPublic()))
	defer pub.Close()

	for _, path := range []string{"/api/v1/labels", "/api/v1/demo/state"} {
		resp, err := http.Get(pub.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("public demo %s = %d, want 200(인증 없이 열림)", path, resp.StatusCode)
		}
	}

	// status 가 public=true 를 알린다(프론트의 로그인 UI 숨김 근거).
	resp, err := http.Get(pub.URL + "/api/v1/demo/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	var body struct {
		Data demoStatusData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("status 디코드: %v", err)
	}
	resp.Body.Close()
	if !body.Data.Public {
		t.Fatalf("public demo status.public = false, want true")
	}

	// 액션도 인증 없이 통과해야 시연 조작이 성립한다.
	actResp, err := http.Post(pub.URL+"/api/v1/demo/actions/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST reset: %v", err)
	}
	io.Copy(io.Discard, actResp.Body)
	actResp.Body.Close()
	if actResp.StatusCode != http.StatusOK {
		t.Fatalf("public demo reset = %d, want 200", actResp.StatusCode)
	}

	// 데모 엔진 없이 WithDemoPublic 만 주면 무시된다 — 실서비스 인증 유지.
	svc := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t), WithDemoPublic()))
	defer svc.Close()
	resp2, err := http.Get(svc.URL + "/api/v1/labels")
	if err != nil {
		t.Fatalf("GET labels(실서비스): %v", err)
	}
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("데모 엔진 없는 WithDemoPublic 에서 labels = %d, want 401(인증 유지)", resp2.StatusCode)
	}
}

// 문의 접수는 방문자용이라 public 데모에서 열리지만, 문의 *조회* 는 이름·
// 이메일이 담기므로 public 이어도 반드시 인증돼야 한다.
//
// 이 테스트가 막는 회귀는 구체적이다: 문의 조회를 다른 데모 라우트처럼
// protect() 로 감싸는 것. public 데모에서 protect 는 통과 래퍼(server.go)라
// 그 순간 방문자 개인정보가 전 세계에 공개된다.
func TestDemoContact_접수는_공개_조회는_항상_인증(t *testing.T) {
	engine := newDemoEngine(t)
	pub := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t),
		WithDemo(engine), WithDemoPublic()))
	defer pub.Close()

	post := func(body string) int {
		t.Helper()
		resp, err := http.Post(pub.URL+"/api/v1/demo/contact", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST contact: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(`{"name":"홍길동","org":"LG U+","email":"hong@example.com","message":"도입 문의"}`); got != http.StatusOK {
		t.Fatalf("public demo 문의 접수 = %d, want 200(무인증 접수)", got)
	}
	if got := post(`{"name":"","message":""}`); got != http.StatusBadRequest {
		t.Fatalf("빈 문의 = %d, want 400", got)
	}

	// 조회는 public 데모여도 401 — 이 한 줄이 개인정보 공개 회귀의 방벽이다.
	resp, err := http.Get(pub.URL + "/api/v1/demo/contacts")
	if err != nil {
		t.Fatalf("GET contacts: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public demo 문의 조회 = %d, want 401(항상 인증)", resp.StatusCode)
	}
	if strings.Contains(string(raw), "hong@example.com") {
		t.Fatalf("401 응답 본문에 문의 정보가 새어 나왔다: %s", raw)
	}

	// 접수 상한 초과는 400 이 아니라 429 여야 한다 — 방문자가 "내가 잘못 썼다"와
	// "잠시 후 다시"를 구분할 수 있어야 한다.
	limited := false
	for i := 0; i < 40; i++ {
		if post(`{"name":"봇","message":"스팸"}`) == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("연속 접수에도 429 가 나오지 않았다 — 폭주 방어가 없다")
	}

	// 실서비스(데모 엔진 부재)에는 문의 경로 자체가 없다.
	svc := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t)))
	defer svc.Close()
	offResp, err := http.Post(svc.URL+"/api/v1/demo/contact", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST contact(실서비스): %v", err)
	}
	io.Copy(io.Discard, offResp.Body)
	offResp.Body.Close()
	// 404 든 405 든 상관없다 — 계약은 "실서비스에는 문의 경로가 없다"는 것이다.
	// (mux 에 GET /api/ 가 등록돼 있어 같은 경로의 POST 는 405 로 나온다.)
	if offResp.StatusCode != http.StatusNotFound && offResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("실서비스 문의 접수 = %d, want 404/405(미등록)", offResp.StatusCode)
	}
}

// 신규 컨트롤 액션이 HTTP 표면에서 파라미터까지 온전히 전달되는지 —
// 바디 필드가 시나리오 파라미터로 매핑되지 않으면 화면 조작이 조용히 무시된다.
func TestDemoAction_컨트롤_파라미터_전달(t *testing.T) {
	engine := newDemoEngine(t)
	srv := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t),
		WithDemo(engine), WithDemoPublic()))
	defer srv.Close()

	post := func(action string, body any) (int, string) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}
		resp, err := http.Post(srv.URL+"/api/v1/demo/actions/"+action, "application/json", &buf)
		if err != nil {
			t.Fatalf("POST %s: %v", action, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	if code, body := post("set-mode", map[string]string{"mode": "limited-auto"}); code != http.StatusOK {
		t.Fatalf("set-mode = %d %s", code, body)
	}
	if got := engine.Snapshot().Scenario.Mode; got != "limited-auto" {
		t.Fatalf("모드가 반영되지 않았다: %q", got)
	}

	if code, body := post("configure-burnin", map[string]any{
		"profile": "B200-Blackwell-v1", "durationMin": 45, "targetUtilPct": 90,
	}); code != http.StatusOK {
		t.Fatalf("configure-burnin = %d %s", code, body)
	}
	b := engine.Snapshot().Scenario.Victim.Burnin
	if b.DurationMin != 45 || b.TargetUtilPct != 90 || b.Profile != "B200-Blackwell-v1" {
		t.Fatalf("번인 설정 미반영: %+v", b)
	}

	// 범위 밖은 409 로 거절되고 기존 값이 유지된다.
	if code, _ := post("configure-burnin", map[string]any{"targetUtilPct": 500}); code != http.StatusConflict {
		t.Fatalf("범위 밖 targetUtilPct = %d, want 409", code)
	}
	if engine.Snapshot().Scenario.Victim.Burnin.TargetUtilPct != 90 {
		t.Fatalf("거절된 요청이 값을 덮어썼다")
	}

	if code, body := post("jump-phase", map[string]string{"phase": "burnin-failed"}); code != http.StatusOK {
		t.Fatalf("jump-phase = %d %s", code, body)
	}
	if got := engine.Snapshot().Scenario.Phase; got != "burnin-failed" {
		t.Fatalf("단계 점프 미반영: %q", got)
	}
	if code, _ := post("jump-phase", map[string]string{"phase": "nope"}); code != http.StatusConflict {
		t.Fatalf("알 수 없는 단계 = %d, want 409", code)
	}

	if code, body := post("ack-alert", map[string]any{"id": 1}); code != http.StatusOK {
		t.Fatalf("ack-alert = %d %s", code, body)
	}
}

func TestDemoState_인증과_본문(t *testing.T) {
	engine := newDemoEngine(t)
	srv := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t), WithDemo(engine)))
	defer srv.Close()

	// 미인증 → 401.
	resp, err := http.Get(srv.URL + "/api/v1/demo/state")
	if err != nil {
		t.Fatalf("GET state 미인증: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("미인증 state = %d, want 401", resp.StatusCode)
	}

	// 로그인 후 → 200 + 스냅샷 필드.
	cookie := loginForTest(t, srv)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/demo/state", nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET state 인증: %v", err)
	}
	var state struct {
		Status string        `json:"status"`
		Data   demo.Snapshot `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("state 디코드: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("인증 state = %d, want 200", resp.StatusCode)
	}
	if state.Data.Fleet.GPUTotal != 24 {
		t.Fatalf("fleet.gpuTotal = %d, want 24", state.Data.Fleet.GPUTotal)
	}
	if state.Data.Scenario.Phase != string(demo.PhaseNormal) {
		t.Fatalf("scenario.phase = %s, want normal", state.Data.Scenario.Phase)
	}
	if state.Data.Scenario.Victim.UUID == "" {
		t.Fatalf("victim 이 비어 있다")
	}
}

func TestDemoAction_경로들(t *testing.T) {
	engine := newDemoEngine(t)
	srv := httptest.NewServer(NewServer(newTestDB(t), nil, newTestAuthenticator(t), WithDemo(engine)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	post := func(action string, body any) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode body: %v", err)
			}
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/demo/actions/"+action, &buf)
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", action, err)
		}
		return resp
	}

	// 알 수 없는 액션 → 404.
	resp := post("no-such-action", nil)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown action = %d, want 404", resp.StatusCode)
	}

	// normal 단계에서 격리 승인 → 409 (단계 불일치 도메인 거절).
	resp = post(string(demo.ActionApproveIsolation), nil)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("normal 단계 approve = %d, want 409", resp.StatusCode)
	}

	// reset 은 어느 단계에서든 200.
	resp = post(string(demo.ActionReset), nil)
	var out struct {
		Data demo.ActionResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("reset 디코드: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !out.Data.Applied {
		t.Fatalf("reset = %d applied=%v, want 200/true", resp.StatusCode, out.Data.Applied)
	}

	// 미인증 액션 → 401.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/demo/actions/reset", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST 미인증: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("미인증 액션 = %d, want 401", resp.StatusCode)
	}
}
