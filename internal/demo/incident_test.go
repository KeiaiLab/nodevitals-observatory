package demo

import "testing"

// 체인은 GPU → 물리 서버 → K8s Node → Node Pool → Cluster → AZ → Region → CSP
// 순서로, 위로 갈수록 분모(Total)만 커져야 한다. 분자(Scope)가 함께 커지면
// blast radius 를 과장하게 된다 — 장애는 노드 1대에 갇혀 있다.
func TestIncidentChain_계층_순서와_영향_규모(t *testing.T) {
	e := NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:1400,b:클라우드 B:700"), func() int64 { return t0 })
	if _, err := e.Do(ActionJumpPhase, "admin", map[string]string{"phase": string(PhaseAwaitingApproval)}); err != nil {
		t.Fatalf("단계 이동 실패: %v", err)
	}
	chain := e.Snapshot().Incident

	want := []string{"GPU", "물리 서버", "Kubernetes Node", "Node Pool", "Cluster", "CSP / Region / AZ"}
	if len(chain.Links) != len(want) {
		t.Fatalf("체인 %d 계층, want %d", len(chain.Links), len(want))
	}
	for i, level := range want {
		if chain.Links[i].Level != level {
			t.Fatalf("%d번째 계층 = %q, want %q", i, chain.Links[i].Level, level)
		}
		if chain.Links[i].Value == "" {
			t.Fatalf("%s 계층의 식별자가 비었다", level)
		}
	}

	// 분자 고정 · 분모 단조 증가.
	scope := chain.Links[1].Scope // 물리 서버부터는 노드 전체가 영향권
	for _, l := range chain.Links[1:] {
		if l.Scope != scope {
			t.Fatalf("%s 계층 Scope=%d, want %d — 영향 규모가 계층마다 부풀었다", l.Level, l.Scope, scope)
		}
		if l.Total < l.Scope {
			t.Fatalf("%s 계층 Total(%d) < Scope(%d)", l.Level, l.Total, l.Scope)
		}
	}
	for i := 3; i < len(chain.Links); i++ {
		if chain.Links[i].Total < chain.Links[i-1].Total {
			t.Fatalf("%s 계층 Total(%d) 이 하위 계층(%d)보다 작다 — 포함 관계가 깨졌다",
				chain.Links[i].Level, chain.Links[i].Total, chain.Links[i-1].Total)
		}
	}
	if !chain.Active {
		t.Fatalf("승인 대기 단계인데 Active=false")
	}
}

// 정상 단계에서는 진행 중인 장애가 없다고 말해야 한다 — 체인이 항상 붉으면
// 화면이 늑대 소년이 된다.
func TestIncidentChain_정상_단계는_비활성(t *testing.T) {
	e := NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:64"), func() int64 { return t0 })
	if _, err := e.Do(ActionJumpPhase, "admin", map[string]string{"phase": string(PhaseNormal)}); err != nil {
		t.Fatalf("단계 이동 실패: %v", err)
	}
	if e.Snapshot().Incident.Active {
		t.Fatalf("정상 단계인데 Active=true")
	}
}
