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
