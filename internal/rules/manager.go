package rules

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
)

// Notification 은 밖으로 나갈 알림 하나다. 발송 구현(webhook·그 밖)을 이
// 패키지가 알지 못하도록 자체 타입을 쓴다 — 룰 평가가 전송 방식에 묶이면
// 채널을 바꿀 때마다 평가기를 건드리게 된다.
type Notification struct {
	Labels      map[string]string
	Annotations map[string]string
	Status      string // "firing" | "resolved"
	Value       float64
}

// Sink 는 알림을 받아 어딘가로 보낸다.
type Sink interface {
	Notify([]Notification) error
}

// RuleState·GroupState 는 /api/v1/rules 응답의 원천이다.
type RuleState struct {
	Name       string
	Expression string
	State      string
	LastError  string
	Alerts     []Notification
}

type GroupState struct {
	Name  string
	Rules []RuleState
}

// Manager 는 그룹들을 주기적으로 평가하고, 상태가 바뀐 알림만 내보낸다.
type Manager struct {
	mu     sync.Mutex
	groups []*Group
	q      promql.Queryable
	sink   Sink
	log    *slog.Logger

	// firing 은 직전 평가에서 발송된 알림들이다. 이것과 비교해 "새로 뜬 것"
	// 과 "풀린 것" 만 내보낸다 — 매 평가마다 같은 알림을 넘기면 반복 억제
	// 판단이 발송기 쪽으로만 몰려 흐려진다.
	firing map[string]Notification
}

func NewManager(groups []*Group, q promql.Queryable, sink Sink) *Manager {
	return &Manager{
		groups: groups, q: q, sink: sink,
		log:    slog.Default(),
		firing: map[string]Notification{},
	}
}

// SetQueryable 은 데이터 원천을 바꾼다(테스트·재구성용).
func (m *Manager) SetQueryable(q promql.Queryable) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.q = q
}

// SetLogger 는 로거를 바꾼다.
func (m *Manager) SetLogger(l *slog.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log = l
}

// EvalOnce 는 모든 그룹을 한 번 평가하고 변화분을 내보낸다.
func (m *Manager) EvalOnce(evalMS int64) {
	m.mu.Lock()
	q, sink, log := m.q, m.sink, m.log
	groups := m.groups
	m.mu.Unlock()

	current := map[string]Notification{}
	for _, g := range groups {
		for _, r := range g.Rules {
			for _, a := range r.Eval(q, evalMS) {
				if a.State != StateFiring {
					continue // pending 은 아직 사람에게 알릴 단계가 아니다
				}
				n := Notification{
					Labels: a.Labels, Annotations: a.Annotations,
					Status: "firing", Value: a.Value,
				}
				current[fingerprint(a.Labels)] = n
			}
		}
	}

	m.mu.Lock()
	var out []Notification
	for fp, n := range current {
		if _, already := m.firing[fp]; !already {
			out = append(out, n)
		}
	}
	for fp, prev := range m.firing {
		if _, still := current[fp]; !still {
			r := prev
			r.Status = "resolved"
			out = append(out, r)
		}
	}
	m.firing = current
	m.mu.Unlock()

	if len(out) == 0 {
		return
	}
	// 발송 실패는 평가를 멈추지 않는다 — 수신 측 장애가 관측까지 세우면
	// 정작 복구에 필요한 데이터가 끊긴다.
	if err := sink.Notify(out); err != nil {
		log.Warn("알림 발송 실패 — 평가는 계속한다", "건수", len(out), "err", err)
	}
}

// Run 은 가장 짧은 그룹 주기로 평가를 반복한다. ctx 가 끝나면 돌아온다.
func (m *Manager) Run(ctx context.Context) {
	interval := m.tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.EvalOnce(now.UnixMilli())
		}
	}
}

// tick 은 그룹 주기 중 가장 짧은 것이다. 그룹별 주기를 따로 돌리는 것은
// 룰 수가 늘고 주기가 실제로 갈릴 때 하면 된다.
func (m *Manager) tick() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	shortest := time.Duration(0)
	for _, g := range m.groups {
		if g.Interval <= 0 {
			continue
		}
		if shortest == 0 || g.Interval < shortest {
			shortest = g.Interval
		}
	}
	if shortest == 0 {
		shortest = 30 * time.Second
	}
	return shortest
}

// Snapshot 은 현재 룰 상태다. /api/v1/rules 가 이것을 그대로 낸다.
func (m *Manager) Snapshot() []GroupState {
	m.mu.Lock()
	groups := m.groups
	m.mu.Unlock()

	out := make([]GroupState, 0, len(groups))
	for _, g := range groups {
		gs := GroupState{Name: g.Name}
		for _, r := range g.Rules {
			rs := RuleState{Name: r.Name, Expression: r.Expression(), State: "inactive"}
			for _, a := range r.Active() {
				rs.Alerts = append(rs.Alerts, Notification{
					Labels: a.Labels, Annotations: a.Annotations,
					Status: a.State.String(), Value: a.Value,
				})
				// firing 이 하나라도 있으면 룰 상태는 firing 이다.
				if a.State == StateFiring {
					rs.State = "firing"
				} else if rs.State != "firing" {
					rs.State = "pending"
				}
			}
			gs.Rules = append(gs.Rules, rs)
		}
		out = append(out, gs)
	}
	return out
}
