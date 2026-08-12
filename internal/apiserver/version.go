package apiserver

import (
	"net/http"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/buildinfo"
)

// handleVersion 은 지금 돌고 있는 빌드가 무엇이고 언제 반영됐는지 알린다.
//
// 인증을 걸지 않는다 — 시연 중 "지금 보시는 게 어느 빌드인가" 에 답하는 것이
// 목적이라 로그인 뒤에 숨으면 쓸모가 없다. 노출되는 값은 이미 이미지 태그와
// git 이력으로 공개돼 있는 것뿐이다(비밀 없음).
//
// healthz/readyz 와 같은 급의 운영 엔드포인트라 데모 모드 여부와 무관하게 항상 산다.
func handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w, buildinfo.Get(time.Now()))
}
