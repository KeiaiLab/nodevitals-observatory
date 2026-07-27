package demo

import (
	"testing"
	"time"
)

func TestReseed_플릿이_통째로_새로_만들어진다(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:64,b:클라우드 B:32")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	beforeSeed := e.cfg.Seed
	beforeUUIDs := map[string]bool{}
	for _, g := range e.fleet.GPUs {
		beforeUUIDs[g.UUID] = true
	}
	beforeVictim := e.scen.victim.UUID

	res, err := e.Do(ActionReseed, "admin", map[string]string{"seed": "999"})
	if err != nil {
		t.Fatalf("재시딩 실패: %v", err)
	}
	if !res.Applied {
		t.Fatalf("재시딩 결과 applied=false")
	}
	waitReseed(t, e)

	if e.cfg.Seed == beforeSeed {
		t.Fatalf("시드가 그대로다: %d", e.cfg.Seed)
	}
	// 인벤토리가 실제로 바뀌어야 "새 목데이터"다 — 같은 uuid 가 대부분이면
	// 버튼을 눌러도 시연이 똑같아 보인다.
	same := 0
	for _, g := range e.fleet.GPUs {
		if beforeUUIDs[g.UUID] {
			same++
		}
	}
	if same > len(e.fleet.GPUs)/10 {
		t.Fatalf("재시딩 후에도 uuid %d/%d 가 동일 — 플릿이 새로 만들어지지 않았다",
			same, len(e.fleet.GPUs))
	}
	if e.scen.victim.UUID == beforeVictim {
		t.Fatalf("victim 이 그대로다 — 시나리오가 재구성되지 않았다")
	}
	// 규모(계약된 GPU 수)는 유지돼야 한다 — 재시딩은 내용 교체이지 규모 변경이 아니다.
	if len(e.fleet.GPUs) != 96 {
		t.Fatalf("재시딩 후 GPU 수 %d, want 96", len(e.fleet.GPUs))
	}
	// 시나리오는 처음(normal)부터 다시 시작한다.
	if e.scen.phase != PhaseNormal || e.scen.cycle != 1 {
		t.Fatalf("재시딩 후 단계=%s 사이클=%d — 처음부터가 아니다", e.scen.phase, e.scen.cycle)
	}
}

func TestReseed_같은_시드는_같은_플릿(t *testing.T) {
	cfg := testConfig(t, "a:클라우드 A:32")
	e1 := NewEngine(newTestDB(t), cfg, func() int64 { return t0 })
	e2 := NewEngine(newTestDB(t), cfg, func() int64 { return t0 })

	for _, e := range []*Engine{e1, e2} {
		if _, err := e.Do(ActionReseed, "admin", map[string]string{"seed": "4242"}); err != nil {
			t.Fatalf("재시딩 실패: %v", err)
		}
		waitReseed(t, e)
	}
	for i := range e1.fleet.GPUs {
		if e1.fleet.GPUs[i].UUID != e2.fleet.GPUs[i].UUID {
			t.Fatalf("같은 시드인데 플릿이 다르다 idx=%d", i)
		}
	}
}

func TestReseed_스냅샷_정합이_유지된다(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:64,b:클라우드 B:32")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	if _, err := e.Do(ActionReseed, "admin", map[string]string{"seed": "777"}); err != nil {
		t.Fatalf("재시딩 실패: %v", err)
	}
	waitReseed(t, e)

	snap := e.Snapshot()
	if snap.Reseeding {
		t.Fatalf("완료 후에도 reseeding=true")
	}
	if snap.Seed != 777 {
		t.Fatalf("스냅샷 시드 = %d, want 777", snap.Seed)
	}
	// 전 화면 정합 계약(총계 = CSP 합)이 재시딩 후에도 유지돼야 한다.
	sum := 0
	for _, c := range snap.Fleet.CSPs {
		sum += c.GPUTotal
	}
	if sum != snap.Fleet.GPUTotal || snap.Fleet.GPUTotal != len(e.fleet.GPUs) {
		t.Fatalf("재시딩 후 정합 붕괴: csp합=%d 총계=%d 실제=%d",
			sum, snap.Fleet.GPUTotal, len(e.fleet.GPUs))
	}
	// 서빙 통계도 새 플릿 기준으로 다시 채워져야 한다.
	if len(snap.Serving) != len(e.fleet.Pools) {
		t.Fatalf("재시딩 후 서빙 통계 %d개, want %d", len(snap.Serving), len(e.fleet.Pools))
	}
}

func TestReseed_시드_미지정이면_매번_다르다(t *testing.T) {
	cfg := testConfig(t, "a:클라우드 A:32")
	now := t0
	e := NewEngine(newTestDB(t), cfg, func() int64 { return now })

	seeds := map[int64]bool{}
	for i := 0; i < 3; i++ {
		now = t0 + int64(i)*60_000 // 클릭 간 시각이 흐른다
		if _, err := e.Do(ActionReseed, "admin", nil); err != nil {
			t.Fatalf("재시딩 %d 실패: %v", i, err)
		}
		waitReseed(t, e)
		seeds[e.cfg.Seed] = true
	}
	if len(seeds) < 3 {
		t.Fatalf("시드 미지정 재시딩이 %d종만 나왔다 — 매번 새 플릿이어야 한다", len(seeds))
	}
}

func TestReseed_잘못된_시드는_거절(t *testing.T) {
	e := NewEngine(newTestDB(t), testConfig(t, "a:클라우드 A:32"), func() int64 { return t0 })
	if _, err := e.Do(ActionReseed, "admin", map[string]string{"seed": "abc"}); err == nil {
		t.Fatalf("비정수 시드가 허용됐다")
	}
}

func TestAdapter_전부_mock(t *testing.T) {
	specs, err := ParseFleetSpec(defaultFleetSpec)
	if err != nil {
		t.Fatalf("플릿 스펙: %v", err)
	}
	// 이 데모는 실제 CSP API 에 붙지 않는다 — "실제 연동" 표기가 하나라도
	// 남으면 화면이 사실과 다른 주장을 하게 된다(사용자 지시).
	for _, s := range specs {
		if s.Adapter != "mock" {
			t.Fatalf("CSP %q 어댑터 = %q, want mock", s.ID, s.Adapter)
		}
	}
}

// waitReseed 는 비동기 재시딩 백필 완료를 기다린다.
func waitReseed(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for e.Reseeding() {
		if time.Now().After(deadline) {
			t.Fatalf("재시딩이 끝나지 않았다")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
