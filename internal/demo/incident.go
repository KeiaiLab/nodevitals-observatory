package demo

// 장애 분석 체인 — "이 장애가 어디서 났고 어디까지 번지는가"를 한 축으로 편다
// (사용자 지시 2026-07-27):
//
//   장애 → GPU UUID / MIG → 물리 서버 → Kubernetes Node → Node Pool
//        → Cluster → CSP / Region / AZ
//
// 아래에서 위로 읽는 순서가 중요하다. 관제 화면이 CSP 부터 내려가면 "어디를
// 봐야 하나"로 끝나지만, 장애 개체에서 시작해 위로 펴면 각 계층에서 *영향
// 규모가 얼마나 커지는지* 가 그대로 보인다 — 그게 blast radius 다.

import "fmt"

// ChainLink 는 체인 한 계층이다.
type ChainLink struct {
	Level  string `json:"level"`  // 계층 이름 (GPU / 물리 서버 / …)
	Value  string `json:"value"`  // 그 계층의 식별자
	Detail string `json:"detail"` // 부가 정보(모델·MIG·시리얼 등)
	// Scope 는 이 계층에서 장애가 닿는 GPU 수, Total 은 그 계층의 전체 GPU 수다.
	// 둘의 비가 곧 "여기까지 번졌을 때 얼마나 아픈가"다.
	Scope int    `json:"scope"`
	Total int    `json:"total"`
	Tone  string `json:"tone"`
}

// IncidentChain 은 현재 진행 중인 장애의 분석 체인이다.
type IncidentChain struct {
	// Active=false 면 진행 중인 장애가 없다 — 화면은 "정상" 상태를 보여준다.
	Active   bool        `json:"active"`
	Code     string      `json:"code"`
	Title    string      `json:"title"`
	Severity string      `json:"severity"`
	Phase    string      `json:"phase"`
	At       int64       `json:"at"`
	Links    []ChainLink `json:"links"`
}

// buildIncidentChain 은 victim 을 기점으로 체인을 편다. 호출부(buildSnapshot)가
// 이미 mu 를 잡고 있다.
func (e *Engine) buildIncidentChain(tMS int64) IncidentChain {
	s, f := e.scen, e.fleet
	v := s.victim
	if v == nil {
		return IncidentChain{}
	}

	chain := IncidentChain{
		Active: s.phase != PhaseNormal && s.phase != PhaseReturned,
		Phase:  string(s.phase),
		At:     tMS,
	}
	// 장애 제목·심각도는 victim 을 가리키는 가장 최근 알림에서 가져온다 —
	// 화면이 문구를 새로 지어내면 알림 목록과 다른 말을 하게 된다.
	for i := len(s.alerts) - 1; i >= 0; i-- {
		a := s.alerts[i]
		if a.Instance == v.Instance {
			chain.Code, chain.Title, chain.Severity, chain.At = a.Code, a.Title, a.Severity, a.At
			break
		}
	}
	if chain.Code == "" {
		// 알림이 아직 없을 수 있다(단계 점프로 진입한 경우). 그때 "장애 없음"이라
		// 말하면 active=true 와 정면으로 어긋나므로 단계에서 문구를 만든다.
		if chain.Active {
			chain.Code, chain.Severity = "IN_PROGRESS", "major"
			chain.Title = phaseIncidentTitle(s.phase)
		} else {
			chain.Code, chain.Title, chain.Severity = "NONE", "진행 중인 장애 없음", "info"
		}
	}

	node := f.NodeOf(v.Instance)
	if node == nil {
		return chain
	}

	// 각 계층의 전체 GPU 수 — 분모다.
	var nodePoolTotal, clusterTotal, azTotal, regionTotal, cspTotal int
	for _, g := range f.GPUs {
		if g.CSP == v.CSP {
			cspTotal++
		}
		if g.Region == v.Region {
			regionTotal++
		}
		if g.AZ == v.AZ {
			azTotal++
		}
		if g.Cluster == v.Cluster {
			clusterTotal++
		}
		if g.NodePool == v.NodePool {
			nodePoolTotal++
		}
	}

	// 영향 규모는 계층을 올라가도 *커지지 않는다* — 장애는 노드 1대에 갇혀
	// 있고, 위 계층은 "그 노드를 품은 범위"일 뿐이다. 이 구분이 blast radius
	// 를 과장하지 않는 핵심이다: 분자는 고정, 분모만 커진다.
	nodeGPUs := len(node.GPUs)
	asset := e.buildNodeAsset(node, tMS)
	mig := migProfileOf(e.cfg.Seed, fmt.Sprintf("%s/%s", v.Instance, v.Device), v.Model)

	chain.Links = []ChainLink{
		{
			Level: "GPU", Value: v.UUID,
			Detail: fmt.Sprintf("%s · %s · MIG %s", v.Device, v.Model, mig),
			Scope:  1, Total: nodeGPUs, Tone: "crit",
		},
		{
			Level: "물리 서버", Value: asset.SerialNumber,
			Detail: fmt.Sprintf("랙 %s · PDU %s", node.Rack, node.PDU),
			Scope:  nodeGPUs, Total: nodeGPUs, Tone: "crit",
		},
		{
			Level: "Kubernetes Node", Value: v.Instance,
			Detail: nodeStateLabel(s, v.Instance),
			Scope:  nodeGPUs, Total: nodeGPUs, Tone: "major",
		},
		{
			Level: "Node Pool", Value: node.NodePool,
			Detail: fmt.Sprintf("%s 노드 풀", node.Model),
			Scope:  nodeGPUs, Total: nodePoolTotal, Tone: "warn",
		},
		{
			Level: "Cluster", Value: v.Cluster,
			Detail: fmt.Sprintf("서빙 풀 %s", orDash(v.Pool)),
			Scope:  nodeGPUs, Total: clusterTotal, Tone: "warn",
		},
		// 마지막은 CSP / Region / AZ 를 한 계층으로 묶는다. 클러스터는 여러 AZ 에
		// 걸쳐 있으므로(K8s 관행) AZ 를 Cluster 위 계층으로 두면 포함 관계가
		// 거짓이 된다 — 회귀 테스트가 잡은 실제 결함이다. 대신 "이 노드가 클라우드
		// 어디에 있는가"를 한 줄로 말한다.
		{
			Level: "CSP / Region / AZ",
			Value: fmt.Sprintf("%s / %s / %s", cspDisplayOf(f, v.CSP), RegionDisplay(v.Region), node.AZ),
			Detail: fmt.Sprintf("리전 %s장 · AZ %s장 (전원·네트워크 장애 도메인)",
				formatInt(regionTotal), formatInt(azTotal)),
			Scope: nodeGPUs, Total: cspTotal, Tone: "info",
		},
	}
	return chain
}

// phaseIncidentTitle 은 알림이 아직 없는 구간의 장애 제목이다 — 체인이
// active 인데 "장애 없음"이라 말하는 모순을 막는다.
func phaseIncidentTitle(p Phase) string {
	switch p {
	case PhaseDegrading:
		return "무음 열화 진행 — K8s Ready 상태에서 GPU 이상"
	case PhaseAwaitingApproval:
		return "격리 승인 대기 — 자동복구가 근거를 모았다"
	case PhaseDraining:
		return "격리 진행 중 — Graceful Drain"
	case PhaseReplacing:
		return "노드 교체 중"
	case PhaseBurnin1, PhaseBurnin2:
		return "번인 검증 진행 중"
	case PhaseBurninFailed:
		return "번인 실패 — 재검증 필요"
	case PhaseReadyToReturn:
		return "검증 통과 — 운영 복귀 대기"
	default:
		return "장애 처리 진행 중"
	}
}

// nodeStateLabel 은 K8s 관점의 노드 상태다 — "Ready 인데 GPU 는 불량"이라는
// 무음 장애의 핵심 대비를 이 한 줄이 담는다.
func nodeStateLabel(s *Scenario, instance string) string {
	if s.NodeMissing(instance) {
		return "Offline — 수집 중단"
	}
	switch s.phase {
	case PhaseDraining:
		return "Draining — 파드 퇴거 중"
	case PhaseReplacing, PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2, PhaseReadyToReturn:
		return "Cordoned — 스케줄링 차단"
	default:
		return "Ready — K8s 는 정상으로 본다"
	}
}

// formatInt 는 천 단위 구분 — 7000 이 "7,000" 으로 읽혀야 규모가 전달된다.
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

func cspDisplayOf(f *Fleet, id string) string {
	for _, spec := range f.Specs {
		if spec.ID == id {
			return spec.Display
		}
	}
	return id
}

func orDash(s string) string {
	if s == "" {
		return "미할당"
	}
	return s
}
