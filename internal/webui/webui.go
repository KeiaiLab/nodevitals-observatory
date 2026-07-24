// Package webui 는 nodevitals-observatory 관제 콘솔의 빌드 산출 정적 자산을
// embed.FS 로 담아 서빙한다. 데이터는 전부 인증 뒤 /api/v1/* 가 낸다 — 이
// 패키지는 마크업·번들만 서빙한다. M5: React 콘솔(web/, Vite 빌드)이
// 클라이언트 라우팅(React Router)을 쓰므로, `/assets/*` 는 정적 자산 전용으로
// 서빙하고 그 외 비-API 경로는 index.html 로 폴백한다(m5-design.md §2 D2) —
// 새로고침·직접 URL 진입(/overview, /explorer 등)에서도 클라이언트 라우터가
// 문서를 받도록 보장한다.
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

// securityHeaders 는 콘솔 응답에 심층방어 헤더를 붙인다. CSP 로 self 외
// 스크립트 실행을 원천 차단하고(외부 CDN 0 과 정합) 클릭재킹·MIME 스니핑을
// 막는다(m4-design.md 보안 적대검토 F-SEC-6 반영). shadcn/Recharts 도입 후
// 인라인 style 주입이 실제로 필요해지면(m5-design.md D3) 그 시점에 CSP 를
// 갱신한다 — 아직 그 컴포넌트가 없는 이 변경 범위에서는 완화하지 않는다.
func securityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'self'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
}

// Handler 는 콘솔 정적 자산 라우팅을 구성한다(m5-design.md §2 D2):
//
//	GET /assets/*  → 실재하는 일반 파일만 정적 서빙(Cache-Control: no-cache).
//	                 부재·디렉터리 요청은 404 — 자산 경로는 항상 실재해야 한다
//	                 (SPA 폴백 없음).
//	GET / (그 외)  → 1) 경로가 FS 루트의 실재하는 일반 파일이면 그 파일
//	                    (no-cache, 예: 향후 public/ 산출물인 favicon 등)
//	                 2) 아니면 index.html 200(SPA 폴백, no-store) — React
//	                    Router 새로고침·직접 URL 진입(/overview, /explorer 등)
//	                    이 깨지지 않게 클라이언트 라우팅에 문서를 넘긴다.
//
// apiserver 쪽 배선은 무변경이다: /healthz·/api/v1/* 같은 구체 패턴이
// http.ServeMux 우선순위로 이 catch-all 보다 먼저 매치되므로(server.go:51
// 주석 계약), 미인증 GET /api/v1/query 는 여전히 401(JSON)이지 index.html 이
// 아니다 — webui 는 자신에게 실제로 도달한 요청만 SPA 폴백한다.
func Handler() http.Handler {
	root := FS()

	mux := http.NewServeMux()

	// StripPrefix 없이 전체 경로로 마운트한다: Vite 산출은 outDir 안의 내부
	// assets/ 디렉터리(FS 경로 assets/<name>-<hash>.js)에 놓이므로 URL
	// /assets/X ↔ FS assets/X 가 1:1 이다(vite.config.ts base '/'+assetsDir
	// 'assets' 계약). M4 평면 구조 시절의 StripPrefix("/assets") 잔재는 URL
	// /assets/X 를 FS 루트 X 로 조회해 실 해시 자산 전부를 404(빈 화면)로
	// 만들었다 — TestHandler_index참조_해시자산_전부_200 이 이 회귀를 잡는다.
	mux.Handle("GET /assets/", withNoCache(serveAsset(root)))
	mux.HandleFunc("GET /", serveSPA(root)) // 서브트리 catch-all — /overview,/map,/explorer,/login 전부 여기로

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

// serveSPA 는 파일 우선 → index.html 폴백 catch-all 핸들러다(m5-design.md §2
// D2). "/assets/" 서브트리는 Handler 등록에서 이미 우선 소비되므로 여기
// 도달하는 요청은 그 밖의 전부다: 경로가 FS 루트의 실재하는 일반 파일(디렉터리
// 아님, 예: 향후 public/ 산출물인 favicon 등)이면 그 파일을 no-cache 로,
// 아니면 index.html 을 200 으로 낸다(SPA 폴백, no-store) — React Router 의
// 클라이언트 라우팅이 새로고침·직접 URL 진입에서도 문서를 받도록 보장한다.
func serveSPA(root fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(root)
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if info, err := fs.Stat(root, name); err == nil && !info.IsDir() {
				withNoCache(fileServer).ServeHTTP(w, r)
				return
			}
		}

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
