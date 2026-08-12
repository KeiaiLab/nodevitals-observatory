// Package buildinfo 는 빌드 시점에 주입된 식별자와 프로세스 기동 시각을 들고 있다.
//
// 왜 별도 패키지인가 — ldflags 의 -X 는 *패키지 수준 string 변수* 에만 꽂힌다.
// main 에 두면 apiserver 가 못 읽고, apiserver 에 두면 main 이 로그로 못 찍는다.
// 양쪽이 같은 값을 보게 하려면 둘 다 import 할 수 있는 자리에 있어야 한다.
//
// 값의 성격이 둘로 갈린다:
//   - Version/Commit/BuiltAt — *빌드* 시점에 굳는다(ldflags 주입, 이미지에 박힘)
//   - StartedAt              — *기동* 시점에 정해진다(런타임). 파드가 새 이미지로
//     떠야만 바뀌므로 "이 빌드가 실제로 라이브에 반영된 시각" 과 같다.
//
// 주입이 없으면(로컬 go run·go test) 전부 폴백값이라 "dev" 로 보인다 — 빌드
// 파이프라인을 거치지 않았다는 사실 자체가 드러나야 하므로 빈 문자열로 두지 않는다.
package buildinfo

import "time"

// 아래 3개는 ldflags 로 덮어쓴다:
//
//	-X github.com/KeiaiLab/nodevitals-observatory/internal/buildinfo.Version=0.13.6
var (
	// Version 은 차트 appVersion 이자 이미지 태그다(단일 출처 — deploy/chart/Chart.yaml).
	Version = "dev"
	// Commit 은 빌드된 소스의 git SHA(짧은 형식).
	Commit = "unknown"
	// BuiltAt 은 이미지 빌드 시각(RFC3339 UTC). 미주입이면 빈 문자열.
	BuiltAt = ""
)

// startedAt 은 프로세스가 뜬 시각 — 패키지 초기화 시점에 한 번 굳는다.
// 테스트에서 고정할 수 있도록 함수 경유로만 노출한다.
var startedAt = time.Now().UTC()

// Snapshot 은 /api/v1/version 응답이자 기동 로그의 원본이다.
type Snapshot struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// BuiltAt 은 미주입 시 생략된다 — "" 를 그대로 내보내면 프론트가
	// 빈 날짜를 렌더하려다 Invalid Date 를 찍는다.
	BuiltAt string `json:"builtAt,omitempty"`
	// StartedAt 은 이 빌드가 실제로 서비스에 반영된 시각이다.
	StartedAt string `json:"startedAt"`
	// UptimeSeconds 는 프론트가 시계 오차 없이 경과를 표시할 수 있게 서버가 직접 센 값이다.
	UptimeSeconds int64 `json:"uptimeSeconds"`
}

// Get 은 현재 빌드·기동 정보를 반환한다. now 를 주면 테스트에서 고정 가능하다.
func Get(now time.Time) Snapshot {
	return Snapshot{
		Version:       Version,
		Commit:        Commit,
		BuiltAt:       BuiltAt,
		StartedAt:     startedAt.Format(time.RFC3339),
		UptimeSeconds: int64(now.UTC().Sub(startedAt).Seconds()),
	}
}

// StartedAt 은 기동 시각을 그대로 돌려준다(로그·테스트용).
func StartedAt() time.Time { return startedAt }
