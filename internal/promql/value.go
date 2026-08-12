// Package promql 는 PromQL 서브셋의 파싱과 평가를 담당한다. 데이터는
// labels.Querier 에서만 읽고, 저장 구조는 알지 못한다.
package promql

import "github.com/KeiaiLab/nodevitals-observatory/internal/labels"

// Point 는 한 시점의 값이다. T 는 unix 밀리초.
type Point struct {
	T int64
	V float64
}

// Sample 은 instant vector 의 원소 — 라벨셋 하나당 시점 하나.
type Sample struct {
	Labels labels.Labels
	T      int64
	V      float64
}

// Vector 는 같은 시점의 샘플 집합이다(PromQL instant vector).
type Vector []Sample

// Series 는 range vector 의 원소 — 라벨셋 하나의 시간 구간.
type Series struct {
	Labels labels.Labels
	Points []Point
}

// Matrix 는 range vector 다.
type Matrix []Series

// Scalar 는 라벨이 없는 단일 값이다.
type Scalar float64
