package rules

import (
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
	"github.com/KeiaiLab/nodevitals-observatory/internal/storage"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

func newQ(t *testing.T, samples ...sample) promql.Queryable {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, s := range samples {
		m := map[string]string{labels.MetricName: s.name}
		for k, v := range s.kv {
			m[k] = v
		}
		if err := db.Append(labels.LabelsFromMap(m), s.t, s.v); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	return storage.New(db)
}

type sample struct {
	name string
	kv   map[string]string
	t    int64
	v    float64
}

func mustRule(t *testing.T, alert, expr string, forDur time.Duration) *Rule {
	t.Helper()
	r, err := NewRule(alert, expr, forDur, map[string]string{"severity": "critical"}, nil)
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	return r
}

// for 가 0 이면 조건을 만족한 즉시 firing 이다.
func TestRuleFiresImmediatelyWhenForIsZero(t *testing.T) {
	q := newQ(t, sample{"x", map[string]string{"node": "a"}, 1000, 5})
	r := mustRule(t, "HighX", `x > 3`, 0)

	alerts := r.Eval(q, 1000)
	if len(alerts) != 1 {
		t.Fatalf("알림 %d 개, want 1: %+v", len(alerts), alerts)
	}
	if alerts[0].State != StateFiring {
		t.Errorf("state=%v, want firing", alerts[0].State)
	}
}

// for 가 있으면 조건을 만족해도 그 시간이 지나기 전에는 pending 이다. 이
// 경계를 잘못 잡으면 순간적인 스파이크가 전부 알림이 된다.
func TestRulePendingUntilForElapses(t *testing.T) {
	q := newQ(t, sample{"x", nil, 1000, 5})
	r := mustRule(t, "HighX", `x > 3`, 2*time.Minute)

	a1 := r.Eval(q, 1000)
	if len(a1) != 1 || a1[0].State != StatePending {
		t.Fatalf("첫 평가 %+v, want pending 하나", a1)
	}

	// 1분 뒤 — 아직 for 미충족
	a2 := r.Eval(q, 1000+60_000)
	if len(a2) != 1 || a2[0].State != StatePending {
		t.Fatalf("1분 뒤 %+v, want 여전히 pending", a2)
	}

	// 2분 뒤 — 충족
	a3 := r.Eval(q, 1000+120_000)
	if len(a3) != 1 || a3[0].State != StateFiring {
		t.Fatalf("2분 뒤 %+v, want firing", a3)
	}
}

// 조건이 풀리면 상태가 사라져야 한다. 남아 있으면 해소된 알림이 계속 울린다.
func TestRuleClearsWhenConditionNoLongerHolds(t *testing.T) {
	q := newQ(t, sample{"x", nil, 1000, 5})
	r := mustRule(t, "HighX", `x > 3`, 0)
	if got := r.Eval(q, 1000); len(got) != 1 {
		t.Fatalf("첫 평가 %+v, want 1", got)
	}

	// 값이 임계 아래로 내려간 새 저장소
	q2 := newQ(t, sample{"x", nil, 2000, 1})
	got := r.Eval(q2, 2000)
	if len(got) != 0 {
		t.Errorf("해소 후 %+v, want 빈 목록", got)
	}
	if n := len(r.Active()); n != 0 {
		t.Errorf("활성 알림 %d 개, want 0", n)
	}
}

// pending 중에 조건이 풀렸다가 다시 만족하면 for 가 처음부터 다시 세야 한다
// — 이어서 세면 짧은 깜빡임이 쌓여 firing 이 된다.
func TestRuleRestartsForAfterConditionBreaks(t *testing.T) {
	up := newQ(t, sample{"x", nil, 0, 5})
	down := newQ(t, sample{"x", nil, 0, 1})
	r := mustRule(t, "HighX", `x > 3`, 2*time.Minute)

	r.Eval(up, 0)              // pending 시작
	r.Eval(down, 60_000)       // 1분 뒤 해소
	got := r.Eval(up, 120_000) // 2분 뒤 다시 만족 — 여기서 firing 이면 안 된다
	if len(got) != 1 || got[0].State != StatePending {
		t.Errorf("재시작 후 %+v, want pending (for 를 처음부터 다시 센다)", got)
	}
}

// 라벨셋이 다르면 각각 독립된 알림이다.
func TestRuleTracksAlertsPerLabelSet(t *testing.T) {
	q := newQ(t,
		sample{"x", map[string]string{"node": "a"}, 1000, 5},
		sample{"x", map[string]string{"node": "b"}, 1000, 9},
	)
	r := mustRule(t, "HighX", `x > 3`, 0)
	got := r.Eval(q, 1000)
	if len(got) != 2 {
		t.Fatalf("알림 %d 개, want 2 (라벨셋마다 하나)", len(got))
	}
}

// 룰 라벨은 결과 라벨을 덮어쓴다 — severity 같은 것을 룰이 정하기 때문이다.
func TestRuleLabelsAreAttached(t *testing.T) {
	q := newQ(t, sample{"x", map[string]string{"node": "a"}, 1000, 5})
	r := mustRule(t, "HighX", `x > 3`, 0)
	got := r.Eval(q, 1000)
	if got[0].Labels["severity"] != "critical" {
		t.Errorf("severity 라벨이 없다: %+v", got[0].Labels)
	}
	if got[0].Labels["node"] != "a" {
		t.Errorf("결과 라벨이 유지되지 않았다: %+v", got[0].Labels)
	}
	if got[0].Labels["alertname"] != "HighX" {
		t.Errorf("alertname 이 붙지 않았다: %+v", got[0].Labels)
	}
}

// 식이 틀리면 룰 생성 시점에 거절한다 — 평가 때마다 실패하는 룰을 안고
// 도는 것보다 낫다.
func TestNewRuleRejectsBadExpression(t *testing.T) {
	if _, err := NewRule("Bad", `topk(3, x)`, 0, nil, nil); err == nil {
		t.Error("미지원 문법이 룰로 만들어졌다")
	}
}
