package demo

import (
	"fmt"
	"math"
	"strconv"
)

// Phase 는 victim GPU 생애주기 단계다. 자동 타임아웃 전이(무인 루프)와 운영자
// 액션 전이(즉시)가 공존한다 — 시연자가 아무것도 안 눌러도 서사가 돌고,
// 누르면 그 즉시 반응한다.
type Phase string

const (
	PhaseNormal           Phase = "normal"
	PhaseDegrading        Phase = "degrading"
	PhaseAwaitingApproval Phase = "awaiting-approval"
	PhaseDraining         Phase = "draining"
	PhaseReplacing        Phase = "replacing"
	PhaseBurnin1          Phase = "burnin-1"
	PhaseBurninFailed     Phase = "burnin-failed"
	PhaseBurnin2          Phase = "burnin-2"
	PhaseReadyToReturn    Phase = "ready-to-return"
	PhaseReturned         Phase = "returned"
)

// phaseOrder 와 base duration(ms). 합계 ≈ 35분/사이클, TimeScale 로 나눠 배속.
var phaseOrder = []Phase{
	PhaseNormal, PhaseDegrading, PhaseAwaitingApproval, PhaseDraining,
	PhaseReplacing, PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2,
	PhaseReadyToReturn, PhaseReturned,
}

var phaseBaseDurMS = map[Phase]int64{
	PhaseNormal:           4 * 60_000,
	PhaseDegrading:        5 * 60_000,
	PhaseAwaitingApproval: 3 * 60_000,
	PhaseDraining:         4 * 60_000,
	PhaseReplacing:        3 * 60_000,
	PhaseBurnin1:          5 * 60_000,
	PhaseBurninFailed:     2 * 60_000,
	PhaseBurnin2:          5 * 60_000,
	PhaseReadyToReturn:    2 * 60_000,
	PhaseReturned:         2 * 60_000,
}

// Action 은 운영자 액션이다.
type Action string

const (
	ActionApproveIsolation    Action = "approve-isolation"
	ActionStartBurnin         Action = "start-burnin"
	ActionReturnToService     Action = "return-to-service"
	ActionReset               Action = "reset"
	ActionRegisterIdleReason  Action = "register-idle-reason"
	ActionReportFalsePositive Action = "report-false-positive"
	ActionSetMode             Action = "set-mode"
	ActionConfigureBurnin     Action = "configure-burnin"
	ActionJumpPhase           Action = "jump-phase"
	ActionAckAlert            Action = "ack-alert"
	ActionReseed              Action = "reseed"
)

// RemediationMode 는 자동복구 정책이다 — 화면의 모드 셀렉터가 이 값을 바꾸고,
// 시나리오 자동 전이가 실제로 달라진다(표시용이 아니다).
type RemediationMode string

const (
	// ModeObserve 는 탐지만 하고 조치하지 않는다 — 승인 대기에서 무한 정지.
	ModeObserve RemediationMode = "observe"
	// ModeApprove 는 운영자 승인 기반(기본) — 대기 타임아웃 시 자동 승인.
	ModeApprove RemediationMode = "approve"
	// ModeLimitedAuto 는 가드레일 내 자동 — 승인 단계를 건너뛰고 즉시 격리.
	ModeLimitedAuto RemediationMode = "limited-auto"
)

// 번인 파라미터 범위 — 화면 슬라이더와 동일 계약.
const (
	burninMinMinutes = 30
	burninMaxMinutes = 180
	burninRefMinutes = 120 // 이 값일 때 번인 단계가 기준 duration 을 쓴다
	burninMinUtil    = 80
	burninMaxUtil    = 100
)

// AlertEvent 는 알림 타임라인 항목이다(/demo/state 로 노출).
type AlertEvent struct {
	// ID 는 알림의 고유 식별자다 — 확인 처리 키. At(타임스탬프)로 식별하면
	// 같은 틱에 발생한 알림들이 한 번의 확인으로 모두 처리된다(실측 결함).
	ID       int64  `json:"id"`
	At       int64  `json:"at"`
	Severity string `json:"severity"` // info | warning | major | critical
	Code     string `json:"code"`
	Title    string `json:"title"`
	Instance string `json:"instance,omitempty"`
	Device   string `json:"device,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// Acked 는 운영자가 확인 처리했는지다 — 서버가 기억해야 새로고침 후에도
	// 유지된다(로컬 표시만 바꾸면 "눌러도 안 되는 버튼"이 된다).
	Acked bool `json:"acked,omitempty"`
}

// AuditEntry 는 감사·증적 항목이다 — 수행 주체·근거·결과를 남긴다(운영 조치
// 감사 서사의 최소 표면).
type AuditEntry struct {
	At        int64  `json:"at"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	PhaseFrom string `json:"phaseFrom,omitempty"`
	PhaseTo   string `json:"phaseTo,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
	Result    string `json:"result"`
}

const (
	maxAlerts       = 100
	maxAudit        = 200
	drainInflight   = 1240 // draining 진입 시점 in-flight 추론 요청 수
	drainPods       = 24   // 퇴거 대상 파드 수
	burninTargetPct = 95
)

// Scenario 는 victim 생애주기 + 상시 이상 개체를 관리한다. 동시성 보호는
// Engine.mu 가 담당한다(이 타입 자체는 잠그지 않는다).
type Scenario struct {
	fleet *Fleet
	cfg   Config

	victim  *GPU
	baseCSP string

	phase      Phase
	phaseStart int64
	cycle      int
	fired      map[string]bool // 단계 내 1회성 알림 발화 기록(단계 진입 시 리셋)

	// 운영자 설정 — 화면에서 바꾸면 시나리오 동작이 실제로 달라진다.
	mode           RemediationMode
	burninProfile_ string
	burninMinutes  int
	burninUtilPct  int

	// ambientTick 은 마지막으로 앰비언트 알림을 낸 15s 틱이다(중복 발화 방지).
	ambientTick int64
	// alertSeq 는 알림 ID 발번기, ackedAlerts 는 확인 처리한 알림 ID 집합이다.
	alertSeq    int64
	ackedAlerts map[int64]bool
	// eventTick 은 마지막 K8s 이벤트 발화 틱, events 는 이벤트 링버퍼다.
	eventTick int64
	events    []ClusterEvent

	// 상시 이상 — 사이클과 무관하게 유지되는 배경 결함(장애 2·온도 3·수집중단 1).
	faultGPUs []*GPU
	hotGPUs   []*GPU
	// hotRack 은 온도 이상이 몰린 랙이다 — 랙 단위 냉각 이상 서사의 근거.
	hotRack     string
	missingNode *Node

	alerts []AlertEvent
	audit  []AuditEntry

	idleReasons    map[string]string // uuid → 미사용 사유
	falsePositives map[string]string // uuid → 피드백 메모
}

// newScenario 는 결정론으로 victim·이상 개체를 선정한다. victim 은 기준(native)
// CSP 의 고부하 할당 GPU — "멀쩡히 서빙 중이던 GPU 가 무음 열화한다"는 서사
// 전제다. 이상 개체는 mock CSP 쪽에 흩어 배치해 히트맵에 지리적 다양성을 준다.
func newScenario(fleet *Fleet, cfg Config, startMS int64) *Scenario {
	s := &Scenario{
		fleet:          fleet,
		cfg:            cfg,
		phase:          PhaseNormal,
		phaseStart:     startMS,
		cycle:          1,
		fired:          map[string]bool{},
		idleReasons:    map[string]string{},
		falsePositives: map[string]string{},
		mode:           ModeApprove,
		burninMinutes:  burninRefMinutes,
		burninUtilPct:  burninTargetPct,
		ackedAlerts:    map[int64]bool{},
	}
	s.baseCSP = fleet.Specs[0].ID

	pickFrom := func(filter func(*GPU) bool, salt string, avoid map[string]bool) *GPU {
		var candidates []*GPU
		for _, g := range fleet.GPUs {
			if filter(g) && !avoid[g.UUID] {
				candidates = append(candidates, g)
			}
		}
		if len(candidates) == 0 {
			return fleet.GPUs[0]
		}
		return candidates[int(fnvHash(cfg.Seed, "pick", salt)%uint64(len(candidates)))]
	}

	taken := map[string]bool{}
	s.victim = pickFrom(func(g *GPU) bool {
		return g.CSP == s.baseCSP && g.Allocated && g.band == 3
	}, "victim", taken)
	taken[s.victim.UUID] = true

	for i := 0; i < 2; i++ {
		g := pickFrom(func(g *GPU) bool { return g.CSP != s.baseCSP && g.Allocated }, fmt.Sprintf("fault%d", i), taken)
		taken[g.UUID] = true
		s.faultGPUs = append(s.faultGPUs, g)
	}
	// 온도 이상 3장은 **같은 랙**에서 고른다 — 실제 냉각 이상은 개별 GPU 가
	// 아니라 랙(냉기 통로·팬) 단위로 나타난다. 무작위로 흩뿌리면 운영자가
	// 보는 순간 만든 데이터로 읽힌다.
	hotSeed := pickFrom(func(g *GPU) bool { return g.Allocated && g.band >= 2 }, "hot0", taken)
	s.hotRack = hotSeed.Rack
	taken[hotSeed.UUID] = true
	s.hotGPUs = append(s.hotGPUs, hotSeed)
	for _, g := range fleet.GPUs {
		if len(s.hotGPUs) >= 3 {
			break
		}
		if g.Rack == s.hotRack && g.Allocated && !taken[g.UUID] {
			taken[g.UUID] = true
			s.hotGPUs = append(s.hotGPUs, g)
		}
	}
	// 수집 중단 노드 1대 — victim·이상 GPU 가 없는 노드에서 고른다.
	for _, n := range fleet.Nodes {
		clean := n.CSP != s.baseCSP
		for _, g := range n.GPUs {
			if taken[g.UUID] {
				clean = false
				break
			}
		}
		if clean {
			s.missingNode = n
			break
		}
	}

	s.seedInitialEvents(startMS)
	return s
}

// seedInitialEvents 는 부팅 직후 알림/감사 링이 비어 보이지 않게 상시 이상을
// 알림으로 등재한다.
func (s *Scenario) seedInitialEvents(tMS int64) {
	for _, g := range s.faultGPUs {
		s.pushAlert(AlertEvent{At: tMS, Severity: "critical", Code: "ECC_UNCORRECTED",
			Title: "비정정 ECC 오류 — 카드 오프라인", Instance: g.Instance, Device: g.Device,
			Detail: fmt.Sprintf("%s · 비정정 ECC 6건, 워크로드 재배치 필요", g.Model)})
	}
	for _, g := range s.hotGPUs {
		s.pushAlert(AlertEvent{At: tMS, Severity: "major", Code: "THERMAL",
			Title: "온도 임계 초과 (>80°C)", Instance: g.Instance, Device: g.Device,
			Detail: fmt.Sprintf("랙 %s · Thermal Throttle 지속 — 랙 냉각 점검 대상", g.Rack)})
	}
	if s.missingNode != nil {
		s.pushAlert(AlertEvent{At: tMS, Severity: "warning", Code: "AGENT_MISSING",
			Title: "에이전트 수집 중단 (Agent Missing)", Instance: s.missingNode.Instance,
			Detail: "노드 텔레메트리 미수신 — K8s 는 Ready 로 보고 중"})
	}
	s.pushAudit(AuditEntry{At: tMS, Actor: "system", Action: "boot",
		Result: "데모 시나리오 초기화 — 관찰 모드 진입", PhaseTo: string(PhaseNormal)})
	s.seedEvents(tMS)
}

func (s *Scenario) pushAlert(a AlertEvent) {
	s.alertSeq++
	a.ID = s.alertSeq
	s.alerts = append(s.alerts, a)
	if len(s.alerts) > maxAlerts {
		s.alerts = s.alerts[len(s.alerts)-maxAlerts:]
	}
}

func (s *Scenario) pushAudit(a AuditEntry) {
	s.audit = append(s.audit, a)
	if len(s.audit) > maxAudit {
		s.audit = s.audit[len(s.audit)-maxAudit:]
	}
}

// scaledDur 는 단계 duration 을 배속으로 나눈 실효값이다. 번인 단계는 운영자가
// 설정한 지속시간에 비례한다 — 슬라이더가 실제 시연 길이를 바꾼다.
func (s *Scenario) scaledDur(p Phase) int64 {
	base := phaseBaseDurMS[p]
	if p == PhaseBurnin1 || p == PhaseBurnin2 {
		mins := s.burninMinutes
		if mins <= 0 {
			mins = burninRefMinutes
		}
		base = base * int64(mins) / burninRefMinutes
	}
	ts := s.cfg.TimeScale
	if ts <= 0 {
		ts = 1
	}
	d := int64(float64(base) / ts)
	if d < 15_000 {
		d = 15_000 // emit 1틱보다 짧아지면 단계가 화면에 잡히지 않는다
	}
	return d
}

// progress 는 현 단계 진행률 [0,1] 이다.
func (s *Scenario) progress(tMS int64) float64 {
	d := s.scaledDur(s.phase)
	if d <= 0 {
		return 1
	}
	p := float64(tMS-s.phaseStart) / float64(d)
	return clamp(p, 0, 1)
}

// Advance 는 fast 틱마다 호출된다 — 타임아웃 자동 전이 + 단계 내 1회성 알림 +
// 앰비언트 이벤트(플릿 전역 일상 알림).
func (s *Scenario) Advance(tMS int64) {
	// 단계 내 진행 알림 (degrading 의 XID·throttle 등장 등)
	s.firePhaseAlerts(tMS)
	s.fireAmbient(tMS)
	s.fireEvents(tMS)

	// 제한 자동 모드는 승인 단계를 건너뛴다 — degrading 종료 즉시 격리.
	if s.mode == ModeLimitedAuto && s.phase == PhaseDegrading &&
		tMS-s.phaseStart >= s.scaledDur(s.phase) {
		s.transition(PhaseDraining, "policy", "제한 자동 모드 — 가드레일 통과로 승인 생략", tMS)
		return
	}

	if tMS-s.phaseStart < s.scaledDur(s.phase) {
		return
	}
	switch s.phase {
	case PhaseAwaitingApproval:
		// 관찰 모드는 조치하지 않는다 — 운영자가 승인할 때까지 대기 유지.
		if s.mode == ModeObserve {
			return
		}
		// 자동 승인 — 무인 루프 보장. 감사에 actor=auto 로 남긴다.
		s.transition(PhaseDraining, "auto", "승인 대기 타임아웃 — 정책상 자동 승인", tMS)
	case PhaseBurninFailed:
		s.transition(PhaseBurnin2, "auto", "재번인 자동 시작", tMS)
	case PhaseReadyToReturn:
		s.transition(PhaseReturned, "auto", "검증 통과 — 자동 재투입", tMS)
	case PhaseReturned:
		s.cycle++
		s.transition(PhaseNormal, "system", fmt.Sprintf("사이클 %d 시작", s.cycle), tMS)
	default:
		if next := s.nextPhase(); next != "" {
			s.transition(next, "system", "", tMS)
		}
	}
}

func (s *Scenario) nextPhase() Phase {
	for i, p := range phaseOrder {
		if p == s.phase && i+1 < len(phaseOrder) {
			return phaseOrder[i+1]
		}
	}
	return ""
}

// transition 은 단계를 바꾸고 알림·감사·부수효과를 기록한다.
func (s *Scenario) transition(to Phase, actor, reason string, tMS int64) {
	from := s.phase
	s.phase = to
	s.phaseStart = tMS
	s.fired = map[string]bool{}

	target := fmt.Sprintf("%s/%s", s.victim.Instance, s.victim.Device)
	if reason == "" {
		// 사유 없는 자동 전이도 감사 행의 결과 칸이 비지 않게 한다 —
		// 빈 칸은 "기록 누락"으로 오독된다.
		reason = fmt.Sprintf("단계 전이: %s → %s", phaseLabels[from], phaseLabels[to])
	}
	entry := AuditEntry{At: tMS, Actor: actor, Action: "phase-transition", Target: target,
		PhaseFrom: string(from), PhaseTo: string(to), Result: reason}

	switch to {
	case PhaseDraining:
		s.victim.Allocated = false
		entry.Action = "isolate"
		entry.Evidence = fmt.Sprintf("Health %d · 대체 용량 확보 · PDB 위반 없음", s.VictimHealth(tMS).Score)
		if entry.Result == "" {
			entry.Result = "Cordon 적용 — Graceful Drain 시작"
		}
		s.pushAlert(AlertEvent{At: tMS, Severity: "warning", Code: "CORDON",
			Title: "노드 Cordon — 신규 워크로드 차단", Instance: s.victim.Instance, Device: s.victim.Device,
			Detail: "격리 승인 완료 · in-flight 추론 보호 드레인 진입"})
	case PhaseReplacing:
		entry.Action = "replace-request"
		entry.Result = "CSP 노드풀 교체 트리거 — 비동기 추적 시작"
		s.pushAlert(AlertEvent{At: tMS, Severity: "info", Code: "REPLACE",
			Title: "노드 교체 요청 (CSP API)", Instance: s.victim.Instance,
			Detail: "드롭된 추론 요청 0건 — 드레인 완료 후 교체"})
	case PhaseBurnin1, PhaseBurnin2:
		if to == PhaseBurnin1 {
			// 교체 완료 — 새 개체(uuid 교체)로 번인을 시작한다.
			s.fleet.ReplaceUUID(s.victim, s.cycle)
		}
		entry.Action = "burnin-start"
		entry.Evidence = fmt.Sprintf("프로파일 %s · 목표 부하 %d%%", s.burninProfile(), burninTargetPct)
		entry.Result = "K8s Job 번인 실행"
	case PhaseBurninFailed:
		entry.Action = "burnin-verdict"
		entry.Result = "재검증 필요 (Uncordon 보류) — 번인 중 정정 ECC 증가"
		s.pushAlert(AlertEvent{At: tMS, Severity: "major", Code: "BURNIN_FAIL",
			Title: "번인 1차 실패 — 재검증 필요", Instance: s.victim.Instance, Device: s.victim.Device,
			Detail: "정정 ECC 3건 증가 · Health 75 · Uncordon 보류"})
	case PhaseReadyToReturn:
		entry.Action = "burnin-verdict"
		entry.Result = fmt.Sprintf("검증 통과 — Health %d · 재투입 대기", s.VictimHealth(tMS).Score)
	case PhaseReturned:
		s.victim.Allocated = true
		entry.Action = "uncordon"
		entry.Result = "검증 통과 노드만 Uncordon — 운영 풀 재투입"
		s.pushAlert(AlertEvent{At: tMS, Severity: "info", Code: "RETURNED",
			Title: "운영 풀 재투입 완료", Instance: s.victim.Instance, Device: s.victim.Device,
			Detail: "번인·검증 통과 (Health 96)"})
	case PhaseNormal:
		s.victim.Allocated = true
	}
	s.pushAudit(entry)
}

// phaseLabels 는 단계의 사람용 이름이다(리모컨 단계 점프 목록·화면 공용).
var phaseLabels = map[Phase]string{
	PhaseNormal:           "정상",
	PhaseDegrading:        "무음 열화 진행",
	PhaseAwaitingApproval: "격리 승인 대기",
	PhaseDraining:         "Graceful Drain",
	PhaseReplacing:        "노드 교체 중",
	PhaseBurnin1:          "번인 검증 1차",
	PhaseBurninFailed:     "번인 실패 — 재검증 필요",
	PhaseBurnin2:          "번인 검증 2차",
	PhaseReadyToReturn:    "재투입 대기",
	PhaseReturned:         "운영 풀 복귀",
}

// modeLabels 는 복구 모드의 사람용 이름이다(감사 로그·응답 메시지 공용).
var modeLabels = map[RemediationMode]string{
	ModeObserve:     "관찰",
	ModeApprove:     "운영자 승인",
	ModeLimitedAuto: "제한 자동",
}

// burninProfile 은 번인 프로파일명이다 — 운영자가 지정했으면 그 값, 아니면
// GPU 모델 기본값(B200 은 전용 룰셋 프로파일).
func (s *Scenario) burninProfile() string {
	if s.burninProfile_ != "" {
		return s.burninProfile_
	}
	if s.victim.Model == "NVIDIA B200" {
		return "B200-Blackwell-v1"
	}
	return "High-Intensity-CUDA-v1"
}

// maintenanceLocks 는 CSP 정비 예정으로 자동복구를 보류 중인 노드를 낸다.
// 대상은 실제 플릿에서 결정론으로 고르므로 화면의 다른 목록과 같은 개체를
// 가리킨다 — 예시용 가짜 노드명을 쓰면 "한 데이터셋" 정합이 깨진다.
func (s *Scenario) maintenanceLocks() []MaintenanceLock {
	if len(s.fleet.Nodes) == 0 {
		return nil
	}
	n := s.fleet.Nodes[int(fnvHash(s.cfg.Seed, "maintenance")%uint64(len(s.fleet.Nodes)))]
	if n.Instance == s.victim.Instance {
		return nil // victim 과 겹치면 서사가 헷갈린다 — 이 사이클은 표시하지 않는다
	}
	return []MaintenanceLock{{
		Instance: n.Instance, CSP: n.CSP,
		Window: "오늘 22:00~24:00", Reason: "CSP 정비 예정 — 노드풀 펌웨어 업데이트",
	}}
}

// isolated 는 victim 이 현재 격리 상태인지다(cordon~재투입 직전).
func (s *Scenario) isolated() bool {
	switch s.phase {
	case PhaseDraining, PhaseReplacing, PhaseBurnin1, PhaseBurninFailed,
		PhaseBurnin2, PhaseReadyToReturn:
		return true
	}
	return false
}

// phaseIndexOfStrict 는 미지의 단계에 -1 을 낸다(jump-phase 검증용).
func phaseIndexOfStrict(p Phase) int {
	for i, x := range phaseOrder {
		if x == p {
			return i
		}
	}
	return -1
}

// firePhaseAlerts 는 단계 진행률에 따른 1회성 알림을 발화한다.
func (s *Scenario) firePhaseAlerts(tMS int64) {
	p := s.progress(tMS)
	fire := func(key string, f func()) {
		if !s.fired[key] {
			s.fired[key] = true
			f()
		}
	}
	v := s.victim
	switch s.phase {
	case PhaseDegrading:
		if p >= 0.15 {
			fire("temp", func() {
				s.pushAlert(AlertEvent{At: tMS, Severity: "major", Code: "THERMAL",
					Title: "온도 상승 추세 감지", Instance: v.Instance, Device: v.Device,
					Detail: "임계 80°C 접근 — 상태 '주의' 전환"})
			})
		}
		if p >= 0.4 {
			fire("ecc", func() {
				s.pushAlert(AlertEvent{At: tMS, Severity: "major", Code: "ECC_RATE",
					Title: "정정 ECC 누적 증가", Instance: v.Instance, Device: v.Device,
					Detail: "1h 내 반복 증가 — 무음 열화 의심"})
			})
		}
		if p >= 0.6 {
			fire("xid63", func() {
				s.pushAlert(AlertEvent{At: tMS, Severity: "critical", Code: "XID_63",
					Title: "XID 63 — Row Remap 이벤트", Instance: v.Instance, Device: v.Device,
					Detail: "ECC page retirement 발생 — 격리 검토 필요"})
			})
		}
		if p >= 0.8 {
			fire("nvlink", func() {
				s.pushAlert(AlertEvent{At: tMS, Severity: "critical", Code: "NVLINK",
					Title: "NVLink 링크 오류 (Link 2)", Instance: v.Instance, Device: v.Device,
					Detail: "상태 '장애 의심' — K8s 는 여전히 Ready"})
			})
		}
	case PhaseDraining:
		if p >= 0.95 {
			fire("drained", func() {
				s.pushAlert(AlertEvent{At: tMS, Severity: "info", Code: "DRAINED",
					Title: "Graceful Drain 완료", Instance: v.Instance, Device: v.Device,
					Detail: fmt.Sprintf("파드 %d개 퇴거 · 드롭된 요청 0건 · Taint 적용", drainPods)})
			})
		}
	}
}

// ambientKind 는 앰비언트(일상) 알림 유형이다 — 시나리오와 무관하게 플릿
// 전역에서 늘 벌어지는 일. 이게 없으면 평시 단계에서 알림 타임라인이 죽어
// 보인다(실 관제는 정상 상태에서도 계속 신호가 흐른다).
type ambientKind struct {
	code, title, severity string
	detail                func(g *GPU, sig gpuSignals) string
}

var ambientKinds = []ambientKind{
	{"UTIL_SURGE", "사용률 급등", "info", func(g *GPU, s gpuSignals) string {
		return fmt.Sprintf("%.0f%% 도달 — 추론 트래픽 버스트 (%s)", s.Util, g.Pool)
	}},
	{"MEM_PRESSURE", "GPU 메모리 압박", "warning", func(g *GPU, s gpuSignals) string {
		return fmt.Sprintf("메모리 %.0f%% 점유 — 배치 크기 검토 권고", s.MemUsed/s.MemTotal*100)
	}},
	{"POWER_CAP", "전력 상한 근접", "info", func(_ *GPU, s gpuSignals) string {
		return fmt.Sprintf("%.0fW — 랙 전력 예산 확인", s.Power)
	}},
	{"THERMAL_RISE", "온도 상승 추세", "warning", func(_ *GPU, s gpuSignals) string {
		return fmt.Sprintf("%.1f°C — 냉각 여유 감소", s.Temp)
	}},
	{"POD_RESCHEDULE", "추론 파드 재배치", "info", func(g *GPU, _ gpuSignals) string {
		return fmt.Sprintf("%s 풀 워크로드 재배치 완료", g.Pool)
	}},
	{"JOB_COMPLETE", "배치 잡 종료", "info", func(g *GPU, _ gpuSignals) string {
		return fmt.Sprintf("%s 잡 완료 — 자원 반환", g.Pool)
	}},
}

// fireAmbient 는 15s 틱마다 확률적으로 앰비언트 알림 1건을 낸다(결정론 —
// 같은 seed·시각이면 같은 알림). 대상은 실제 신호를 읽어 보고하므로 화면의
// 수치와 알림 내용이 어긋나지 않는다.
func (s *Scenario) fireAmbient(tMS int64) {
	tick := tMS / 15_000
	if tick == s.ambientTick {
		return
	}
	s.ambientTick = tick
	if unitFloat(fnvHash(s.cfg.Seed, "ambient", strconv.FormatInt(tick, 10))) > 0.4 {
		return
	}

	pool := s.fleet.GPUs
	if len(pool) == 0 {
		return
	}
	g := pool[int(fnvHash(s.cfg.Seed, "ambient-gpu", strconv.FormatInt(tick, 10))%uint64(len(pool)))]
	if !g.Allocated || g == s.victim {
		return
	}
	k := ambientKinds[int(fnvHash(s.cfg.Seed, "ambient-kind", strconv.FormatInt(tick, 10))%uint64(len(ambientKinds)))]
	sig := computeSignals(g, tMS, s.cfg.Seed)
	s.pushAlert(AlertEvent{
		At: tMS, Severity: k.severity, Code: k.code, Title: k.title,
		Instance: g.Instance, Device: g.Device, Detail: k.detail(g, sig),
	})
}

// Apply 는 운영자 액션을 처리한다. 현 단계에서 허용되지 않으면 에러(409 사유)를
// 낸다 — 시연 중 오조작이 서사를 깨지 않게 하는 안전장치다.
func (s *Scenario) Apply(a Action, actor string, params map[string]string, tMS int64) (string, error) {
	switch a {
	case ActionApproveIsolation:
		if s.phase != PhaseDegrading && s.phase != PhaseAwaitingApproval {
			return "", fmt.Errorf("현 단계(%s)에서는 격리 승인이 불가하다 — degrading/awaiting-approval 에서만 허용", s.phase)
		}
		s.transition(PhaseDraining, actor, "운영자 승인 — 격리 실행", tMS)
		return "격리 승인 — Cordon → Drain → Taint 진행", nil

	case ActionStartBurnin:
		if s.phase != PhaseBurninFailed {
			return "", fmt.Errorf("현 단계(%s)에서는 재번인 시작이 불가하다 — burnin-failed 에서만 허용", s.phase)
		}
		s.transition(PhaseBurnin2, actor, "운영자 재번인 시작", tMS)
		return "재번인 시작 — 클린 런 검증", nil

	case ActionReturnToService:
		if s.phase != PhaseReadyToReturn {
			return "", fmt.Errorf("현 단계(%s)에서는 재투입이 불가하다 — ready-to-return 에서만 허용", s.phase)
		}
		s.transition(PhaseReturned, actor, "운영자 재투입 승인", tMS)
		return "운영 풀 재투입 완료", nil

	case ActionReset:
		from := s.phase
		s.victim.Allocated = true
		s.phase = PhaseNormal
		s.phaseStart = tMS
		s.fired = map[string]bool{}
		s.pushAudit(AuditEntry{At: tMS, Actor: actor, Action: "reset",
			Target:    fmt.Sprintf("%s/%s", s.victim.Instance, s.victim.Device),
			PhaseFrom: string(from), PhaseTo: string(PhaseNormal), Result: "시나리오 초기화"})
		return "시나리오를 처음으로 되돌렸다", nil

	case ActionRegisterIdleReason:
		uuid, reason := params["uuid"], params["reason"]
		if uuid == "" || reason == "" {
			return "", fmt.Errorf("uuid 와 reason 파라미터가 필요하다")
		}
		g, ok := s.fleet.ByUUID[uuid]
		if !ok || g.IdleDays == 0 {
			return "", fmt.Errorf("장기 미사용 관리 대상이 아닌 uuid 다: %s", uuid)
		}
		s.idleReasons[uuid] = reason
		s.pushAudit(AuditEntry{At: tMS, Actor: actor, Action: "register-idle-reason",
			Target: uuid, Result: "미사용 사유 등록: " + reason})
		return "미사용 사유를 등록했다", nil

	case ActionReportFalsePositive:
		uuid := params["uuid"]
		if uuid == "" {
			return "", fmt.Errorf("uuid 파라미터가 필요하다")
		}
		note := params["note"]
		if note == "" {
			note = "오탐 신고"
		}
		s.falsePositives[uuid] = note
		s.pushAudit(AuditEntry{At: tMS, Actor: actor, Action: "report-false-positive",
			Target: uuid, Result: "오탐 피드백 접수 — 룰 보정 검토 대상"})
		return "오탐 피드백을 접수했다", nil

	case ActionSetMode:
		mode := RemediationMode(params["mode"])
		label, ok := modeLabels[mode]
		if !ok {
			return "", fmt.Errorf("알 수 없는 복구 모드: %q (observe|approve|limited-auto)", params["mode"])
		}
		from := s.mode
		s.mode = mode
		s.pushAudit(AuditEntry{At: tMS, Actor: actor, Action: "set-mode",
			Evidence: fmt.Sprintf("%s → %s", modeLabels[from], label),
			Result:   "자동복구 정책 변경 — " + label})
		return "복구 모드를 " + label + " 로 변경했다", nil

	case ActionConfigureBurnin:
		if p := params["profile"]; p != "" {
			s.burninProfile_ = p
		}
		if v := params["durationMin"]; v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < burninMinMinutes || n > burninMaxMinutes {
				return "", fmt.Errorf("지속시간은 %d~%d분 정수여야 한다: %q", burninMinMinutes, burninMaxMinutes, v)
			}
			s.burninMinutes = n
		}
		if v := params["targetUtilPct"]; v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < burninMinUtil || n > burninMaxUtil {
				return "", fmt.Errorf("목표 사용률은 %d~%d%% 정수여야 한다: %q", burninMinUtil, burninMaxUtil, v)
			}
			s.burninUtilPct = n
		}
		s.pushAudit(AuditEntry{At: tMS, Actor: actor, Action: "configure-burnin",
			Target:   fmt.Sprintf("%s/%s", s.victim.Instance, s.victim.Device),
			Evidence: fmt.Sprintf("프로파일 %s · %d분 · 목표 %d%%", s.burninProfile(), s.burninMinutes, s.burninUtilPct),
			Result:   "번인 프로파일 저장"})
		return "번인 프로파일을 저장했다", nil

	case ActionJumpPhase:
		target := Phase(params["phase"])
		if phaseIndexOfStrict(target) < 0 {
			return "", fmt.Errorf("알 수 없는 단계: %q", params["phase"])
		}
		if target == s.phase {
			return "이미 해당 단계다", nil
		}
		// 교체 단계 이후로 건너뛰면 새 개체가 있어야 서사가 성립한다.
		if phaseIndexOfStrict(target) >= phaseIndexOf(PhaseBurnin1) &&
			phaseIndexOf(s.phase) < phaseIndexOf(PhaseBurnin1) {
			s.fleet.ReplaceUUID(s.victim, s.cycle)
		}
		s.transition(target, actor, "운영자 단계 점프(시연 제어)", tMS)
		return "단계를 " + string(target) + " 로 이동했다", nil

	case ActionAckAlert:
		v := params["id"]
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			return "", fmt.Errorf("알림 ID 가 필요하다: %q", v)
		}
		s.ackedAlerts[id] = true
		return "알림을 확인 처리했다", nil
	}
	return "", fmt.Errorf("알 수 없는 액션: %s", a)
}

// ---- 신호 오버라이드 ----

// Override 는 victim·상시 이상 개체의 기본 신호를 단계별로 덮어쓴다.
// Util=NaN 은 "emit 중단"(교체 창 시리즈 소멸) 계약이다.
func (s *Scenario) Override(g *GPU, tMS int64, sig *gpuSignals) {
	for _, f := range s.faultGPUs {
		if g == f {
			sig.Util = 0
			sig.Temp = 41 + math.Abs(signalNoise(s.cfg.Seed, g.UUID, "ft", tMS))*2
			sig.Power = modelSpecOf(g.Model).IdleW
			sig.EccUnc = 6
			sig.MemUsed = sig.MemTotal * 0.02
			return
		}
	}
	for _, h := range s.hotGPUs {
		if g == h {
			sig.Temp = 83 + math.Abs(signalNoise(s.cfg.Seed, g.UUID, "ht", tMS))*3
			sig.Throttle = true
			return
		}
	}
	if g != s.victim {
		return
	}

	p := s.progress(tMS)
	spec := modelSpecOf(g.Model)
	switch s.phase {
	case PhaseNormal:
		// 기본 신호 그대로 (고부하 대역)
	case PhaseDegrading:
		sig.Temp = lerp(73, 85, p) + signalNoise(s.cfg.Seed, g.UUID, "dt", tMS)*0.8
		sig.EccCorr += math.Floor(p * 14)
		if p > 0.5 {
			sig.Throttle = true
			sig.Util = clamp(sig.Util-8, 0, 100) // throttle 로 인한 성능 저하
		}
	case PhaseAwaitingApproval:
		sig.Temp = 84 + signalNoise(s.cfg.Seed, g.UUID, "at", tMS)*1.2
		sig.EccCorr += 14
		sig.Throttle = true
		sig.Util = clamp(sig.Util-10, 0, 100)
	case PhaseDraining:
		sig.Util = clamp(sig.Util*(1-p), 0, 100)
		sig.Temp = lerp(84, 55, p)
		sig.EccCorr += 14
		sig.Power = spec.IdleW + (sig.Power-spec.IdleW)*(1-p)
		sig.MemUsed = sig.MemTotal * (0.05 + 0.6*(1-p))
	case PhaseReplacing:
		sig.Util = math.NaN() // 교체 창 — emit 중단, 시리즈 소멸
	case PhaseBurnin1, PhaseBurnin2:
		// 운영자가 설정한 목표 부하를 실제로 따른다 — 슬라이더가 곡선을 바꾼다.
		sig.Util = clamp(float64(s.burninUtilPct)+math.Abs(signalNoise(s.cfg.Seed, g.UUID, "bu", tMS))*3, 0, 99.9)
		// 1차 번인은 온도가 임계(80°C)를 살짝 넘는다 — 실패 판정(Health 75)의
		// 시각적 근거. 2차(클린 런)는 76°C 에서 멈춰 통과(96)한다.
		peak := 81.0
		if s.phase == PhaseBurnin2 {
			peak = 76
		}
		sig.Temp = lerp(48, peak, clamp(p*1.4, 0, 1)) + signalNoise(s.cfg.Seed, g.UUID, "bt", tMS)*0.8
		sig.Power = spec.IdleW + (spec.TDPWatts-spec.IdleW)*0.97
		sig.MemUsed = sig.MemTotal * 0.92
		sig.EccCorr = 0 // 교체된 새 개체
		if s.phase == PhaseBurnin1 && p > 0.85 {
			sig.EccCorr = 3 // 1차 번인 말미 — 실패 판정의 근거
		}
	case PhaseBurninFailed:
		sig.Util = 4
		sig.Temp = lerp(78, 52, p)
		sig.Power = spec.IdleW + 20
		sig.EccCorr = 3
	case PhaseReadyToReturn, PhaseReturned:
		if s.phase == PhaseReadyToReturn {
			sig.Util = 2
			sig.Temp = 45
			sig.Power = spec.IdleW
		}
		sig.EccCorr = 0
	}
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// NodeMissing 은 수집 중단 노드 여부다 — emitFast 가 해당 노드 시리즈 전체를
// 건너뛴다(Agent Missing 재현: 데이터가 "안 오는" 것이 신호다).
func (s *Scenario) NodeMissing(instance string) bool {
	return s.missingNode != nil && s.missingNode.Instance == instance
}

// FaultSet 은 장애 개체 uuid 집합이다 — 집계의 faulted 카운트와 히트맵 오버레이
// 근거. victim 은 격리~검증 구간에서 장애로 계수한다.
func (s *Scenario) FaultSet() map[string]struct{} {
	out := map[string]struct{}{}
	for _, g := range s.faultGPUs {
		out[g.UUID] = struct{}{}
	}
	switch s.phase {
	case PhaseDraining, PhaseReplacing, PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2, PhaseReadyToReturn:
		out[s.victim.UUID] = struct{}{}
	}
	return out
}

// PhaseIndex 는 시계열 annotation 용 단계 인덱스다.
func (s *Scenario) PhaseIndex() int {
	for i, p := range phaseOrder {
		if p == s.phase {
			return i
		}
	}
	return 0
}

// VictimHealth 는 victim 의 현재 Health Score 다. 번인 판정 구간은 대본 고정
// 값(75/96 서사)에 수렴하도록 입력을 구성한다.
func (s *Scenario) VictimHealth(tMS int64) HealthResult {
	p := s.progress(tMS)
	switch s.phase {
	case PhaseNormal:
		return Score(HealthInput{TempC: 72, BaselineNote: "경미한 클럭 변동 (정상 범위)"})
	case PhaseDegrading:
		in := HealthInput{TempC: lerp(73, 85, p), EccCorrDelta1h: math.Floor(p * 14)}
		if p > 0.5 {
			in.ThrottleActive = true
		}
		if p > 0.6 {
			in.XIDRecent = true
		}
		return Score(in)
	case PhaseAwaitingApproval:
		return Score(HealthInput{TempC: 84.5, EccCorrDelta1h: 14, ThrottleActive: true, XIDRecent: true})
	case PhaseDraining, PhaseReplacing:
		return Score(HealthInput{TempC: 84, EccCorrDelta1h: 14, XIDRecent: true})
	case PhaseBurnin1:
		return Score(HealthInput{TempC: lerp(48, 78, p), BaselineNote: "번인 진행 중 — 판정 전"})
	case PhaseBurninFailed:
		// 대본 고정 75 = 100 − temp_high(10, 번인 피크 80.6°C) − ecc_corrected_surge(15).
		return Score(HealthInput{TempC: 80.6, EccCorrDelta1h: 6})
	case PhaseBurnin2:
		return Score(HealthInput{TempC: lerp(48, 76, p), BaselineNote: "번인 진행 중 — 판정 전"})
	case PhaseReadyToReturn, PhaseReturned:
		return Score(HealthInput{TempC: 76.2, BaselineNote: ""})
	}
	return Score(HealthInput{BaselineNote: "판정 데이터 없음"})
}
