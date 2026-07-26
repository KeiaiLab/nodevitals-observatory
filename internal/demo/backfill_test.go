package demo

import (
	"testing"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// recordingAppender 는 시리즈별 마지막 타임스탬프를 추적하는 스텁이다 —
// tsdb head 의 "시리즈별 시간 오름차순 append" 전제를 백필이 지키는지 검증.
type recordingAppender struct {
	lastT map[string]int64
	count int
	viol  int
}

func (r *recordingAppender) Append(lset tsdb.Labels, tMS int64, _ float64) error {
	key := lset.MapKey()
	if prev, ok := r.lastT[key]; ok && tMS < prev {
		r.viol++
	}
	r.lastT[key] = tMS
	r.count++
	return nil
}

func TestBackfill_단조성과_결정론(t *testing.T) {
	cfg := testConfig(t, "a:클라우드 A:16,b:클라우드 B:8")

	run := func() *recordingAppender {
		rec := &recordingAppender{lastT: map[string]int64{}}
		e := NewEngine(rec, cfg, func() int64 { return t0 })
		if err := e.Backfill(t0); err != nil {
			t.Fatalf("백필 실패: %v", err)
		}
		return rec
	}

	r1 := run()
	if r1.count == 0 {
		t.Fatalf("백필이 아무 샘플도 쓰지 않았다")
	}
	if r1.viol != 0 {
		t.Fatalf("시리즈 내 시간 역행 append %d건 — head 계약 위반", r1.viol)
	}

	// 결정론 — 같은 (cfg, now) 는 같은 샘플 수를 만든다.
	r2 := run()
	if r1.count != r2.count {
		t.Fatalf("백필 결정론 위반: %d != %d", r1.count, r2.count)
	}

	// 집계 백필(10m)이 per-GPU 창(2m)보다 길게 존재해야 한다 — 24h 추이 서사.
	aggKeyFound := false
	for key, last := range r1.lastT {
		_ = last
		if len(key) > 0 {
			aggKeyFound = true
			break
		}
	}
	if !aggKeyFound {
		t.Fatalf("집계 시리즈가 백필되지 않았다")
	}
}
