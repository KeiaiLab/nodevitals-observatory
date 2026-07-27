// M6-데모: GPU 플릿 데모 모드 API. /api/v1/demo/status 는 비인증(불리언 1개만
// 노출 — NetBird-gated 내부 도메인 전제에서 수용), 나머지는 기존 auth
// 미들웨어 뒤에 선다. 데모 엔진이 없으면(실서비스 인스턴스) status 만 등록되고
// 나머지 경로는 기존 handleUnknownAPI 가 JSON 404 를 낸다 — off 분기 코드 0.
package apiserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/KeiaiLab/nodevitals-observatory/internal/demo"
)

// ServerOption 은 NewServer 의 선택 설정이다 — 기존 호출부(main·테스트)를
// 바꾸지 않고 데모 배선을 얹기 위한 variadic 확장점.
type ServerOption func(*serverConfig)

type serverConfig struct {
	demo *demo.Engine
	// openAccess 는 보호 라우트의 인증을 해제한다 — public demo 전용.
	// 합성 데이터만 서빙하는 데모 인스턴스에서 로그인 장벽 없이 URL 접속
	// 즉시 콘솔이 열리게 한다. NewServer 가 demo 엔진 부재 시 무시한다
	// (실서비스에서 실수로 켜지는 경로 차단).
	openAccess bool
}

// WithDemo 는 데모 엔진을 배선한다(nil 이면 무시 — off 와 동일).
func WithDemo(e *demo.Engine) ServerOption {
	return func(c *serverConfig) { c.demo = e }
}

// WithDemoPublic 은 public demo 모드다 — 데모 엔진과 함께일 때만 유효하다.
func WithDemoPublic() ServerOption {
	return func(c *serverConfig) { c.openAccess = true }
}

type demoStatusData struct {
	Enabled bool `json:"enabled"`
	Ready   bool `json:"ready"`
	// Public 은 조회 API 인증이 해제된 public demo 여부다 — 프론트가 로그인
	// 화면·로그아웃 버튼을 건너뛰는 근거.
	Public bool `json:"public"`
}

// handleDemoStatus 는 데모 모드 여부만 알린다. 프론트가 이 값으로 데모 전용
// 화면(격리·검증 콘솔, 시나리오 리모컨)의 노출과 인증 우회를 결정한다.
func handleDemoStatus(e *demo.Engine, public bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		d := demoStatusData{Enabled: e != nil, Public: public}
		if e != nil {
			d.Ready = e.Ready()
		}
		writeSuccess(w, d)
	}
}

// handleDemoState 는 시나리오·알림·감사·드레인·번인 스냅샷을 낸다. 백필 중
// (readyz false)에는 503 — 엔진 뮤텍스를 잡고 수십 초 매달리는 대신 명시적
// 재시도 신호를 준다.
func handleDemoState(e *demo.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !e.Ready() {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "데모 백필 진행 중 — 잠시 후 재시도")
			return
		}
		writeSuccess(w, e.Snapshot())
	}
}

// demoActionBody 는 액션 요청 바디다(전 필드 선택 — 액션별로 쓰는 것만 본다).
type demoActionBody struct {
	UUID          string `json:"uuid"`
	Reason        string `json:"reason"`
	Note          string `json:"note"`
	Mode          string `json:"mode"`
	Phase         string `json:"phase"`
	Profile       string `json:"profile"`
	DurationMin   int    `json:"durationMin"`
	TargetUtilPct int    `json:"targetUtilPct"`
	At            int64  `json:"at"`
}

// params 는 바디를 시나리오 액션 파라미터 맵으로 바꾼다. 0/빈값은 넣지 않아
// "미지정"과 "0 지정"이 구분된다(부분 갱신 허용 계약).
func (b demoActionBody) params() map[string]string {
	p := map[string]string{
		"uuid": b.UUID, "reason": b.Reason, "note": b.Note,
		"mode": b.Mode, "phase": b.Phase, "profile": b.Profile,
	}
	if b.DurationMin > 0 {
		p["durationMin"] = strconv.Itoa(b.DurationMin)
	}
	if b.TargetUtilPct > 0 {
		p["targetUtilPct"] = strconv.Itoa(b.TargetUtilPct)
	}
	if b.At > 0 {
		p["at"] = strconv.FormatInt(b.At, 10)
	}
	return p
}

// maxDemoActionBody 는 액션 바디 상한이다 — auth 로그인 바디 1KiB 상한과 같은
// 취지(비대 바디로 인한 메모리 증폭 차단).
const maxDemoActionBody = 4 << 10

// handleDemoAction 은 POST /api/v1/demo/actions/{action} 을 처리한다.
// 단계 불일치(시연 오조작)는 409 로 낸다 — 서사를 깨는 대신 명시 거절.
func handleDemoAction(e *demo.Engine, actor func() string) http.HandlerFunc {
	allowed := map[string]demo.Action{
		string(demo.ActionApproveIsolation):    demo.ActionApproveIsolation,
		string(demo.ActionStartBurnin):         demo.ActionStartBurnin,
		string(demo.ActionReturnToService):     demo.ActionReturnToService,
		string(demo.ActionReset):               demo.ActionReset,
		string(demo.ActionRegisterIdleReason):  demo.ActionRegisterIdleReason,
		string(demo.ActionReportFalsePositive): demo.ActionReportFalsePositive,
		string(demo.ActionSetMode):             demo.ActionSetMode,
		string(demo.ActionConfigureBurnin):     demo.ActionConfigureBurnin,
		string(demo.ActionJumpPhase):           demo.ActionJumpPhase,
		string(demo.ActionAckAlert):            demo.ActionAckAlert,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !e.Ready() {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "데모 백필 진행 중 — 잠시 후 재시도")
			return
		}
		action, ok := allowed[r.PathValue("action")]
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "알 수 없는 데모 액션: "+r.PathValue("action"))
			return
		}

		var body demoActionBody
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxDemoActionBody+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", "요청 바디 읽기 실패")
			return
		}
		if len(raw) > maxDemoActionBody {
			writeError(w, http.StatusRequestEntityTooLarge, "bad_data", "요청 바디가 너무 크다")
			return
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				writeError(w, http.StatusBadRequest, "bad_data", "요청 바디 JSON 파싱 실패")
				return
			}
		}

		result, err := e.Do(action, actor(), body.params())
		if err != nil {
			// 단계 불일치·대상 부재 등 도메인 거절은 전부 409 — 요청 형식은
			// 유효하나 현재 상태와 충돌한다는 의미가 정확하다.
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeSuccess(w, result)
	}
}
