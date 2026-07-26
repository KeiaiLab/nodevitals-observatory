package demo

import (
	"testing"
	"time"
)

// 고정 ms epoch — apiserver 테스트 관례(queryrange_test.go) 미러.
const t0 = int64(1_700_000_000_000)

func testConfig(t *testing.T, fleetSpec string) Config {
	t.Helper()
	specs, err := ParseFleetSpec(fleetSpec)
	if err != nil {
		t.Fatalf("플릿 스펙 파싱 실패: %v", err)
	}
	return Config{
		Seed: 7, Fleet: specs,
		BackfillAgg: 10 * time.Minute, BackfillGPU: 2 * time.Minute,
		TimeScale: 1.0,
	}
}

func newTestScenario(t *testing.T) (*Fleet, *Scenario) {
	t.Helper()
	cfg := testConfig(t, "a:클라우드 A:32,b:클라우드 B:16")
	fleet := BuildFleet(cfg.Fleet, cfg.Seed)
	return fleet, newScenario(fleet, cfg, t0)
}

func TestScenario_자동_전이_전체_사이클(t *testing.T) {
	_, s := newTestScenario(t)

	if s.phase != PhaseNormal {
		t.Fatalf("초기 단계 = %s, want normal", s.phase)
	}

	// 각 단계 duration + 1틱 시점으로 시간을 밀며 자동 전이를 순회한다.
	now := t0
	want := []Phase{
		PhaseDegrading, PhaseAwaitingApproval, PhaseDraining, PhaseReplacing,
		PhaseBurnin1, PhaseBurninFailed, PhaseBurnin2, PhaseReadyToReturn,
		PhaseReturned, PhaseNormal,
	}
	for _, w := range want {
		now = s.phaseStart + s.scaledDur(s.phase) + 1
		s.Advance(now)
		if s.phase != w {
			t.Fatalf("자동 전이 오류: got %s, want %s", s.phase, w)
		}
	}
	if s.cycle != 2 {
		t.Fatalf("사이클 미증가: %d, want 2", s.cycle)
	}
}

func TestScenario_교체시_uuid_변경과_복귀시_할당복원(t *testing.T) {
	fleet, s := newTestScenario(t)
	orig := s.victim.UUID

	now := t0
	for s.phase != PhaseBurnin1 {
		now = s.phaseStart + s.scaledDur(s.phase) + 1
		s.Advance(now)
	}
	if s.victim.UUID == orig {
		t.Fatalf("burnin-1 진입 후에도 uuid 가 그대로다 — 교체 서사 누락")
	}
	if _, ok := fleet.ByUUID[s.victim.UUID]; !ok {
		t.Fatalf("새 uuid 가 ByUUID 색인에 없다")
	}
	if _, ok := fleet.ByUUID[orig]; ok {
		t.Fatalf("구 uuid 가 색인에 남아 있다")
	}
	if s.victim.Allocated {
		t.Fatalf("격리 구간에서 victim 이 여전히 할당 상태다")
	}

	for s.phase != PhaseReturned {
		now = s.phaseStart + s.scaledDur(s.phase) + 1
		s.Advance(now)
	}
	if !s.victim.Allocated {
		t.Fatalf("재투입 후 할당이 복원되지 않았다")
	}
}

func TestScenario_액션_전이와_단계_불일치(t *testing.T) {
	_, s := newTestScenario(t)

	// normal 에서 격리 승인은 불가(409 사유).
	if _, err := s.Apply(ActionApproveIsolation, "admin", nil, t0+1); err == nil {
		t.Fatalf("normal 단계에서 approve-isolation 이 허용됐다")
	}

	// degrading 진입 후 승인 → 즉시 draining.
	now := s.phaseStart + s.scaledDur(PhaseNormal) + 1
	s.Advance(now)
	if s.phase != PhaseDegrading {
		t.Fatalf("degrading 진입 실패: %s", s.phase)
	}
	if _, err := s.Apply(ActionApproveIsolation, "admin", nil, now+1); err != nil {
		t.Fatalf("degrading 에서 승인 실패: %v", err)
	}
	if s.phase != PhaseDraining {
		t.Fatalf("승인 후 단계 = %s, want draining", s.phase)
	}

	// 감사 로그에 isolate 항목이 actor=admin 으로 남아야 한다.
	found := false
	for _, a := range s.audit {
		if a.Action == "isolate" && a.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("승인 격리의 감사 항목이 없다: %+v", s.audit)
	}

	// reset — 어느 단계에서든 normal 복귀 + victim 할당 복원.
	if _, err := s.Apply(ActionReset, "admin", nil, now+2); err != nil {
		t.Fatalf("reset 실패: %v", err)
	}
	if s.phase != PhaseNormal || !s.victim.Allocated {
		t.Fatalf("reset 후 상태 오류: phase=%s allocated=%v", s.phase, s.victim.Allocated)
	}
}

func TestScenario_부가_액션(t *testing.T) {
	fleet, s := newTestScenario(t)

	var idle *GPU
	for _, g := range fleet.GPUs {
		if g.IdleDays > 0 {
			idle = g
			break
		}
	}
	if idle == nil {
		t.Skip("소형 플릿에 장기 미사용 개체가 없다 — 시드 조합상 허용")
	}
	if _, err := s.Apply(ActionRegisterIdleReason, "admin",
		map[string]string{"uuid": idle.UUID, "reason": "서비스 출시 지연"}, t0+1); err != nil {
		t.Fatalf("미사용 사유 등록 실패: %v", err)
	}
	if s.idleReasons[idle.UUID] != "서비스 출시 지연" {
		t.Fatalf("사유 미기록: %+v", s.idleReasons)
	}
	if _, err := s.Apply(ActionRegisterIdleReason, "admin",
		map[string]string{"uuid": "GPU-없는-uuid", "reason": "x"}, t0+2); err == nil {
		t.Fatalf("존재하지 않는 uuid 등록이 허용됐다")
	}
	if _, err := s.Apply(ActionReportFalsePositive, "admin",
		map[string]string{"uuid": idle.UUID}, t0+3); err != nil {
		t.Fatalf("오탐 피드백 실패: %v", err)
	}
}

func TestVictimHealth_대본_고정점(t *testing.T) {
	_, s := newTestScenario(t)

	// 대본 앵커: 1차 번인 실패 = 75, 재투입 대기 = 96.
	s.phase = PhaseBurninFailed
	s.phaseStart = t0
	if got := s.VictimHealth(t0 + 1).Score; got != 75 {
		t.Fatalf("burnin-failed Health = %d, want 75", got)
	}
	s.phase = PhaseReadyToReturn
	if got := s.VictimHealth(t0 + 1).Score; got != 96 {
		t.Fatalf("ready-to-return Health = %d, want 96", got)
	}

	// 판정 근거가 반드시 동반된다(설명 가능성 계약).
	res := Score(HealthInput{TempC: 80.6, EccCorrDelta1h: 6})
	if len(res.Deductions) != 2 || res.Score != 75 {
		t.Fatalf("감점표 구성 오류: %+v", res)
	}
}
