package promql

import "github.com/KeiaiLab/nodevitals-observatory/internal/labels"

// 평가기가 요구하는 것은 storage.go 의 인터페이스뿐이므로, 테스트는 저장
// 엔진 없이 이 가짜 하나로 끝난다 — 계층을 나눈 이득이 여기서 나온다.

type fakeIter struct {
	pts []Point
	i   int
}

func (it *fakeIter) Next() bool {
	it.i++
	return it.i <= len(it.pts)
}
func (it *fakeIter) At() (int64, float64) { p := it.pts[it.i-1]; return p.T, p.V }
func (it *fakeIter) Err() error           { return nil }

type fakeSeries struct {
	lbls labels.Labels
	pts  []Point
}

func (s fakeSeries) Labels() labels.Labels    { return s.lbls }
func (s fakeSeries) Iterator() SampleIterator { return &fakeIter{pts: s.pts} }

type fakeQuerier struct {
	mint, maxt int64
	series     []fakeSeries
}

// Select 는 매처를 만족하고 [mint,maxt] 에 점이 있는 시리즈를 낸다 — 실제
// 저장 계층이 하는 일과 같은 계약이다.
func (q *fakeQuerier) Select(ms ...*labels.Matcher) []StorageSeries {
	var out []StorageSeries
	for _, s := range q.series {
		ok := true
		for _, m := range ms {
			if !m.Matches(s.lbls.Get(m.Name)) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		var pts []Point
		for _, p := range s.pts {
			if p.T >= q.mint && p.T <= q.maxt {
				pts = append(pts, p)
			}
		}
		if len(pts) == 0 {
			continue
		}
		out = append(out, fakeSeries{lbls: s.lbls, pts: pts})
	}
	return out
}

type fakeStorage struct{ series []fakeSeries }

func (f *fakeStorage) Querier(mint, maxt int64) (Querier, func() error, error) {
	return &fakeQuerier{mint: mint, maxt: maxt, series: f.series}, func() error { return nil }, nil
}

func mkStorage(ss ...fakeSeries) *fakeStorage { return &fakeStorage{series: ss} }

func srs(name string, kv map[string]string, pts ...Point) fakeSeries {
	m := map[string]string{labels.MetricName: name}
	for k, v := range kv {
		m[k] = v
	}
	return fakeSeries{lbls: labels.LabelsFromMap(m), pts: pts}
}

// labelsMetricNameForTest 는 테스트에서 __name__ 잔존을 확인할 때 쓴다.
const labelsMetricNameForTest = labels.MetricName
