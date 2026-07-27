package demo

import "testing"

func sumCounts(cs []StatusCount) int {
	n := 0
	for _, c := range cs {
		n += c.Count
	}
	return n
}

// 상황판 분류는 상호배타여야 한다 — 합이 전체와 어긋나면 카드마다 다른 규모를
// 말하게 되고, 그게 전 화면 단일 목데이터 정합 계약을 깬다.
func TestDashboard_분류합이_전체와_일치한다(t *testing.T) {
	e := NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:64,b:클라우드 B:32"), func() int64 { return t0 })

	// 시나리오 전 단계를 훑는다 — 격리·복구 구간에서만 드러나는 이중 계상이 있다.
	for _, p := range []Phase{
		PhaseNormal, PhaseDegrading, PhaseAwaitingApproval, PhaseDraining,
		PhaseReplacing, PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2,
		PhaseReadyToReturn, PhaseReturned,
	} {
		if _, err := e.Do(ActionJumpPhase, "admin", map[string]string{"phase": string(p)}); err != nil {
			t.Fatalf("%s 단계 이동 실패: %v", p, err)
		}
		snap := e.Snapshot()
		d := snap.Dashboard

		if got := sumCounts(d.GPUs); got != len(e.fleet.GPUs) {
			t.Fatalf("[%s] GPU 분류 합 %d, want %d", p, got, len(e.fleet.GPUs))
		}
		if got := sumCounts(d.Nodes); got != len(e.fleet.Nodes) {
			t.Fatalf("[%s] 노드 분류 합 %d, want %d", p, got, len(e.fleet.Nodes))
		}
		if got := sumCounts(d.Collect); got != len(e.fleet.Nodes) {
			t.Fatalf("[%s] 수집 분류 합 %d, want %d", p, got, len(e.fleet.Nodes))
		}
		for _, c := range append(append([]StatusCount{}, d.GPUs...), d.Nodes...) {
			if c.Count < 0 {
				t.Fatalf("[%s] 음수 카운트: %s=%d — 분류가 겹쳐 빼기가 어긋났다", p, c.Key, c.Count)
			}
		}
	}
}

// 리전은 공통 필터의 한 차원이다 — 비어 있으면 드롭다운이 죽는다.
func TestDashboard_리전_목록이_채워진다(t *testing.T) {
	e := NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:1400,b:클라우드 B:700"), func() int64 { return t0 })
	snap := e.Snapshot()

	if len(snap.Fleet.Regions) == 0 {
		t.Fatalf("리전 목록이 비었다")
	}
	seen := map[string]bool{}
	for _, r := range snap.Fleet.Regions {
		if seen[r.ID] {
			t.Fatalf("리전 중복: %s", r.ID)
		}
		seen[r.ID] = true
		if r.Display == "" || r.CSP == "" {
			t.Fatalf("리전 표시명/CSP 가 비었다: %+v", r)
		}
	}
	// 모든 GPU 는 목록에 있는 리전에 속해야 한다 — 필터로 고르면 반드시 잡힌다.
	for _, g := range e.fleet.GPUs {
		if !seen[g.Region] {
			t.Fatalf("GPU %s 의 리전 %q 가 목록에 없다", g.UUID, g.Region)
		}
	}
}
