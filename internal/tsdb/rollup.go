package tsdb

import "math"

const (
	// Resolution5m 은 5분 롤업 블록의 표식이다.
	Resolution5m = "5m"

	rollupInterval int64 = 5 * 60 * 1000

	// RollupLabel 은 롤업 종류를 담는 예약 라벨이다. 롤업을 별도 저장
	// 포맷으로 만들지 않고 "라벨이 하나 더 붙은 보통 시리즈"로 표현하면
	// 블록 쓰기·읽기·질의 코드를 그대로 재사용할 수 있다.
	RollupLabel = "__rollup__"
)

// rollupKinds 는 저장하는 집계 4종이다. 평균은 sum/count 로 유도하므로
// 따로 저장하지 않는다 — 그래야 여러 버킷을 다시 합칠 때도 정확하다.
var rollupKinds = []string{"sum", "count", "min", "max"}

type rollupBucket struct {
	start int64
	sum   float64
	count float64
	min   float64
	max   float64
}

// RollupSeries 는 원본 시리즈들을 interval 크기 버킷으로 집계해, 원본 하나당
// 4개(sum/count/min/max)의 새 시리즈를 만든다. 버킷 타임스탬프는 버킷 시작
// 시각이다.
func RollupSeries(src []*memSeries, interval int64) []*memSeries {
	if len(src) == 0 || interval <= 0 {
		return nil
	}

	out := make([]*memSeries, 0, len(src)*len(rollupKinds))
	var nextRef uint64

	for _, s := range src {
		var buckets []rollupBucket
		curIdx := -1

		for _, c := range s.chunks {
			it := c.Iterator()
			for it.Next() {
				t, v := it.At()
				// 타임스탬프는 유닉스 밀리초(양수)라 0 방향 절삭이
				// 곧 내림이다. 음수 시각은 들어오지 않는다.
				start := (t / interval) * interval

				// 슬라이스 인덱스로 다룬다 — append 가 재할당해도
				// 이전 요소 포인터를 들고 있지 않게 하려는 것이다.
				if curIdx < 0 || buckets[curIdx].start != start {
					buckets = append(buckets, rollupBucket{
						start: start,
						min:   math.Inf(1),
						max:   math.Inf(-1),
					})
					curIdx = len(buckets) - 1
				}
				b := &buckets[curIdx]
				b.sum += v
				b.count++
				if v < b.min {
					b.min = v
				}
				if v > b.max {
					b.max = v
				}
			}
		}
		if len(buckets) == 0 {
			continue
		}

		for _, kind := range rollupKinds {
			nextRef++
			lset := NewLabels(append(s.lset.Copy(), Label{RollupLabel, kind})...)
			rs := &memSeries{ref: nextRef, lset: lset}
			for _, b := range buckets {
				var v float64
				switch kind {
				case "sum":
					v = b.sum
				case "count":
					v = b.count
				case "min":
					v = b.min
				case "max":
					v = b.max
				}
				// append 는 청크가 차면 알아서 새 청크를 연다.
				_ = rs.append(b.start, v)
			}
			out = append(out, rs)
		}
	}
	return out
}
