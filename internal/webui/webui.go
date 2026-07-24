// Package webui 는 nodevitals-observatory 관제 콘솔의 정적 자산(로그인 +
// Overview 화면, 순정 HTML/JS/CSS)을 embed.FS 로 담아 서빙한다. 데이터는
// 전부 인증 뒤 /api/v1/* 가 낸다(m4-design.md §0 D1) — 이 패키지는 마크업만
// 서빙하고, 인증 판정은 브라우저에서 app.js 가 /api/v1/query 응답 코드로
// 스스로 수행한다. 빌드 체인 없음(React/node 금지) — 크래프트된 정적 자산.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assets embed.FS

// FS 는 assets 디렉터리를 루트로 한 서브트리를 반환한다(테스트·외부 소비용).
// go:embed 는 빌드 시점에 고정되므로 fs.Sub 실패는 임베드 자체가 깨졌다는
// 뜻이라 panic 으로 즉시 드러낸다 — 런타임에 조용히 넘어가면 안 되는 불변식.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic("webui: assets 서브트리 구성 실패: " + err.Error())
	}
	return sub
}

// securityHeaders 는 콘솔 응답에 심층방어 헤더를 붙인다. XSS 는 app.js 가
// textContent/createElement 전용 렌더로 1차 방어하지만, CSP 로 self 외
// 스크립트 실행을 원천 차단하고(외부 CDN 0 과 정합) 클릭재킹·MIME 스니핑을
// 막는다(m4-design.md 보안 적대검토 F-SEC-6 반영).
func securityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'self'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
}

// Handler 는 콘솔 정적 자산 라우팅을 구성한다:
//
//	GET /         → index.html (Cache-Control: no-store — 배포 즉시 반영)
//	GET /assets/* → app.js / style.css (Cache-Control: no-cache)
//	그 외         → 404 (SPA 폴백 없음 — 뷰 토글은 JS 안에서만 일어난다)
func Handler() http.Handler {
	root := FS()

	mux := http.NewServeMux()

	// "/{$}" 는 정확히 "/" 만 매치하는 Go 1.22+ 종료 와일드카드다 — 접미사
	// 없는 "/" 패턴은 서브트리(모든 미매치 경로)로 흡수돼버려 "SPA 폴백
	// 없음(그 외 GET = 404)" 계약을 못 지킨다.
	mux.HandleFunc("GET /{$}", serveIndex(root))
	mux.Handle("GET /assets/", http.StripPrefix("/assets", withNoCache(serveAsset(root))))

	return mux
}

// serveAsset 는 assets/ 서브트리 중 실재하는 일반 파일만 http.FileServerFS 로
// 위임한다. http.FileServerFS 는 디렉터리 요청(GET /assets/ 가 StripPrefix 뒤
// 빈 경로 "/" 가 되는 경우 포함)에 그 디렉터리의 index.html 을 자동으로
// 200 서빙하는 기본 동작을 갖는데, 이는 "GET /assets/* → app.js/style.css,
// 그 외 404"(m4-design.md §2) 계약을 깨고 로그인 마크업을 /assets/ 경로로
// 새 나가는 회귀를 일으킨다. 요청 경로가 실재하는 일반 파일(디렉터리 아님)일
// 때만 위임하고, 그 외(빈 경로·디렉터리·부재)는 명시적으로 404 처리한다.
func serveAsset(root fs.FS) http.Handler {
	fileServer := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(root, name)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndex 는 앱 셸(index.html)을 낸다. 로그인/Overview 두 뷰가 모두 이
// 한 문서 안에 있고 app.js 가 hidden 속성으로 토글한다(D1) — 서버측
// 템플릿·조건 렌더는 없다.
func serveIndex(root fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		securityHeaders(w.Header())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}

// withNoCache 는 정적 자산 응답에 캐시 무효화 + 심층방어 헤더를 얹는다.
// 0.2.x 는 버전 해시 체인이 없는 단일 이미지 배포라 no-cache 로 매 배포
// 반영을 보장한다(§2 Simplicity — 해시 파일명 체인은 M5 shadcn 빌드 도입 시).
func withNoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w.Header())
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
