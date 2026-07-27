package demo

// /api/v1/demo/state 응답 모델 — 프론트 서사 패널(시나리오·알림·감사·드레인·
// 번인 체크리스트)의 데이터 계약이다. 시계열은 여기 담지 않는다(차트는 TSDB
// 질의가 담당) — JSON 폭주 방지와 관심사 분리.

// Snapshot 은 상태 전체의 일관 복사본이다(Engine.mu 하에서 생성).
type Snapshot struct {
	Seed        int64   `json:"seed"`
	Cycle       int     `json:"cycle"`
	GeneratedAt int64   `json:"generatedAt"`
	TimeScale   float64 `json:"timeScale"`
	/** 재시딩 백필 진행 중 — 화면이 "새 데이터 생성 중"을 표시하는 근거. */
	Reseeding bool          `json:"reseeding"`
	Fleet     FleetSummary  `json:"fleet"`
	Scenario  ScenarioState `json:"scenario"`
	Alerts    []AlertEvent  `json:"alerts"`
	Audit     []AuditEntry  `json:"audit"`
	Idle      []IdleGPU     `json:"idle"`
	/** 서빙 계층 — 이 플랫폼의 워크로드는 추론 서빙이다(하드웨어 지표만으로는
	 *  운영 콘솔로 보이지 않는다). 값은 같은 틱의 사용률에서 파생한다. */
	Serving []ServingStats `json:"serving"`
	/** 운영 품질 지표(NFR-04) — 가용률·MTBF/MTTR·오차 예산. */
	SLO SLOState `json:"slo"`
	/** K8s 이벤트 스트림 — 알림과 분리된 '일상'. */
	Events []ClusterEvent `json:"events"`
}

type CSPSummary struct {
	ID        string  `json:"id"`
	Display   string  `json:"display"`
	Adapter   string  `json:"adapter"` // "native" | "mock"
	GPUTotal  int     `json:"gpuTotal"`
	Allocated int     `json:"allocated"`
	Faulted   int     `json:"faulted"`
	AllocPct  float64 `json:"allocPct"`
}

type ModelCount struct {
	Model string `json:"model"`
	Count int    `json:"count"`
}

type PoolSummary struct {
	ID        string `json:"id"`
	Display   string `json:"display"`
	Tenant    string `json:"tenant"`
	Allocated int    `json:"allocated"`
}

type TenantSummary struct {
	ID      string `json:"id"`
	Display string `json:"display"`
}

// MissingGPU 는 Agent Missing 노드 소속 GPU 다 — 시리즈가 없어 벽면에 나타나지
// 않는 개체를 프론트가 '데이터 없음' 셀로 합성해 총 셀 수를 인벤토리 전수와
// 일치시키는 근거(전 화면 정합 계약).
type MissingGPU struct {
	UUID     string `json:"uuid"`
	Instance string `json:"instance"`
	Device   string `json:"device"`
	CSP      string `json:"csp"`
	Cluster  string `json:"cluster"`
}

type FleetSummary struct {
	GPUTotal         int             `json:"gpuTotal"`
	NodeTotal        int             `json:"nodeTotal"`
	Allocated        int             `json:"allocated"`
	Faulted          int             `json:"faulted"`
	TempAlerts       int             `json:"tempAlerts"`
	AgentMissing     int             `json:"agentMissing"`
	CSPs             []CSPSummary    `json:"csps"`
	Models           []ModelCount    `json:"models"`
	Pools            []PoolSummary   `json:"pools"`
	Tenants          []TenantSummary `json:"tenants"`
	FaultUUIDs       []string        `json:"faultUuids"`       // 히트맵 빨간 테두리 오버레이
	TempAlertUUIDs   []string        `json:"tempAlertUuids"`   // 히트맵 주황 점 오버레이
	MissingInstances []string        `json:"missingInstances"` // Agent Missing 노드
	MissingGpus      []MissingGPU    `json:"missingGpus"`      // 위 노드 소속 GPU (벽면 합성 셀)
}

type StepState struct {
	Key    string `json:"key"` // cordon | drain | taint
	Label  string `json:"label"`
	Status string `json:"status"` // pending | running | done
}

type DrainState struct {
	Active          bool        `json:"active"`
	Progress        float64     `json:"progress"`
	InflightTotal   int         `json:"inflightTotal"`
	Inflight        int         `json:"inflight"`
	PodsTotal       int         `json:"podsTotal"`
	PodsEvicted     int         `json:"podsEvicted"`
	DroppedRequests int         `json:"droppedRequests"` // 서사 핵심: 항상 0
	Steps           []StepState `json:"steps"`
}

type CheckItem struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"` // pending | running | pass | warn
}

type BurninState struct {
	Active        bool        `json:"active"`
	Attempt       int         `json:"attempt"`
	Profile       string      `json:"profile"`
	TargetUtilPct int         `json:"targetUtilPct"`
	DurationMin   int         `json:"durationMin"`
	Progress      float64     `json:"progress"`
	Checklist     []CheckItem `json:"checklist"`
	Verdict       string      `json:"verdict,omitempty"`
}

// ModeOption 은 복구 모드 선택지다 — 화면 셀렉터가 이 목록을 그대로 그린다.
type ModeOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Describe string `json:"describe"`
}

// PhaseOption 은 시연 제어용 단계 목록이다(리모컨 단계 점프).
type PhaseOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Index int    `json:"index"`
}

type VictimState struct {
	Instance string       `json:"instance"`
	Device   string       `json:"device"`
	UUID     string       `json:"uuid"`
	Model    string       `json:"model"`
	CSP      string       `json:"csp"`
	Cluster  string       `json:"cluster"`
	Pool     string       `json:"pool"`
	Health   HealthResult `json:"health"`
	Drain    DrainState   `json:"drain"`
	Burnin   BurninState  `json:"burnin"`
}

// MaintenanceLock 은 CSP 정비 예정 신호를 받아 자동복구를 보류한 노드다 —
// 폐루프 안전장치의 표면. 실제 플릿 노드를 지목해야 화면 전체가 한 데이터셋을
// 보고 있다는 정합이 유지된다(하드코딩 예시 노드명 금지).
type MaintenanceLock struct {
	Instance string `json:"instance"`
	CSP      string `json:"csp"`
	Window   string `json:"window"`
	Reason   string `json:"reason"`
}

type ScenarioState struct {
	Phase         string      `json:"phase"`
	PhaseIndex    int         `json:"phaseIndex"`
	PhaseStartAt  int64       `json:"phaseStartAt"`
	PhaseDeadline int64       `json:"phaseDeadline"`
	AutoAdvance   bool        `json:"autoAdvance"`
	PendingAction string      `json:"pendingAction,omitempty"`
	Victim        VictimState `json:"victim"`
	/** 현재 복구 모드 — 화면 셀렉터의 선택 상태이자 자동 전이의 실제 정책. */
	Mode        string        `json:"mode"`
	ModeOptions []ModeOption  `json:"modeOptions"`
	Phases      []PhaseOption `json:"phases"`
	/** CSP 정비 예정으로 자동복구를 보류 중인 노드(실제 플릿 개체). */
	MaintenanceLocks []MaintenanceLock `json:"maintenanceLocks"`
}

type IdleGPU struct {
	UUID     string `json:"uuid"`
	Instance string `json:"instance"`
	Device   string `json:"device"`
	Model    string `json:"model"`
	CSP      string `json:"csp"`
	Pool     string `json:"pool"`
	IdleDays int    `json:"idleDays"`
	Reason   string `json:"reason,omitempty"`
}

// NodeAssetByInstance 는 노드 자산 대장을 낸다(드릴다운 API). 없는 노드는
// ok=false.
func (e *Engine) NodeAssetByInstance(instance string) (NodeAsset, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, n := range e.fleet.Nodes {
		if n.Instance == instance {
			return e.buildNodeAsset(n, e.nowFn()), true
		}
	}
	return NodeAsset{}, false
}

// buildSnapshot 은 Engine.mu 하에서 호출된다(호출자 책임). 링 버퍼는 복사해
// 내보낸다 — 스냅샷 반환 후의 내부 변경이 응답에 새어 나가지 않게.
func (e *Engine) buildSnapshot(tMS int64) Snapshot {
	s := e.scen
	f := e.fleet

	snap := Snapshot{
		Seed:        e.cfg.Seed,
		Cycle:       s.cycle,
		GeneratedAt: tMS,
		TimeScale:   e.cfg.TimeScale,
		Reseeding:   e.reseeding.Load(),
	}

	// ---- fleet 요약 (정적 카운트 + 시나리오 유래 결함) ----
	faults := s.FaultSet()
	perCSP := map[string]*CSPSummary{}
	for _, spec := range f.Specs {
		perCSP[spec.ID] = &CSPSummary{ID: spec.ID, Display: spec.Display, Adapter: spec.Adapter}
	}
	modelCount := map[string]int{}
	poolAlloc := map[string]int{}

	fs := FleetSummary{NodeTotal: len(f.Nodes)}
	for _, g := range f.GPUs {
		fs.GPUTotal++
		c := perCSP[g.CSP]
		c.GPUTotal++
		if g.Allocated {
			fs.Allocated++
			c.Allocated++
			poolAlloc[g.Pool]++
		}
		if _, ok := faults[g.UUID]; ok {
			fs.Faulted++
			c.Faulted++
			fs.FaultUUIDs = append(fs.FaultUUIDs, g.UUID)
		}
		modelCount[g.Model]++
	}
	for _, g := range s.hotGPUs {
		fs.TempAlertUUIDs = append(fs.TempAlertUUIDs, g.UUID)
	}
	fs.TempAlerts = len(s.hotGPUs)
	if s.phase == PhaseDegrading || s.phase == PhaseAwaitingApproval {
		fs.TempAlertUUIDs = append(fs.TempAlertUUIDs, s.victim.UUID)
		fs.TempAlerts++
	}
	if s.missingNode != nil {
		fs.AgentMissing = 1
		fs.MissingInstances = append(fs.MissingInstances, s.missingNode.Instance)
		for _, g := range s.missingNode.GPUs {
			fs.MissingGpus = append(fs.MissingGpus, MissingGPU{
				UUID: g.UUID, Instance: g.Instance, Device: g.Device, CSP: g.CSP, Cluster: g.Cluster,
			})
		}
	}
	for _, spec := range f.Specs {
		c := perCSP[spec.ID]
		if c.GPUTotal > 0 {
			c.AllocPct = float64(c.Allocated) / float64(c.GPUTotal) * 100
		}
		fs.CSPs = append(fs.CSPs, *c)
	}
	for _, m := range gpuModels {
		if n := modelCount[m.Name]; n > 0 {
			fs.Models = append(fs.Models, ModelCount{Model: m.Name, Count: n})
		}
	}
	for _, p := range f.Pools {
		fs.Pools = append(fs.Pools, PoolSummary{ID: p.ID, Display: p.Display, Tenant: p.Tenant, Allocated: poolAlloc[p.ID]})
	}
	for _, t := range f.Tenants {
		fs.Tenants = append(fs.Tenants, TenantSummary{ID: t.ID, Display: t.Display})
	}
	snap.Fleet = fs

	// ---- 시나리오 상태 ----
	phases := make([]PhaseOption, 0, len(phaseOrder))
	for i, p := range phaseOrder {
		phases = append(phases, PhaseOption{ID: string(p), Label: phaseLabels[p], Index: i})
	}
	snap.Scenario = ScenarioState{
		Phase:         string(s.phase),
		PhaseIndex:    s.PhaseIndex(),
		PhaseStartAt:  s.phaseStart,
		PhaseDeadline: s.phaseStart + s.scaledDur(s.phase),
		// 관찰 모드의 승인 대기는 자동 전이하지 않는다 — 화면이 "대기 중"을
		// 정확히 표시하도록 실제 정책을 반영한다.
		AutoAdvance:   !(s.mode == ModeObserve && s.phase == PhaseAwaitingApproval),
		PendingAction: pendingActionFor(s.phase),
		Victim:        e.buildVictimState(tMS),
		Mode:          string(s.mode),
		ModeOptions: []ModeOption{
			{ID: string(ModeObserve), Label: modeLabels[ModeObserve], Describe: "탐지만 하고 조치하지 않는다 — 승인 전까지 대기 유지"},
			{ID: string(ModeApprove), Label: modeLabels[ModeApprove], Describe: "운영자 승인 후 격리 (권장) — 대기 타임아웃 시 자동 승인"},
			{ID: string(ModeLimitedAuto), Label: modeLabels[ModeLimitedAuto], Describe: "가드레일 통과 시 승인 없이 즉시 격리"},
		},
		Phases:           phases,
		MaintenanceLocks: s.maintenanceLocks(),
	}

	// ---- 링 버퍼 복사 (최신이 앞으로) ----
	snap.Alerts = reverseCopyAlerts(s.alerts)
	for i := range snap.Alerts {
		snap.Alerts[i].Acked = s.ackedAlerts[snap.Alerts[i].ID]
	}
	snap.Audit = reverseCopyAudit(s.audit)

	// ---- 서빙·SLO·이벤트 (마지막 틱의 파생값 재사용 — 재계산하면 화면과
	// 시계열이 어긋난다) ----
	snap.Serving = append([]ServingStats(nil), e.serving...)
	snap.SLO = e.slo
	snap.Events = reverseCopyEvents(s.events)

	// ---- 장기 미사용 GPU ----
	for _, g := range f.GPUs {
		if g.IdleDays > 0 {
			snap.Idle = append(snap.Idle, IdleGPU{
				UUID: g.UUID, Instance: g.Instance, Device: g.Device, Model: g.Model,
				CSP: g.CSP, Pool: g.Pool, IdleDays: g.IdleDays, Reason: s.idleReasons[g.UUID],
			})
		}
	}
	return snap
}

func pendingActionFor(p Phase) string {
	switch p {
	case PhaseDegrading, PhaseAwaitingApproval:
		return string(ActionApproveIsolation)
	case PhaseBurninFailed:
		return string(ActionStartBurnin)
	case PhaseReadyToReturn:
		return string(ActionReturnToService)
	}
	return ""
}

func (e *Engine) buildVictimState(tMS int64) VictimState {
	s := e.scen
	v := s.victim
	p := s.progress(tMS)

	vs := VictimState{
		Instance: v.Instance, Device: v.Device, UUID: v.UUID, Model: v.Model,
		CSP: v.CSP, Cluster: v.Cluster, Pool: v.Pool,
		Health: s.VictimHealth(tMS),
	}

	// 드레인 상태 — draining 단계에서만 라이브 카운터, 그 이후 단계는 완료 표시.
	afterDrain := s.PhaseIndex() > phaseIndexOf(PhaseDraining)
	switch {
	case s.phase == PhaseDraining:
		vs.Drain = DrainState{
			Active: true, Progress: p,
			InflightTotal: drainInflight, Inflight: int(float64(drainInflight) * (1 - p)),
			PodsTotal: drainPods, PodsEvicted: int(float64(drainPods) * p),
			DroppedRequests: 0,
			Steps: []StepState{
				{Key: "cordon", Label: "Cordon — 신규 워크로드 차단", Status: "done"},
				{Key: "drain", Label: "Graceful Drain — in-flight 보호 퇴거", Status: "running"},
				{Key: "taint", Label: "Taint — 노드 격리 확정", Status: "pending"},
			},
		}
		if p >= 0.95 {
			vs.Drain.Steps[1].Status = "done"
			vs.Drain.Steps[2].Status = "running"
		}
	case afterDrain:
		vs.Drain = DrainState{
			Active: false, Progress: 1,
			InflightTotal: drainInflight, Inflight: 0,
			PodsTotal: drainPods, PodsEvicted: drainPods,
			DroppedRequests: 0,
			Steps: []StepState{
				{Key: "cordon", Label: "Cordon — 신규 워크로드 차단", Status: "done"},
				{Key: "drain", Label: "Graceful Drain — in-flight 보호 퇴거", Status: "done"},
				{Key: "taint", Label: "Taint — 노드 격리 확정", Status: "done"},
			},
		}
	default:
		vs.Drain = DrainState{
			InflightTotal: drainInflight, PodsTotal: drainPods,
			Steps: []StepState{
				{Key: "cordon", Label: "Cordon — 신규 워크로드 차단", Status: "pending"},
				{Key: "drain", Label: "Graceful Drain — in-flight 보호 퇴거", Status: "pending"},
				{Key: "taint", Label: "Taint — 노드 격리 확정", Status: "pending"},
			},
		}
	}

	vs.Burnin = e.buildBurninState(tMS, p)
	return vs
}

func phaseIndexOf(p Phase) int {
	for i, x := range phaseOrder {
		if x == p {
			return i
		}
	}
	return 0
}

// buildBurninState 는 번인 체크리스트 7종의 단계별 상태를 만든다. 항목 1~5는
// 진행률에 따라 순차 통과, 6(부하 유지)·7(ECC 무결)은 말미 판정이다 — 1차는
// 7번이 warn 으로 끝나 "재검증 필요", 2차는 전부 pass 로 끝난다.
func (e *Engine) buildBurninState(tMS int64, p float64) BurninState {
	s := e.scen
	b := BurninState{
		Profile:       s.burninProfile(),
		TargetUtilPct: s.burninUtilPct,
		DurationMin:   s.burninMinutes,
	}

	newChecklist := func() []CheckItem {
		return []CheckItem{
			{ID: "k8s-ready", Label: "K8s Ready 확인"},
			{ID: "driver", Label: "NVIDIA 드라이버 버전·로드"},
			{ID: "inventory", Label: "GPU 수량·UUID·MIG 구성"},
			{ID: "dcgm", Label: "DCGM 메트릭·기존 오류 상태"},
			{ID: "nvlink", Label: "NVLink·RDMA 연결"},
			{ID: "burnjob", Label: "검증 GPU Job — 부하 95% 유지"},
			{ID: "ecc", Label: "번인 중 ECC 무결"},
		}
	}
	fill := func(items []CheckItem, prog float64, eccWarnLate bool) {
		for i := 0; i < 5; i++ {
			threshold := float64(i+1) * 0.1
			switch {
			case prog >= threshold:
				items[i].Status = "pass"
			case prog >= threshold-0.1:
				items[i].Status = "running"
			default:
				items[i].Status = "pending"
			}
		}
		switch {
		case prog >= 0.9:
			items[5].Status = "pass"
		case prog >= 0.5:
			items[5].Status = "running"
		default:
			items[5].Status = "pending"
		}
		switch {
		case eccWarnLate && prog >= 0.85:
			items[6].Status = "warn"
		case prog >= 0.9:
			items[6].Status = "pass"
		case prog >= 0.5:
			items[6].Status = "running"
		default:
			items[6].Status = "pending"
		}
	}

	switch s.phase {
	case PhaseBurnin1:
		b.Active, b.Attempt = true, 1
		b.Progress = p
		b.Checklist = newChecklist()
		fill(b.Checklist, p, true)
	case PhaseBurninFailed:
		b.Attempt = 1
		b.Progress = 1
		b.Checklist = newChecklist()
		fill(b.Checklist, 1, true)
		b.Verdict = "재검증 필요 (Uncordon 보류)"
	case PhaseBurnin2:
		b.Active, b.Attempt = true, 2
		b.Progress = p
		b.Checklist = newChecklist()
		fill(b.Checklist, p, false)
	case PhaseReadyToReturn, PhaseReturned:
		b.Attempt = 2
		b.Progress = 1
		b.Checklist = newChecklist()
		fill(b.Checklist, 1, false)
		b.Verdict = "통과 — 재투입 가능"
	default:
		b.Checklist = newChecklist()
		fill(b.Checklist, 0, false)
	}
	return b
}

func reverseCopyAlerts(in []AlertEvent) []AlertEvent {
	out := make([]AlertEvent, len(in))
	for i, a := range in {
		out[len(in)-1-i] = a
	}
	return out
}

func reverseCopyEvents(in []ClusterEvent) []ClusterEvent {
	out := make([]ClusterEvent, len(in))
	for i, a := range in {
		out[len(in)-1-i] = a
	}
	return out
}

func reverseCopyAudit(in []AuditEntry) []AuditEntry {
	out := make([]AuditEntry, len(in))
	for i, a := range in {
		out[len(in)-1-i] = a
	}
	return out
}
