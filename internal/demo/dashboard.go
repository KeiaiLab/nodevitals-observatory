package demo

// 통합 상황판 집계 — "지금 이 플릿이 어떤 상태인가"를 8개 영역으로 요약한다.
//
// 화면이 각자 세면 화면끼리 숫자가 어긋난다. 그래서 분류는 전부 여기서 하고,
// 각 분류는 *상호배타* 라 합이 전체와 일치한다(전 화면 단일 목데이터 정합 계약).

import "fmt"

// StatusCount 는 상황판 한 칸이다. Tone 은 색 결정용 — 화면이 라벨 문자열로
// 색을 추론하면(예: "장애"가 들어가면 빨강) 라벨을 바꾸는 순간 색이 깨진다.
type StatusCount struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Tone  string `json:"tone"` // ok | info | warn | major | crit | muted
}

// Dashboard 는 통합 상황판의 분류 집계다. CSP 상태·저활용·최근 이벤트는
// 이미 FleetSummary/Idle/Events 에 있으므로 중복해 담지 않는다.
type Dashboard struct {
	// Alerts 는 *미확인* 활성 장애 수다 — 확인 처리한 알림은 빠진다.
	Alerts  []StatusCount `json:"alerts"`
	GPUs    []StatusCount `json:"gpus"`
	Nodes   []StatusCount `json:"nodes"`
	Repair  []StatusCount `json:"repair"`
	Collect []StatusCount `json:"collect"`
}

// collectLagged 는 수집 지연 노드다 — 결정론 해시로 소수(≈0.4%)를 고른다.
// 실제 운영에서 늘 몇 대는 스크레이프가 느리다는 사실의 재현이며, 시드가
// 같으면 같은 노드가 나온다.
func collectLagged(seed int64, instance string) bool {
	return unitFloat(fnvHash(seed, "collectlag", instance)) < 0.004
}

// buildDashboard 는 상황판 집계를 만든다. 호출부(buildSnapshot)가 이미 mu 를
// 잡고 있으며, faults 는 그쪽에서 계산한 결과를 재사용한다(2회 계산 방지).
func (e *Engine) buildDashboard(faults map[string]struct{}) Dashboard {
	s, f := e.scen, e.fleet

	// ---- 활성 장애: 미확인 알림의 심각도 분포 ----
	sev := map[string]int{}
	for _, a := range s.alerts {
		if a.Acked {
			continue
		}
		sev[a.Severity]++
	}
	d := Dashboard{
		Alerts: []StatusCount{
			{Key: "critical", Label: "Critical", Count: sev["critical"], Tone: "crit"},
			{Key: "major", Label: "Major", Count: sev["major"], Tone: "major"},
			{Key: "warning", Label: "Warning", Count: sev["warning"], Tone: "warn"},
		},
	}

	// ---- GPU 상태: 상호배타 5분류 (합 = 전체) ----
	hot := map[string]struct{}{}
	for _, g := range s.hotGPUs {
		hot[g.UUID] = struct{}{}
	}
	if s.phase == PhaseDegrading || s.phase == PhaseAwaitingApproval {
		hot[s.victim.UUID] = struct{}{}
	}
	// 격리·복구는 victim 노드 단위로 일어난다 — GPU 1장 때문에 노드째 빠진다는
	// 사실이 시연의 핵심(비용 서사)이라 노드 전체를 그 상태로 센다.
	isolatedUUIDs, recoveringUUIDs := map[string]struct{}{}, map[string]struct{}{}
	if node := f.NodeOf(s.victim.Instance); node != nil {
		var target map[string]struct{}
		switch s.phase {
		case PhaseDraining, PhaseReplacing:
			target = isolatedUUIDs
		case PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2, PhaseReadyToReturn:
			target = recoveringUUIDs
		}
		if target != nil {
			for _, g := range node.GPUs {
				target[g.UUID] = struct{}{}
			}
		}
	}

	var gFault, gDegraded, gIsolated, gRecovering int
	for _, g := range f.GPUs {
		if _, ok := faults[g.UUID]; ok {
			gFault++
			continue
		}
		if _, ok := isolatedUUIDs[g.UUID]; ok {
			gIsolated++
			continue
		}
		if _, ok := recoveringUUIDs[g.UUID]; ok {
			gRecovering++
			continue
		}
		if _, ok := hot[g.UUID]; ok {
			gDegraded++
		}
	}
	gNormal := len(f.GPUs) - gFault - gDegraded - gIsolated - gRecovering
	d.GPUs = []StatusCount{
		{Key: "normal", Label: "정상", Count: gNormal, Tone: "ok"},
		{Key: "degraded", Label: "성능 저하", Count: gDegraded, Tone: "major"},
		{Key: "fault", Label: "장애", Count: gFault, Tone: "crit"},
		{Key: "isolated", Label: "격리", Count: gIsolated, Tone: "warn"},
		{Key: "recovering", Label: "복구 중", Count: gRecovering, Tone: "info"},
	}

	// ---- 노드 상태: 상호배타 5분류 ----
	degradedNodes := map[string]struct{}{}
	for uuid := range faults {
		if g, ok := f.ByUUID[uuid]; ok {
			degradedNodes[g.Instance] = struct{}{}
		}
	}
	for uuid := range hot {
		if g, ok := f.ByUUID[uuid]; ok {
			degradedNodes[g.Instance] = struct{}{}
		}
	}

	var nOffline, nDraining, nCordoned, nDegraded int
	for _, n := range f.Nodes {
		switch {
		case s.NodeMissing(n.Instance):
			nOffline++
		case n.Instance == s.victim.Instance && s.phase == PhaseDraining:
			nDraining++
		case n.Instance == s.victim.Instance && (s.phase == PhaseReplacing ||
			s.phase == PhaseBurnin1 || s.phase == PhaseBurninFailed ||
			s.phase == PhaseBurnin2 || s.phase == PhaseReadyToReturn):
			nCordoned++
		default:
			if _, ok := degradedNodes[n.Instance]; ok {
				nDegraded++
			}
		}
	}
	nReady := len(f.Nodes) - nOffline - nDraining - nCordoned - nDegraded
	d.Nodes = []StatusCount{
		{Key: "ready", Label: "Ready", Count: nReady, Tone: "ok"},
		{Key: "degraded", Label: "Degraded", Count: nDegraded, Tone: "major"},
		{Key: "cordoned", Label: "Cordoned", Count: nCordoned, Tone: "warn"},
		{Key: "draining", Label: "Draining", Count: nDraining, Tone: "info"},
		{Key: "offline", Label: "Offline", Count: nOffline, Tone: "crit"},
	}

	// ---- 자동복구: 사이클 누적 + 현재 진행 ----
	// 대본상 사이클마다 번인 1차가 한 번 실패하고 2차에 통과한다 — 완료 사이클
	// 수가 곧 성공·실패 누적이다.
	done := s.cycle - 1
	running, pending := 0, 0
	switch s.phase {
	case PhaseAwaitingApproval:
		pending = 1
	case PhaseDraining, PhaseReplacing, PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2, PhaseReadyToReturn:
		running = 1
	}
	succeeded, failed := done, done
	if s.phase == PhaseReturned {
		succeeded++
		failed++
	} else if s.phase == PhaseBurnin2 || s.phase == PhaseReadyToReturn {
		failed++ // 1차 실패가 이미 발생한 구간
	}
	d.Repair = []StatusCount{
		{Key: "running", Label: "실행 중", Count: running, Tone: "info"},
		{Key: "succeeded", Label: "성공", Count: succeeded, Tone: "ok"},
		{Key: "failed", Label: "실패", Count: failed, Tone: "major"},
		{Key: "pending", Label: "승인 대기", Count: pending, Tone: "warn"},
	}

	// ---- 수집 상태: 노드 단위 상호배타 3분류 ----
	var cMissing, cLag int
	for _, n := range f.Nodes {
		switch {
		case s.NodeMissing(n.Instance):
			cMissing++
		case collectLagged(e.cfg.Seed, n.Instance):
			cLag++
		}
	}
	d.Collect = []StatusCount{
		{Key: "ok", Label: "정상 수집", Count: len(f.Nodes) - cMissing - cLag, Tone: "ok"},
		{Key: "lag", Label: "수집 지연", Count: cLag, Tone: "warn"},
		{Key: "missing", Label: "데이터 누락", Count: cMissing, Tone: "crit"},
	}
	return d
}

// NodeOf 는 인스턴스명으로 노드를 찾는다.
func (f *Fleet) NodeOf(instance string) *Node {
	for _, n := range f.Nodes {
		if n.Instance == instance {
			return n
		}
	}
	return nil
}

// Regions 는 플릿의 리전 목록이다(공통 필터 차원). 클러스터 순서를 따라
// 중복 없이 모은다 — 정렬을 바꾸면 필터 드롭다운 순서가 흔들린다.
func (f *Fleet) Regions() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range f.Clusters {
		if !seen[c.Region] {
			seen[c.Region] = true
			out = append(out, c.Region)
		}
	}
	return out
}

// RegionDisplay 는 리전 id 의 표시명이다 — "<csp>-kr1" → "KR-1".
func RegionDisplay(region string) string {
	for i := len(region) - 1; i >= 0; i-- {
		if region[i] == '-' {
			suffix := region[i+1:]
			if len(suffix) > 2 && suffix[:2] == "kr" {
				return fmt.Sprintf("KR-%s", suffix[2:])
			}
			return suffix
		}
	}
	return region
}

// dedupeRegions 는 클러스터 순회로 모은 리전 목록에서 중복을 제거한다
// (한 리전에 클러스터가 여럿이다). 첫 등장 순서를 보존해 드롭다운이 흔들리지
// 않게 한다.
func dedupeRegions(in []RegionInfo) []RegionInfo {
	seen := map[string]bool{}
	out := make([]RegionInfo, 0, len(in))
	for _, r := range in {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out
}
