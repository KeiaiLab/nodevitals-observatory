package rules

import (
	"sync"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
)

type recorder struct {
	mu    sync.Mutex
	sends [][]Notification
}

func (r *recorder) Notify(ns []Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]Notification, len(ns))
	copy(cp, ns)
	r.sends = append(r.sends, cp)
	return nil
}

func (r *recorder) all() []Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Notification
	for _, s := range r.sends {
		out = append(out, s...)
	}
	return out
}

func mgrWith(t *testing.T, q promql.Queryable, rec *recorder, rules ...*Rule) *Manager {
	t.Helper()
	g := &Group{Name: "g", Interval: time.Second, Rules: rules}
	return NewManager([]*Group{g}, q, rec)
}

// firing 이 된 알림만 나간다 — pending 은 아직 사람에게 알릴 단계가 아니다.
func TestManagerSendsOnlyFiring(t *testing.T) {
	q := newQ(t, sample{"x", nil, 0, 5})
	rec := &recorder{}
	r := mustRule(t, "A", `x > 3`, time.Minute) // for 1분 → 첫 평가는 pending
	m := mgrWith(t, q, rec, r)

	m.EvalOnce(0)
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("pending 인데 발송됐다: %+v", got)
	}

	m.EvalOnce(60_000) // for 충족
	got := rec.all()
	if len(got) != 1 || got[0].Status != "firing" {
		t.Fatalf("발송 %+v, want firing 하나", got)
	}
}

// 해소되면 resolved 를 한 번 보낸다 — 보내지 않으면 사람이 계속 대응한다.
func TestManagerSendsResolved(t *testing.T) {
	up := newQ(t, sample{"x", nil, 0, 5})
	down := newQ(t, sample{"x", nil, 0, 1})
	rec := &recorder{}
	r := mustRule(t, "A", `x > 3`, 0)

	g := &Group{Name: "g", Interval: time.Second, Rules: []*Rule{r}}
	m := NewManager([]*Group{g}, up, rec)
	m.EvalOnce(0)

	m.SetQueryable(down)
	m.EvalOnce(1000)

	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("발송 %d 건, want 2 (firing + resolved): %+v", len(got), got)
	}
	if got[1].Status != "resolved" {
		t.Errorf("두 번째 발송 status=%q, want resolved", got[1].Status)
	}
}

// 같은 알림이 계속 firing 이어도 반복해서 보내지 않는다 — 반복 억제는
// 발송기가 하지만, 매니저가 매 평가마다 같은 것을 넘기면 그 판단이 흐려진다.
func TestManagerDoesNotResendUnchangedFiring(t *testing.T) {
	q := newQ(t, sample{"x", nil, 0, 5})
	rec := &recorder{}
	r := mustRule(t, "A", `x > 3`, 0)
	m := mgrWith(t, q, rec, r)

	m.EvalOnce(0)
	m.EvalOnce(1000)
	m.EvalOnce(2000)

	if got := rec.all(); len(got) != 1 {
		t.Errorf("발송 %d 건, want 1 (상태가 안 바뀌면 다시 보내지 않는다): %+v", len(got), got)
	}
}

// 발송이 실패해도 평가는 계속돼야 한다 — 수신 측 장애가 관측을 멈추면 안 된다.
type failingSink struct{ calls int }

func (f *failingSink) Notify([]Notification) error { f.calls++; return errSink }

var errSink = &sinkError{}

type sinkError struct{}

func (e *sinkError) Error() string { return "보낼 수 없다" }

func TestManagerKeepsEvaluatingWhenSinkFails(t *testing.T) {
	q := newQ(t, sample{"x", nil, 0, 5})
	f := &failingSink{}
	r := mustRule(t, "A", `x > 3`, 0)
	g := &Group{Name: "g", Interval: time.Second, Rules: []*Rule{r}}
	m := NewManager([]*Group{g}, q, f)

	m.EvalOnce(0)
	m.EvalOnce(1000)

	if len(r.Active()) != 1 {
		t.Errorf("발송 실패 후 활성 알림 %d, want 1 (평가는 계속된다)", len(r.Active()))
	}
}

// API 노출용 스냅샷.
func TestManagerSnapshotExposesRuleState(t *testing.T) {
	q := newQ(t, sample{"x", nil, 0, 5})
	rec := &recorder{}
	r := mustRule(t, "A", `x > 3`, 0)
	m := mgrWith(t, q, rec, r)
	m.EvalOnce(0)

	snap := m.Snapshot()
	if len(snap) != 1 || len(snap[0].Rules) != 1 {
		t.Fatalf("스냅샷 %+v", snap)
	}
	rs := snap[0].Rules[0]
	if rs.Name != "A" || rs.State != "firing" {
		t.Errorf("룰 상태 %+v", rs)
	}
	if rs.Expression == "" {
		t.Error("식이 비어 있다")
	}
}
