// Package webui 테스트 — m5-design.md §2 D2 계약: GET /assets/* 는 실재하는
// 일반 파일만 정적 서빙(부재·디렉터리 404), 그 외 비-API GET 은 파일 우선 →
// index.html 폴백(SPA, React Router 새로고침·직접 URL 진입 지원). 핸들러 계약
// (serveAsset/serveSPA)은 fstest.MapFS 픽스처로 헤르메틱하게 검증하고(프론트
// 빌드 산출물 유무에 비의존 — m5-design.md 적대검토 F5), 실 embed(FS()/
// Handler())는 배선 스모크로 별도 확인한다.
package webui

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// ---- serveAsset 계약 (fixture — 실 embed 구조에 비의존) ----

func TestServeAsset_실재파일_200_no_cache헤더(t *testing.T) {
	root := fstest.MapFS{
		"app.js": {Data: []byte("console.log('fixture')")},
	}
	srv := httptest.NewServer(withNoCache(serveAsset(root)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/app.js")
	if err != nil {
		t.Fatalf("GET /app.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "console.log('fixture')" {
		t.Fatalf("body = %q, want fixture 원문 그대로", body)
	}
}

func TestServeAsset_부재파일_404(t *testing.T) {
	root := fstest.MapFS{"app.js": {Data: []byte("x")}}
	srv := httptest.NewServer(serveAsset(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/missing.js")
	if err != nil {
		t.Fatalf("GET /missing.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestServeAsset_중첩디렉터리_404 는 fs.Stat().IsDir() 거절 분기를 겨냥한다.
// M5 실 embed 는 중첩 구조(내부 assets/ 디렉터리)를 갖지만 산출물 유무·해시에
// 비의존해야 하므로(파일 헤더 주석 F5) fixture 로 직접 재현해 "구현이
// 틀리면(예: IsDir 검사 누락) 이 테스트가 실패하는가"를 보장한다.
func TestServeAsset_중첩디렉터리_404(t *testing.T) {
	root := fstest.MapFS{"sub/inner.js": {Data: []byte("x")}}
	srv := httptest.NewServer(serveAsset(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sub")
	if err != nil {
		t.Fatalf("GET /sub: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (디렉터리 요청)", resp.StatusCode)
	}
}

// ---- serveSPA 계약 (fixture) ----

func TestServeSPA_루트의_실재파일_200_no_cache(t *testing.T) {
	root := fstest.MapFS{
		"index.html":  {Data: []byte(`<!doctype html><body id="app-root"></body>`)},
		"favicon.ico": {Data: []byte("fixture-favicon")},
	}
	srv := httptest.NewServer(serveSPA(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/favicon.ico")
	if err != nil {
		t.Fatalf("GET /favicon.ico: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want %q (파일 우선 분기)", cc, "no-cache")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "fixture-favicon" {
		t.Fatalf("body = %q, want fixture 원문 그대로", body)
	}
}

func TestServeSPA_미지경로_index_폴백_200(t *testing.T) {
	root := fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><body id="app-root"></body>`)},
	}
	srv := httptest.NewServer(serveSPA(root))
	defer srv.Close()

	// React Router 새로고침·직접 URL 진입 시나리오: 단일 세그먼트·다단 경로·
	// 완전 미지 경로 전부 index.html 로 폴백해야 한다.
	for _, path := range []string{"/overview", "/explorer/detail/42", "/nonexistent"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("GET %s read body: %v", path, readErr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200 (SPA 폴백)", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s Content-Type = %q, want text/html 접두", path, ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", path, cc)
		}
		if !strings.Contains(string(body), `id="app-root"`) {
			t.Fatalf("GET %s body 에 앱 셸 마크업 미포함:\n%s", path, body)
		}
	}
}

// TestServeSPA_index_부재시_500 은 임베드가 깨져 index.html 자체가 없는
// 상황(에러 경로)을 실제로 태운다 — 에러를 삼키고 200 빈 본문을 내는 회귀를
// 잡는다.
func TestServeSPA_index_부재시_500(t *testing.T) {
	root := fstest.MapFS{}
	srv := httptest.NewServer(serveSPA(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/anything")
	if err != nil {
		t.Fatalf("GET /anything: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (index.html 부재)", resp.StatusCode)
	}
}

// ---- Handler()/FS() 배선 스모크 (실 embed) ----

// TestHandler_루트_200_앱셸마크업_포함 은 실 embed 스모크다. M5 는 React
// SPA(client-rendered) 라 로그인 폼은 런타임에 JS 가 그린다 — 정적 HTTP
// 응답에는 결코 존재할 수 없는 `id="login-form"` 서버측 마크업을 단언하던
// 이전 버전은 실 빌드 산출물(web/index.html · main.tsx 의 `#root` 마운트
// 지점)과 항상 어긋나 실패했다(적대검증 발견). 마운트 지점 `id="root"` 만
// 확인한다.
func TestHandler_루트_200_앱셸마크업_포함(t *testing.T) {
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
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatalf("body 에 앱 셸 마운트 지점(id=\"root\") 미포함:\n%s", body)
	}
}

// TestHandler_index참조_해시자산_전부_200 은 실 embed 스모크다. M5 Vite 빌드는
// 해시 파일명(/assets/index-<hash>.js 등)을 내므로 M4 처럼 고정 이름
// (app.js/style.css)을 단언할 수 없다 — 대신 빌드 산출 index.html 이 실제로
// 참조하는 /assets/* URL 을 전부 추출해 200 인지 확인한다("셸이 참조하는
// 번들은 반드시 서빙된다"는 사용자 시점 계약, 해시값 비의존). 이 계약 검증은
// M4 잔재 StripPrefix 배선(URL /assets/X → FS 루트 X 조회)이 실 산출물
// (FS assets/X)과 어긋나 참조 자산 전부를 404(빈 화면)로 만들던 회귀를 실제로
// 잡아냈다(적대검증 후속 실측: 참조 URL 404 / 미참조 이중접두 URL 만 200).
// Cache-Control=no-cache 단언은 SPA 폴백(no-store)이 자산 경로를 삼켜 200 HTML
// 을 내는 오탐 통과를 막는다. placeholder 상태(참조 0)면 스킵 — 의무 검증
// 순서(pnpm build → go test)에선 항상 실행된다.
func TestHandler_index참조_해시자산_전부_200(t *testing.T) {
	shell, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html 읽기: %v", err)
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(shell), -1)
	if len(refs) == 0 {
		t.Skip("index.html 에 /assets/* 참조 없음(placeholder 상태) — pnpm build 후 실행된다")
	}

	srv := httptest.NewServer(Handler())
	defer srv.Close()

	for _, m := range refs {
		url := m[1]
		resp, err := http.Get(srv.URL + url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("GET %s read body: %v", url, readErr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 (셸이 참조하는 번들 서빙 실패)", url, resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache (자산 경로가 SPA 폴백에 삼켜짐)", url, cc)
		}
		if len(body) == 0 {
			t.Errorf("GET %s 본문이 비었다", url)
		}
	}
}

func TestHandler_assets_존재하지않는파일_404(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/assets/does-not-exist.js")
	if err != nil {
		t.Fatalf("GET /assets/does-not-exist.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandler_assets_디렉터리_404 는 회귀 테스트다: http.FileServerFS 의
// 디렉터리 index.html 자동서빙 기본동작으로 GET /assets/ 가 "그 외 → 404"
// 계약(m4-design.md §2)을 어기고 200+로그인 마크업을 새 나간 적이 있다
// (적대검증 발견) — 재발 방지. SPA 폴백 도입(M5) 후에도 /assets/ 서브트리는
// 폴백 대상이 아니라는 계약을 재확인한다.
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

// TestHandler_미등록경로_SPA_index_폴백_200 은 M5 의 핵심 변경을 실 embed 로
// 검증한다: m4 시절엔 404 였던 미등록 경로가 이제는 index.html 로 폴백해야
// React Router 새로고침·직접 URL 진입이 깨지지 않는다.
func TestHandler_미등록경로_SPA_index_폴백_200(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	for _, path := range []string{"/nonexistent", "/overview", "/explorer/foo/bar"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("GET %s read body: %v", path, readErr)
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200 (SPA 폴백)", path, resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", path, cc)
		}
		if !strings.Contains(string(body), `id="root"`) {
			t.Fatalf("GET %s body 에 앱 셸 마운트 지점(id=\"root\") 미포함:\n%s", path, body)
		}
	}
}

// TestFS_index_html_존재 는 go:embed 최소 불변식을 고정한다: SPA 폴백의
// 원천인 index.html 은 placeholder(standalone go build)든 Vite 산출이든 항상
// 존재해야 한다. M4 의 고정 3자산(app.js/style.css 포함) 단언은 해시 파일명
// 체계로 무효 — 해시 자산 서빙 검증은 TestHandler_index참조_해시자산_전부_200.
func TestFS_index_html_존재(t *testing.T) {
	info, err := fs.Stat(FS(), "index.html")
	if err != nil {
		t.Fatalf("FS() 에 index.html 부재: %v", err)
	}
	if info.IsDir() {
		t.Fatal("FS() 의 index.html 가 디렉터리 — 파일이어야 한다")
	}
}
