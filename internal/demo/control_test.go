// control_test.go — 화면의 컨트롤(복구 모드·번인 파라미터·단계 점프)이
// 표시용이 아니라 시나리오 동작을 실제로 바꾸는지 검증한다. 데모의 요구가
// "모든 기능을 눌러보고 확인 가능"이므로, 이 계약이 깨지면 시연이 거짓이 된다.
package demo

import "testing"

func TestSetMode_자동전이_정책이_실제로_바뀐다(t *testing.T) {
	// 관찰 모드 — 승인 대기에서 자동 승인하지 않는다.
	_, obs := newTestScenario(t)
	if _, err := obs.Apply(ActionSetMode, "admin", map[string]string{"mode": string(ModeObserve)}, t0); err != nil {
		t.Fatalf("관찰 모드 설정 실패: %v", err)
	}
	now := t0
	for obs.phase != PhaseAwaitingApproval {
		now = obs.phaseStart + obs.scaledDur(obs.phase) + 1
		obs.Advance(now)
		if obs.phase == PhaseDraining {
			t.Fatalf("관찰 모드인데 승인 단계를 건너뛰었다")
		}
	}
	// 대기 시간을 크게 넘겨도 머물러야 한다.
	obs.Advance(now + obs.scaledDur(PhaseAwaitingApproval)*5)
	if obs.phase != PhaseAwaitingApproval {
		t.Fatalf("관찰 모드에서 자동 진행됨: %s", obs.phase)
	}
	// 운영자가 승인하면 즉시 진행한다.
	if _, err := obs.Apply(ActionApproveIsolation, "admin", nil, now+1); err != nil {
		t.Fatalf("관찰 모드 수동 승인 실패: %v", err)
	}
	if obs.phase != PhaseDraining {
		t.Fatalf("수동 승인 후 단계 = %s, want draining", obs.phase)
	}

	// 제한 자동 모드 — 승인 단계를 건너뛴다.
	_, auto := newTestScenario(t)
	if _, err := auto.Apply(ActionSetMode, "admin", map[string]string{"mode": string(ModeLimitedAuto)}, t0); err != nil {
		t.Fatalf("제한 자동 설정 실패: %v", err)
	}
	now = auto.phaseStart + auto.scaledDur(PhaseNormal) + 1
	auto.Advance(now) // → degrading
	if auto.phase != PhaseDegrading {
		t.Fatalf("degrading 진입 실패: %s", auto.phase)
	}
	now = auto.phaseStart + auto.scaledDur(PhaseDegrading) + 1
	auto.Advance(now)
	if auto.phase != PhaseDraining {
		t.Fatalf("제한 자동 모드 단계 = %s, want draining(승인 생략)", auto.phase)
	}

	// 알 수 없는 모드는 거절한다.
	if _, err := auto.Apply(ActionSetMode, "admin", map[string]string{"mode": "yolo"}, now); err == nil {
		t.Fatalf("알 수 없는 모드가 허용됐다")
	}
}

func TestConfigureBurnin_시간과_부하가_실제로_반영된다(t *testing.T) {
	_, s := newTestScenario(t)
	baseDur := s.scaledDur(PhaseBurnin1)

	if _, err := s.Apply(ActionConfigureBurnin, "admin", map[string]string{
		"profile": "B200-Blackwell-v1", "durationMin": "60", "targetUtilPct": "88",
	}, t0); err != nil {
		t.Fatalf("번인 설정 실패: %v", err)
	}
	if got := s.scaledDur(PhaseBurnin1); got >= baseDur {
		t.Fatalf("지속시간 60분 설정 후 단계 길이 %d — 기본(120분) %d 보다 짧아야 한다", got, baseDur)
	}
	if s.burninProfile() != "B200-Blackwell-v1" {
		t.Fatalf("프로파일 미반영: %s", s.burninProfile())
	}

	// 목표 부하가 신호에 반영되는지 — 번인 단계의 victim util 이 설정값 근처.
	s.phase = PhaseBurnin1
	s.phaseStart = t0
	sig := computeSignals(s.victim, t0+1000, s.cfg.Seed)
	s.Override(s.victim, t0+1000, &sig)
	if sig.Util < 87 || sig.Util > 92 {
		t.Fatalf("번인 목표 88%% 설정인데 util=%.1f — 반영되지 않았다", sig.Util)
	}

	// 범위 밖은 거절한다(화면 슬라이더 범위와 동일 계약).
	for _, bad := range []map[string]string{
		{"durationMin": "10"}, {"durationMin": "999"},
		{"targetUtilPct": "10"}, {"targetUtilPct": "120"}, {"targetUtilPct": "x"},
	} {
		if _, err := s.Apply(ActionConfigureBurnin, "admin", bad, t0); err == nil {
			t.Errorf("범위 밖 번인 설정이 허용됐다: %v", bad)
		}
	}
}

func TestJumpPhase_시연제어(t *testing.T) {
	_, s := newTestScenario(t)
	orig := s.victim.UUID

	if _, err := s.Apply(ActionJumpPhase, "admin", map[string]string{"phase": string(PhaseBurninFailed)}, t0); err != nil {
		t.Fatalf("단계 점프 실패: %v", err)
	}
	if s.phase != PhaseBurninFailed {
		t.Fatalf("점프 후 단계 = %s", s.phase)
	}
	// 교체 이후 단계로 건너뛰면 새 개체여야 서사가 성립한다.
	if s.victim.UUID == orig {
		t.Fatalf("번인 단계로 점프했는데 uuid 가 교체 전 그대로다")
	}
	// Health 대본 고정점이 점프 후에도 유지된다.
	if got := s.VictimHealth(t0 + 1).Score; got != 75 {
		t.Fatalf("점프 후 burnin-failed Health = %d, want 75", got)
	}
	if _, err := s.Apply(ActionJumpPhase, "admin", map[string]string{"phase": "없는단계"}, t0); err == nil {
		t.Fatalf("알 수 없는 단계 점프가 허용됐다")
	}
}

func TestAmbient_평시에도_알림이_흐른다(t *testing.T) {
	_, s := newTestScenario(t)
	before := len(s.alerts)
	// 평시(normal) 단계에서 10분간 15s 틱을 돌린다 — 시나리오 알림이 아닌
	// 앰비언트 알림만으로 타임라인이 자라야 한다.
	for i := int64(1); i <= 40; i++ {
		s.Advance(t0 + i*15_000)
	}
	if len(s.alerts) <= before {
		t.Fatalf("평시 10분간 알림이 하나도 늘지 않았다(before=%d after=%d)", before, len(s.alerts))
	}
	// 앰비언트 알림은 실제 개체를 지목해야 한다.
	last := s.alerts[len(s.alerts)-1]
	if last.Instance == "" || last.Code == "" {
		t.Fatalf("앰비언트 알림에 대상/코드가 비었다: %+v", last)
	}
}

func TestSnapshot_컨트롤_메타가_노출된다(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:32,b:클라우드 B:16")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	snap := e.buildSnapshot(t0)
	if snap.Scenario.Mode != string(ModeApprove) {
		t.Fatalf("기본 모드 = %q, want approve", snap.Scenario.Mode)
	}
	if len(snap.Scenario.ModeOptions) != 3 {
		t.Fatalf("모드 선택지 %d개, want 3", len(snap.Scenario.ModeOptions))
	}
	if len(snap.Scenario.Phases) != len(phaseOrder) {
		t.Fatalf("단계 목록 %d개, want %d", len(snap.Scenario.Phases), len(phaseOrder))
	}
	for _, p := range snap.Scenario.Phases {
		if p.Label == "" {
			t.Fatalf("단계 %q 라벨이 비었다", p.ID)
		}
	}
	if snap.Scenario.Victim.Burnin.DurationMin != burninRefMinutes {
		t.Fatalf("번인 기본 지속시간 = %d, want %d", snap.Scenario.Victim.Burnin.DurationMin, burninRefMinutes)
	}
}
