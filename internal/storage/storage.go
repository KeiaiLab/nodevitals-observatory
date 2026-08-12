// Package storage 는 tsdb 를 promql 이 요구하는 모양으로 이어 붙인다.
//
// 이 패키지가 존재하는 이유는 방향 때문이다. promql 은 저장 구현을 몰라야
// 하고(질의 언어가 저장 형식 변화에 끌려다니지 않도록), tsdb 는 질의 언어를
// 몰라야 한다(저장이 특정 질의 모델에 묶이지 않도록). 둘 다 상대를 모르면
// 누군가는 알아야 하는데, 그게 여기다 — 조립은 한 곳에 모아 둔다.
package storage

import (
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/promql"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// New 는 DB 를 promql 이 질의할 수 있는 통로로 감싼다.
func New(db *tsdb.DB) promql.Queryable { return &queryable{db: db} }

type queryable struct{ db *tsdb.DB }

func (q *queryable) Querier(mint, maxt int64) (promql.Querier, func() error, error) {
	tq, closeFn, err := q.db.Querier(mint, maxt)
	if err != nil {
		return nil, nil, err
	}
	return &querier{q: tq}, closeFn, nil
}

type querier struct{ q *tsdb.Querier }

func (q *querier) Select(ms ...*labels.Matcher) []promql.StorageSeries {
	src := q.q.Select(ms...)
	out := make([]promql.StorageSeries, 0, len(src))
	for _, s := range src {
		out = append(out, series{s: s})
	}
	return out
}

// series 는 tsdb.Series 를 promql.StorageSeries 로 옮긴다. 메서드 집합이
// 같더라도 Go 는 슬라이스 원소 타입을 자동으로 바꿔주지 않으므로 한 겹이 든다.
type series struct{ s tsdb.Series }

func (a series) Labels() labels.Labels           { return a.s.Labels() }
func (a series) Iterator() promql.SampleIterator { return a.s.Iterator() }
