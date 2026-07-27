package demo

// 재시딩 — 버튼 한 번으로 플릿 전체를 새 목데이터로 갈아엎는다(사용자 지시
// 2026-07-27). 시연을 반복할 때 매번 같은 노드·같은 uuid·같은 장애가 나오면
// "녹화 영상"처럼 보이므로, 새 시드로 인벤토리·배치·장애 개체를 통째로 다시
// 만든다.
//
// 동시성: 백필은 시리즈별 시간 오름차순 append 를 요구하므로(head 계약),
// 재시딩 백필이 도는 동안 라이브 틱이 끼어들면 안 된다. 그래서 백필 전체를
// e.mu 하에서 수행하고, 그 사실을 reseeding 플래그로 화면에 알린다 —
// 몇 초간의 정지를 숨기면 "먹통"으로 읽힌다.

import (
	"log/slog"
	"time"
)

// 재시딩 백필 창 — 부팅 백필(30h/1h)보다 짧다. 목적은 "차트가 비지 않는 것"
// 이지 장기 이력 복원이 아니며, 창이 길면 버튼이 수십 초 멎는다.
const (
	reseedAggWindow = 6 * time.Hour
	reseedGPUWindow = 10 * time.Minute
)

// ReseedResult 는 재시딩 결과 요약이다.
type ReseedResult struct {
	Seed      int64 `json:"seed"`
	GPUs      int   `json:"gpus"`
	Nodes     int   `json:"nodes"`
	StartedAt int64 `json:"startedAt"`
}

// Reseeding 은 재시딩 백필이 진행 중인지다 — 화면이 "새 데이터 생성 중"을
// 표시하는 근거.
func (e *Engine) Reseeding() bool { return e.reseeding.Load() }

// Reseed 는 새 시드로 플릿·시나리오를 다시 만들고, 짧은 백필을 비동기로 돌린다.
// seed<=0 이면 현재 시각에서 시드를 파생한다(매번 다른 플릿).
func (e *Engine) Reseed(seed int64) ReseedResult {
	now := e.nowFn()
	if seed <= 0 {
		// 시각 기반 파생 — 연속 클릭에도 다른 값이 나오도록 해시를 한 번 더 건다.
		seed = int64(fnvHash(now, "reseed") % 1_000_000_007)
		if seed <= 0 {
			seed = now % 1_000_000_007
		}
	}

	e.mu.Lock()
	e.cfg.Seed = seed
	e.fleet = BuildFleet(e.cfg.Fleet, seed)
	e.scen = newScenario(e.fleet, e.cfg, now)
	// 파생 상태 초기화 — 이전 플릿의 서빙·SLO 가 새 인벤토리와 섞이면
	// 화면끼리 다른 이야기를 하게 된다.
	e.serving, e.slo = nil, SLOState{}
	e.reseeding.Store(true)
	res := ReseedResult{Seed: seed, GPUs: len(e.fleet.GPUs), Nodes: len(e.fleet.Nodes), StartedAt: now}
	e.mu.Unlock()

	go e.reseedBackfill(now)
	return res
}

// reseedBackfill 은 새 플릿의 최근 이력을 채운다. 전 구간 mu 를 잡는다 —
// 라이브 틱이 끼어들면 같은 시리즈에 현재→과거 순서로 써서 head 계약이 깨진다.
func (e *Engine) reseedBackfill(nowMS int64) {
	defer e.reseeding.Store(false)

	e.mu.Lock()
	defer e.mu.Unlock()

	prevAgg, prevGPU := e.cfg.BackfillAgg, e.cfg.BackfillGPU
	e.cfg.BackfillAgg, e.cfg.BackfillGPU = reseedAggWindow, reseedGPUWindow
	err := e.Backfill(nowMS)
	e.cfg.BackfillAgg, e.cfg.BackfillGPU = prevAgg, prevGPU

	if err != nil {
		// 백필 실패는 이력 공백일 뿐이다 — 라이브 틱이 곧 채운다.
		slog.Error("재시딩 백필 실패 — 라이브 emit 으로 계속한다", "err", err)
		return
	}
	slog.Info("재시딩 완료", "seed", e.cfg.Seed, "gpus", len(e.fleet.GPUs))
}
