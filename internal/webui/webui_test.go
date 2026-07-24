// Package webui 테스트 — m4-design.md §6 계약: GET / 200+마크업 · GET
// /assets/* 200 · GET /nonexistent 404 · FS() 자산 3종 존재. httptest 실왕복.
package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_루트_200_로그인마크업_포함(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html 접두", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `id="login-form"`) {
		t.Fatalf("body 에 로그인 마크업(login-form) 미포함:\n%s", body)
	}
}

func TestHandler_assets_app및style_200(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	for _, name := range []string{"app.js", "style.css"} {
		resp, err := http.Get(srv.URL + "/assets/" + name)
		if err != nil {
			t.Fatalf("GET /assets/%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /assets/%s status = %d, want 200", name, resp.StatusCode)
		}
	}
}

func TestHandler_존재하지않는경로_404(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandler_assets_디렉터리_404 는 회귀 테스트다: http.FileServerFS 의
// 디렉터리 index.html 자동서빙 기본동작으로 GET /assets/ 가 "그 외 → 404"
// 계약(m4-design.md §2)을 어기고 200+로그인 마크업을 새 나간 적이 있다
// (적대검증 발견) — 재발 방지.
func TestHandler_assets_디렉터리_404(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/assets/")
	if err != nil {
		t.Fatalf("GET /assets/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (디렉터리 index.html 자동서빙 회귀)", resp.StatusCode)
	}
}

func TestFS_3개자산_존재(t *testing.T) {
	root := FS()

	for _, name := range []string{"index.html", "app.js", "style.css"} {
		if info, err := fs.Stat(root, name); err != nil {
			t.Errorf("FS() 에 %s 부재: %v", name, err)
		} else if info.IsDir() {
			t.Errorf("FS() 의 %s 가 디렉터리 — 파일이어야 한다", name)
		}
	}
}
