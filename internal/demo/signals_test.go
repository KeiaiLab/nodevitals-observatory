package demo

import (
	"math"
	"testing"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

func newTestDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb open 실패: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestEmitFast_실tsdb_왕복 은 소형 플릿을 실제 tsdb 에 emit 하고 Querier 로
// 읽어 (a) per-GPU 시리즈 존재 (b) 집계-개체 정합 (c) 값 대역을 검증한다.
func TestEmitFast_실tsdb_왕복(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:32,b:클라우드 B:16")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	if err := e.emitFast(t0); err != nil {
		t.Fatalf("emitFast 실패: %v", err)
	}

	querier, closeFn, err := db.Querier(t0-60_000, t0+60_000)
	if err != nil {
		t.Fatalf("querier 실패: %v", err)
	}
	defer func() { _ = closeFn() }()

	nameMatcher := func(name string) *tsdb.Matcher {
		m, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, name)
		if err != nil {
			t.Fatalf("matcher 실패: %v", err)
		}
		return m
	}

	// (a) per-GPU util 시리즈: 수집 중단 노드(Agent Missing) 소속만 빠진다.
	utilSeries := querier.Select(nameMatcher(metricUtil))
	missing := 0
	if e.scen.missingNode != nil {
		missing = len(e.scen.missingNode.GPUs)
	}
	if got, want := len(utilSeries), len(e.fleet.GPUs)-missing; got != want {
		t.Fatalf("util 시리즈 수 = %d, want %d (전체 %d − 수집중단 %d)",
			got, want, len(e.fleet.GPUs), missing)
	}

	// (b) 집계 정합 — csp 합 = fleet 값, 그리고 총계는 관측 여부와 무관하게
	// 인벤토리 전수와 일치해야 한다(전 화면 단일 목데이터 정합 계약).
	readScalar := func(name string) float64 {
		series := querier.Select(nameMatcher(name))
		if len(series) != 1 {
			t.Fatalf("%s 시리즈 수 = %d, want 1", name, len(series))
		}
		it := series[0].Iterator()
		var v float64
		found := false
		for it.Next() {
			_, v = it.At()
			found = true
		}
		if !found {
			t.Fatalf("%s 샘플 없음", name)
		}
		return v
	}
	fleetTotal := readScalar("nodevitals_demo_fleet_gpu_total")
	if int(fleetTotal) != len(e.fleet.GPUs) {
		t.Fatalf("fleet_gpu_total = %v — 인벤토리 전수(%d)와 불일치(Agent Missing 제외 금지)",
			fleetTotal, len(e.fleet.GPUs))
	}
	wantAlloc := 0
	for _, g := range e.fleet.GPUs {
		if g.Allocated {
			wantAlloc++
		}
	}
	if got := int(readScalar("nodevitals_demo_fleet_gpu_allocated")); got != wantAlloc {
		t.Fatalf("fleet_gpu_allocated = %d, want 인벤토리 전수 기준 %d", got, wantAlloc)
	}

	cspSeries := querier.Select(nameMatcher("nodevitals_demo_csp_gpu_total"))
	cspSum := 0.0
	for _, s := range cspSeries {
		it := s.Iterator()
		var v float64
		for it.Next() {
			_, v = it.At()
		}
		cspSum += v
	}
	if math.Abs(cspSum-fleetTotal) > 0.001 {
		t.Fatalf("csp 합(%v) ≠ fleet 총계(%v)", cspSum, fleetTotal)
	}

	// (c) 값 대역 — util 은 [0,100].
	for _, s := range utilSeries {
		it := s.Iterator()
		for it.Next() {
			_, v := it.At()
			if v < 0 || v > 100 {
				t.Fatalf("util 대역 위반: %v (%s)", v, s.Labels())
			}
		}
	}

	// 노드 시리즈(기존 Overview 호환)도 존재해야 한다.
	loadSeries := querier.Select(nameMatcher("node_load1"))
	missingNodes := 0
	if e.scen.missingNode != nil {
		missingNodes = 1
	}
	if got, want := len(loadSeries), len(e.fleet.Nodes)-missingNodes; got != want {
		t.Fatalf("node_load1 시리즈 수 = %d, want %d", got, want)
	}
}

// TestComputeSignals_결정론 — 같은 (GPU, t) 는 항상 같은 신호를 낸다(백필과
// 라이브가 이어지는 전제).
func TestComputeSignals_결정론(t *testing.T) {
	cfg := testConfig(t, "a:클라우드 A:8")
	f := BuildFleet(cfg.Fleet, cfg.Seed)
	g := f.GPUs[0]
	a := computeSignals(g, t0, cfg.Seed)
	b := computeSignals(g, t0, cfg.Seed)
	if a != b {
		t.Fatalf("결정론 위반: %+v != %+v", a, b)
	}
	c := computeSignals(g, t0+15_000, cfg.Seed)
	if a == c {
		t.Fatalf("시간이 달라도 신호가 동일하다 — 파형 정지")
	}
}
