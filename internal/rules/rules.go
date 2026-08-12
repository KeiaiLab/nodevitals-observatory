// Package rules 는 알림 룰을 주기적으로 평가하고 그 상태를 관리한다.
//
// 상태가 이 패키지의 존재 이유다. 조건이 참인지는 promql 이 답하지만, "언제부터
// 참이었나"·"이미 알린 것인가"는 평가 사이에 기억해야 알 수 있다.
package rules

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
)

// State 는 알림 하나의 생애다. Prometheus 와 같은 3단계를 쓴다.
type State int

const (
	StateInactive State = iota
	StatePending        // 조건은 참이지만 for 가 아직 안 찼다
	StateFiring
)

func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateFiring:
		return "firing"
	default:
		return "inactive"
	}
}

// Alert 는 활성 상태인 알림 하나다.
type Alert struct {
	Labels      map[string]string
	Annotations map[string]string
	State       State
	// ActiveAtMS 는 조건이 *연속으로* 참이기 시작한 시각이다. 조건이 한 번
	// 풀리면 다시 찍힌다 — 그래야 짧은 깜빡임이 쌓여 firing 이 되지 않는다.
	ActiveAtMS int64
	Value      float64
}

// Rule 은 알림 룰 하나와 그 활성 상태다.
type Rule struct {
	Name        string
	Expr        promql.Node
	exprText    string
	For         time.Duration
	Labels      map[string]string
	Annotations map[string]string

	// active 는 라벨 지문 → 알림. 평가 사이에 유지되어야 for 와 중복 발송
	// 판단이 성립한다.
	active map[string]*Alert
}

// NewRule 은 식을 파싱해 룰을 만든다. 파싱 실패는 여기서 걸러진다 — 평가할
// 때마다 실패하는 룰을 안고 도는 것보다 만들 때 거절하는 편이 낫다.
func NewRule(name, expr string, forDur time.Duration, lbls, anns map[string]string) (*Rule, error) {
	node, err := promql.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("rules: 룰 %q 의 식을 읽을 수 없다: %w", name, err)
	}
	if lbls == nil {
		lbls = map[string]string{}
	}
	if anns == nil {
		anns = map[string]string{}
	}
	return &Rule{
		Name: name, Expr: node, exprText: expr, For: forDur,
		Labels: lbls, Annotations: anns,
		active: map[string]*Alert{},
	}, nil
}

// Expression 은 룰의 원문 식이다(API 노출용).
func (r *Rule) Expression() string { return r.exprText }

// Active 는 현재 pending 또는 firing 인 알림들이다.
func (r *Rule) Active() []*Alert {
	out := make([]*Alert, 0, len(r.active))
	for _, a := range r.active {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return fingerprint(out[i].Labels) < fingerprint(out[j].Labels) })
	return out
}

// Eval 은 evalMS 시점에 룰을 평가하고 활성 알림 목록을 낸다. 조건이 풀린
// 알림은 목록에서도 내부 상태에서도 사라진다 — 남겨 두면 해소된 알림이
// 계속 울린다.
func (r *Rule) Eval(q promql.Queryable, evalMS int64) []*Alert {
	res, err := promql.NewEngine().Eval(q, r.Expr, evalMS)
	if err != nil {
		// 평가 실패는 "조건이 거짓" 과 다르다. 기존 상태를 건드리지 않고
		// 그대로 두어, 일시적 실패가 알림을 해소시키지 않게 한다.
		return r.Active()
	}
	vec, ok := res.(promql.Vector)
	if !ok {
		return r.Active()
	}

	seen := map[string]bool{}
	for _, s := range vec {
		lbls := map[string]string{"alertname": r.Name}
		for _, l := range s.Labels {
			lbls[l.Name] = l.Value
		}
		// 룰 라벨이 마지막에 온다 — severity 같은 값은 룰이 정한다.
		for k, v := range r.Labels {
			lbls[k] = v
		}

		fp := fingerprint(lbls)
		seen[fp] = true

		a, exists := r.active[fp]
		if !exists {
			a = &Alert{Labels: lbls, Annotations: r.Annotations, ActiveAtMS: evalMS, State: StatePending}
			r.active[fp] = a
		}
		a.Value = s.V
		if evalMS-a.ActiveAtMS >= r.For.Milliseconds() {
			a.State = StateFiring
		}
	}

	// 이번 평가에서 조건을 만족하지 않은 알림은 지운다.
	for fp := range r.active {
		if !seen[fp] {
			delete(r.active, fp)
		}
	}
	return r.Active()
}

// fingerprint 는 라벨셋의 안정적인 키다. 정렬해서 이어 붙이므로 map 순회
// 순서에 흔들리지 않는다.
func fingerprint(lbls map[string]string) string {
	keys := make([]string, 0, len(lbls))
	for k := range lbls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte(0)
		sb.WriteString(lbls[k])
		sb.WriteByte(1)
	}
	return sb.String()
}
