package promql

import "github.com/KeiaiLab/nodevitals-observatory/internal/labels"

// 평가기가 저장 계층에 요구하는 것은 이 파일이 전부다. 구현이 아니라 여기의
// 인터페이스에 의존하므로, promql 은 tsdb 를 알지 못한 채 완성되고 테스트된다
// — 반대로 두면 질의 언어가 저장 형식의 변화에 끌려다닌다.

// SampleIterator 는 한 시리즈의 샘플을 시간 오름차순으로 낸다.
type SampleIterator interface {
	Next() bool
	At() (int64, float64)
	Err() error
}

// StorageSeries 는 저장 계층이 돌려주는 시리즈 하나다. promql.Series 는 평가
// *결과* 이고 이쪽은 *원천* 이다 — 둘은 다른 것이라 이름을 나눈다.
type StorageSeries interface {
	Labels() labels.Labels
	Iterator() SampleIterator
}

// Querier 는 한 시간 창에 대한 조회다.
type Querier interface {
	Select(ms ...*labels.Matcher) []StorageSeries
}

// Queryable 은 평가기가 데이터를 얻는 통로다. 닫기 함수는 조회가 잡은 자원을
// 놓는다.
type Queryable interface {
	Querier(mint, maxt int64) (Querier, func() error, error)
}
