package promql

import (
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
)

// Node 는 파싱된 식의 한 조각이다. 평가기는 이 트리를 따라 내려간다.
type Node interface{ node() }

// VectorSelector 는 `name{a="b"}` — 한 시점의 시리즈 집합을 고른다.
type VectorSelector struct {
	Name     string
	Matchers []*labels.Matcher
}

// MatrixSelector 는 `name{...}[5m]` — 구간의 시리즈를 고른다. range 함수의
// 인자로만 올 수 있다.
type MatrixSelector struct {
	VS    *VectorSelector
	Range time.Duration
}

// Call 은 `f(...)` 형태의 함수 호출이다.
type Call struct {
	Func string
	Args []Node
}

// Aggregation 은 `sum by (node) (expr)` 같은 집계다. By 가 false 이고
// Grouping 이 비어 있지 않으면 without 이다.
type Aggregation struct {
	Op       string
	Expr     Node
	By       bool
	Grouping []string
}

// BinaryExpr 는 이항 연산이다. ReturnBool 은 `> bool 0` 처럼 비교 결과를
// 0/1 로 바꾸라는 modifier.
type BinaryExpr struct {
	Op         string
	LHS, RHS   Node
	ReturnBool bool
}

// NumberLiteral 은 스칼라 상수다.
type NumberLiteral struct{ Val float64 }

func (*VectorSelector) node() {}
func (*MatrixSelector) node() {}
func (*Call) node()           {}
func (*Aggregation) node()    {}
func (*BinaryExpr) node()     {}
func (*NumberLiteral) node()  {}
