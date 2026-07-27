package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

	// serving/slo 는 마지막 틱의 파생 상태다 — 스냅샷이 같은 값을 재계산하지
	// 않고 그대로 내보내, 화면 수치와 시계열이 항상 일치하게 한다.
	serving []ServingStats
	slo     SLOState

	// mu 는 scen(상태머신·링버퍼)과 fleet 의 가변 필드(victim uuid 교체 등)를
	// 보호한다 — emit 틱과 HTTP 핸들러(Snapshot/Do)가 경합하는 유일한 지점.
	mu    sync.Mutex
	ready atomic.Bool
	// reseeding 은 재시딩 백필 진행 중 표시다 — 그동안 mu 가 잡혀 있어
	// 상태 조회가 잠깐 멎으므로, 화면이 이유를 말할 수 있어야 한다.
	reseeding atomic.Bool
	// refillingPast 는 "이미 지나간 구간을 다시 채우는 중"이다 — 정체성이
	// 유지되는 시리즈의 역행 거절을 결함이 아닌 정상으로 읽는 근거(appendSample).
	// mu 하에서만 쓰고 읽는다.
	refillingPast bool
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

// errReseedInProgress 는 재시딩 중 재요청을 막는다 — 백필이 mu 를 잡고 있어
// 두 번째 요청은 어차피 대기하고, 중첩 실행은 이력을 뒤섞는다.
var errReseedInProgress = errors.New("재시딩이 이미 진행 중이다 — 완료 후 다시 시도")

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
	// 재시딩은 시나리오가 아니라 엔진 전체를 갈아엎는다 — Scenario.Apply 로
	// 보내면 자기 자신을 교체하는 꼴이라 별도 경로로 처리한다.
	if action == ActionReseed {
		// 시드 파싱이 선점보다 먼저다 — 잘못된 시드가 선점을 소모하면 아무도
		// 재시딩하지 않는데 플래그만 켜진 채 남는다.
		var seed int64
		if v := params["seed"]; v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return ActionResult{}, fmt.Errorf("시드는 정수여야 한다: %q", v)
			}
			seed = n
		}
		res, ok := e.Reseed(seed)
		if !ok {
			return ActionResult{}, errReseedInProgress
		}
		return ActionResult{
			Applied: true, Phase: string(PhaseNormal), At: res.StartedAt,
			Message: fmt.Sprintf("새 목데이터 생성 중 — 시드 %d · GPU %d장 · 노드 %d대",
				res.Seed, res.GPUs, res.Nodes),
		}, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.nowFn()
	msg, err := e.scen.Apply(action, actor, params, t)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{Applied: true, Phase: string(e.scen.phase), At: t, Message: msg}, nil
}
