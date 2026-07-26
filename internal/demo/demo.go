package demo

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// Appender 는 엔진이 필요로 하는 tsdb 표면이다 — *tsdb.DB 가 충족한다.
// 인터페이스로 좁힌 이유는 테스트에서 실 tsdb 왕복과 계수 스텁을 둘 다 쓰기
// 위함이다.
type Appender interface {
	Append(lset tsdb.Labels, t int64, v float64) error
}

// emit 주기 — TimeScale 과 무관하게 고정이다. instant query 의 5분 평가창
// 계약상 데이터 공급 주기는 시나리오 배속과 분리돼야 한다(config.go 참조).
const (
	fastTick = 15 * time.Second
	slowTick = 60 * time.Second
)

// Engine 은 데모 모드의 본체다 — 플릿 신호 emit 루프 + 시나리오 상태머신 +
// 상태 스냅샷/액션 표면.
type Engine struct {
	db    Appender
	cfg   Config
	fleet *Fleet
	scen  *Scenario
	nowFn func() int64

	// mu 는 scen(상태머신·링버퍼)과 fleet 의 가변 필드(victim uuid 교체 등)를
	// 보호한다 — emit 틱과 HTTP 핸들러(Snapshot/Do)가 경합하는 유일한 지점.
	mu    sync.Mutex
	ready atomic.Bool
}

// NewEngine 은 결정론 플릿·시나리오를 구성한다. nowFn 은 wall clock 주입점
// (cmd 전용 예외)이며, 테스트는 고정 epoch 함수를 넣는다.
func NewEngine(db Appender, cfg Config, nowFn func() int64) *Engine {
	fleet := BuildFleet(cfg.Fleet, cfg.Seed)
	return &Engine{
		db:    db,
		cfg:   cfg,
		fleet: fleet,
		scen:  newScenario(fleet, cfg, nowFn()),
		nowFn: nowFn,
	}
}

// Ready 는 백필 완료 여부다 — main 의 /readyz 판정에 배선된다.
func (e *Engine) Ready() bool { return e.ready.Load() }

// Run 은 백필 후 fast/slow 틱 루프를 돈다. ctx 취소로 종료한다.
func (e *Engine) Run(ctx context.Context) {
	start := e.nowFn()
	slog.Info("데모 백필 시작",
		"gpus", len(e.fleet.GPUs), "nodes", len(e.fleet.Nodes),
		"aggWindow", e.cfg.BackfillAgg, "gpuWindow", e.cfg.BackfillGPU)

	e.mu.Lock()
	err := e.Backfill(start)
	e.mu.Unlock()
	if err != nil {
		// 백필 실패는 데모 품질 저하일 뿐 치명이 아니다 — 라이브 emit 은
		// 계속 시도한다(빈 과거 + 현재 데이터라도 시연은 성립).
		slog.Error("데모 백필 실패 — 라이브 emit 만으로 계속한다", "err", err)
	}
	e.ready.Store(true)
	slog.Info("데모 엔진 준비 완료", "backfillTook", time.Duration(e.nowFn()-start)*time.Millisecond)

	fast := time.NewTicker(fastTick)
	defer fast.Stop()
	slow := time.NewTicker(slowTick)
	defer slow.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
			e.mu.Lock()
			t := e.nowFn()
			e.scen.Advance(t)
			if err := e.emitFast(t); err != nil {
				slog.Warn("데모 fast emit 실패", "err", err)
			}
			e.mu.Unlock()
		case <-slow.C:
			e.mu.Lock()
			if err := e.emitSlow(e.nowFn()); err != nil {
				slog.Warn("데모 slow emit 실패", "err", err)
			}
			e.mu.Unlock()
		}
	}
}

// Snapshot 은 /api/v1/demo/state 응답용 일관 복사본이다.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buildSnapshot(e.nowFn())
}

// ActionResult 는 액션 응답이다.
type ActionResult struct {
	Applied bool   `json:"applied"`
	Phase   string `json:"phase"`
	At      int64  `json:"at"`
	Message string `json:"message"`
}

// Do 는 운영자 액션을 적용한다. 단계 불일치는 error 로 나가고 핸들러가 409 로
// 옮긴다.
func (e *Engine) Do(action Action, actor string, params map[string]string) (ActionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.nowFn()
	msg, err := e.scen.Apply(action, actor, params, t)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Applied: true, Phase: string(e.scen.phase), At: t, Message: msg}, nil
}
