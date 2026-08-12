package promql

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
)

// lookbackDelta 는 instant 평가에서 거슬러 올라가 최신 샘플을 찾는 창이다.
// Prometheus 기본값과 같은 5분 — 스크레이프가 한두 번 실패해도 시리즈가
// 사라지지 않게 하되, 그보다 오래 끊긴 시리즈는 없는 것으로 본다. 이 창이
// 없으면 몇 시간 전 죽은 대상이 계속 살아 있는 것처럼 보이고 알림이 해소되지
// 않는다.
const lookbackDelta = 5 * time.Minute

// Engine 은 AST 를 평가한다. 상태가 없어 동시 사용해도 안전하다.
type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

// Eval 은 evalMS 시점 기준으로 식을 평가한다. 반환은 Vector·Matrix·Scalar 중 하나다.
func (e *Engine) Eval(q Queryable, expr Node, evalMS int64) (any, error) {
	switch n := expr.(type) {
	case *NumberLiteral:
		return Scalar(n.Val), nil
	case *VectorSelector:
		return e.evalVectorSelector(q, n, evalMS)
	case *MatrixSelector:
		return e.evalMatrixSelector(q, n, evalMS)
	case *Aggregation:
		return e.evalAggregation(q, n, evalMS)
	case *BinaryExpr:
		return e.evalBinary(q, n, evalMS)
	case *Call:
		return e.evalCall(q, n, evalMS)
	}
	return nil, fmt.Errorf("promql: 평가할 수 없는 노드 %T", expr)
}

func (e *Engine) evalVectorSelector(q Queryable, vs *VectorSelector, evalMS int64) (Vector, error) {
	mint := evalMS - lookbackDelta.Milliseconds()
	querier, closeFn, err := q.Querier(mint, evalMS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()

	out := Vector{}
	for _, s := range querier.Select(vs.Matchers...) {
		it := s.Iterator()
		var (
			lastT int64
			lastV float64
			found bool
		)
		for it.Next() {
			lastT, lastV = it.At()
			found = true
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		out = append(out, Sample{Labels: s.Labels(), T: lastT, V: lastV})
	}
	return out, nil
}

func (e *Engine) evalMatrixSelector(q Queryable, ms *MatrixSelector, evalMS int64) (Matrix, error) {
	mint := evalMS - ms.Range.Milliseconds()
	querier, closeFn, err := q.Querier(mint, evalMS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()

	out := Matrix{}
	for _, s := range querier.Select(ms.VS.Matchers...) {
		it := s.Iterator()
		var pts []Point
		for it.Next() {
			t, v := it.At()
			pts = append(pts, Point{T: t, V: v})
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		if len(pts) == 0 {
			continue
		}
		out = append(out, Series{Labels: s.Labels(), Points: pts})
	}
	return out, nil
}

// groupKey 는 집계 그룹의 식별자와 남길 라벨을 만든다. by 면 나열된 라벨만
// 남기고, without 이면 나열된 것과 __name__ 을 뺀 나머지를 남긴다.
func groupKey(lbls labels.Labels, by bool, grouping []string) (string, labels.Labels) {
	keep := map[string]string{}
	if by {
		for _, g := range grouping {
			if v := lbls.Get(g); v != "" {
				keep[g] = v
			}
		}
	} else {
		drop := map[string]bool{labels.MetricName: true}
		for _, g := range grouping {
			drop[g] = true
		}
		for _, l := range lbls {
			if !drop[l.Name] {
				keep[l.Name] = l.Value
			}
		}
	}

	names := make([]string, 0, len(keep))
	for k := range keep {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, k := range names {
		sb.WriteString(k)
		sb.WriteByte(0)
		sb.WriteString(keep[k])
		sb.WriteByte(1)
	}
	return sb.String(), labels.LabelsFromMap(keep)
}

func (e *Engine) evalAggregation(q Queryable, a *Aggregation, evalMS int64) (Vector, error) {
	inner, err := e.Eval(q, a.Expr, evalMS)
	if err != nil {
		return nil, err
	}
	vec, ok := inner.(Vector)
	if !ok {
		return nil, fmt.Errorf("promql: %s 는 instant vector 에만 쓸 수 있다", a.Op)
	}

	type acc struct {
		lbls  labels.Labels
		sum   float64
		count int
		min   float64
		max   float64
	}
	groups := map[string]*acc{}
	var order []string

	for _, s := range vec {
		key, lbls := groupKey(s.Labels, a.By, a.Grouping)
		g, ok := groups[key]
		if !ok {
			g = &acc{lbls: lbls, min: math.Inf(1), max: math.Inf(-1)}
			groups[key] = g
			order = append(order, key)
		}
		g.sum += s.V
		g.count++
		g.min = math.Min(g.min, s.V)
		g.max = math.Max(g.max, s.V)
	}

	out := make(Vector, 0, len(groups))
	for _, key := range order {
		g := groups[key]
		var v float64
		switch a.Op {
		case "sum":
			v = g.sum
		case "count":
			v = float64(g.count)
		case "avg":
			v = g.sum / float64(g.count)
		case "min":
			v = g.min
		case "max":
			v = g.max
		default:
			return nil, fmt.Errorf("promql: 집계 연산자 %s 를 모른다", a.Op)
		}
		out = append(out, Sample{Labels: g.lbls, T: evalMS, V: v})
	}
	return out, nil
}

// applyOp 는 연산을 적용한다. 비교 연산에서 두 번째 반환값은 "조건을
// 만족하는가" 이고, 산술에서는 언제나 true 다.
func applyOp(op string, l, r float64) (float64, bool, error) {
	switch op {
	case "+":
		return l + r, true, nil
	case "-":
		return l - r, true, nil
	case "*":
		return l * r, true, nil
	case "/":
		return l / r, true, nil
	case "%":
		return math.Mod(l, r), true, nil
	case "==":
		return l, l == r, nil
	case "!=":
		return l, l != r, nil
	case ">":
		return l, l > r, nil
	case "<":
		return l, l < r, nil
	case ">=":
		return l, l >= r, nil
	case "<=":
		return l, l <= r, nil
	}
	return 0, false, fmt.Errorf("promql: 연산자 %s 를 모른다", op)
}

func isComparison(op string) bool {
	switch op {
	case "==", "!=", ">", "<", ">=", "<=":
		return true
	}
	return false
}

func (e *Engine) evalBinary(q Queryable, b *BinaryExpr, evalMS int64) (any, error) {
	lhs, err := e.Eval(q, b.LHS, evalMS)
	if err != nil {
		return nil, err
	}
	rhs, err := e.Eval(q, b.RHS, evalMS)
	if err != nil {
		return nil, err
	}

	ls, lIsScalar := lhs.(Scalar)
	rs, rIsScalar := rhs.(Scalar)

	if lIsScalar && rIsScalar {
		v, keep, err := applyOp(b.Op, float64(ls), float64(rs))
		if err != nil {
			return nil, err
		}
		if isComparison(b.Op) && b.ReturnBool {
			if keep {
				return Scalar(1), nil
			}
			return Scalar(0), nil
		}
		return Scalar(v), nil
	}

	// vector 와 scalar 조합만 지원한다. vector 끼리는 on/ignoring 매칭이
	// 필요한데, 그것 없이 아무 짝이나 맞추면 값이 틀린 채로 나온다.
	var vec Vector
	var scalar float64
	var scalarOnLeft bool
	switch {
	case !lIsScalar && rIsScalar:
		v, ok := lhs.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: 이항 연산의 왼쪽이 instant vector 가 아니다")
		}
		vec, scalar = v, float64(rs)
	case lIsScalar && !rIsScalar:
		v, ok := rhs.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: 이항 연산의 오른쪽이 instant vector 가 아니다")
		}
		vec, scalar, scalarOnLeft = v, float64(ls), true
	default:
		return nil, fmt.Errorf("promql: vector 끼리의 이항 연산은 이번 범위에서 지원하지 않는다(on/ignoring 필요)")
	}

	out := Vector{}
	for _, s := range vec {
		l, r := s.V, scalar
		if scalarOnLeft {
			l, r = scalar, s.V
		}
		v, keep, err := applyOp(b.Op, l, r)
		if err != nil {
			return nil, err
		}
		if isComparison(b.Op) {
			if b.ReturnBool {
				nv := 0.0
				if keep {
					nv = 1
				}
				out = append(out, Sample{Labels: s.Labels, T: s.T, V: nv})
				continue
			}
			if !keep {
				continue
			}
			out = append(out, s)
			continue
		}
		out = append(out, Sample{Labels: dropMetricName(s.Labels), T: s.T, V: v})
	}
	return out, nil
}

// dropMetricName 은 값이 변형된 결과에서 __name__ 을 뗀다. Prometheus 와 같은
// 규칙 — x * 2 의 결과는 더 이상 x 가 아니다.
func dropMetricName(l labels.Labels) labels.Labels {
	m := map[string]string{}
	for _, lb := range l {
		if lb.Name == labels.MetricName {
			continue
		}
		m[lb.Name] = lb.Value
	}
	return labels.LabelsFromMap(m)
}

func (e *Engine) evalCall(q Queryable, c *Call, evalMS int64) (any, error) {
	if fn, ok := rangeFuncs[c.Func]; ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: %s 는 인자 1개를 받는다", c.Func)
		}
		ms, ok := c.Args[0].(*MatrixSelector)
		if !ok {
			return nil, fmt.Errorf("promql: %s 의 인자는 range vector 여야 한다(예: x[5m])", c.Func)
		}
		mat, err := e.evalMatrixSelector(q, ms, evalMS)
		if err != nil {
			return nil, err
		}
		out := Vector{}
		for _, s := range mat {
			v, ok := fn(s, ms.Range)
			if !ok {
				continue
			}
			out = append(out, Sample{Labels: dropMetricName(s.Labels), T: evalMS, V: v})
		}
		return out, nil
	}

	switch c.Func {
	case "absent":
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: absent 는 인자 1개를 받는다")
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		vec, ok := inner.(Vector)
		if !ok {
			return nil, fmt.Errorf("promql: absent 는 instant vector 에만 쓸 수 있다")
		}
		if len(vec) > 0 {
			return Vector{}, nil
		}
		return Vector{{Labels: labels.LabelsFromMap(map[string]string{}), T: evalMS, V: 1}}, nil

	case "clamp_min":
		if len(c.Args) != 2 {
			return nil, fmt.Errorf("promql: clamp_min 은 인자 2개를 받는다")
		}
		return e.applyScalarArg(q, c, evalMS, math.Max)

	case "day_of_week":
		return Scalar(float64(time.UnixMilli(evalMS).UTC().Weekday())), nil

	case "hour":
		return Scalar(float64(time.UnixMilli(evalMS).UTC().Hour())), nil

	case "time":
		return Scalar(float64(evalMS) / 1000), nil

	case "vector":
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: vector 는 인자 1개를 받는다")
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		s, ok := inner.(Scalar)
		if !ok {
			return nil, fmt.Errorf("promql: vector 의 인자는 스칼라여야 한다")
		}
		return Vector{{Labels: labels.LabelsFromMap(map[string]string{}), T: evalMS, V: float64(s)}}, nil
	}

	if fn, ok := instantFuncs[c.Func]; ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("promql: %s 는 인자 1개를 받는다", c.Func)
		}
		inner, err := e.Eval(q, c.Args[0], evalMS)
		if err != nil {
			return nil, err
		}
		switch v := inner.(type) {
		case Scalar:
			return Scalar(fn(float64(v))), nil
		case Vector:
			out := Vector{}
			for _, s := range v {
				out = append(out, Sample{Labels: dropMetricName(s.Labels), T: s.T, V: fn(s.V)})
			}
			return out, nil
		}
		return nil, fmt.Errorf("promql: %s 에 쓸 수 없는 인자 타입", c.Func)
	}

	return nil, fmt.Errorf("promql: 함수 %s 는 이번 범위에서 지원하지 않는다", c.Func)
}

// applyScalarArg 는 `f(vector, scalar)` 형태를 처리한다.
func (e *Engine) applyScalarArg(q Queryable, c *Call, evalMS int64, fn func(a, b float64) float64) (any, error) {
	inner, err := e.Eval(q, c.Args[0], evalMS)
	if err != nil {
		return nil, err
	}
	argN, err := e.Eval(q, c.Args[1], evalMS)
	if err != nil {
		return nil, err
	}
	sc, ok := argN.(Scalar)
	if !ok {
		return nil, fmt.Errorf("promql: %s 의 두 번째 인자는 스칼라여야 한다", c.Func)
	}
	vec, ok := inner.(Vector)
	if !ok {
		return nil, fmt.Errorf("promql: %s 의 첫 인자는 instant vector 여야 한다", c.Func)
	}
	out := Vector{}
	for _, s := range vec {
		out = append(out, Sample{Labels: s.Labels, T: s.T, V: fn(s.V, float64(sc))})
	}
	return out, nil
}
