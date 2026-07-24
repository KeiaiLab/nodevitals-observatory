package scrape

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/discovery"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// discoverFunc 는 함수 하나를 discovery.Discoverer 로 어댑트한다(테스트 전용
// stub — 실 네트워크·k8s 의존 없이 Discover 의 반환값을 자유롭게 주입한다).
type discoverFunc func(ctx context.Context) ([]discovery.Target, error)

func (f discoverFunc) Discover(ctx context.Context) ([]discovery.Target, error) { return f(ctx) }

// openTestDB 는 임시 디렉터리에 실 tsdb.DB 를 연다(왕복 검증 — mock 아님).
func openTestDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// latestSample 은 name{instance=instance} 시리즈의 최신(유일) 샘플 값을 낸다.
// 시리즈가 정확히 1개가 아니면 즉시 실패한다 — self-metric 은 사이클마다
// 타겟당 한 점만 남아야 하므로 개수 자체가 계약이다.
func latestSample(t *testing.T, q *tsdb.Querier, name, instance string) float64 {
	t.Helper()
	mName, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, name)
	if err != nil {
		t.Fatalf("NewMatcher(%q): %v", name, err)
	}
	mInst, err := tsdb.NewMatcher(tsdb.MatchEqual, instanceLabel, instance)
	if err != nil {
		t.Fatalf("NewMatcher(instance=%q): %v", instance, err)
	}
	series := q.Select(mName, mInst)
	if len(series) != 1 {
		t.Fatalf("%s{instance=%s} 시리즈 개수 = %d, want 1", name, instance, len(series))
	}
	it := series[0].Iterator()
	if !it.Next() {
		t.Fatalf("%s{instance=%s} 샘플이 없다", name, instance)
	}
	_, v := it.At()
	return v
}

// ---- ScrapeOnce: 저장 + 라벨 매핑 (왕복) ----

func TestScrapeOnce_저장과_라벨매핑(t *testing.T) {
	const t0 = int64(1_700_000_000_000) // 임의 고정 ms epoch — 결정론 계약

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `node_load1{node="e101"} 1.5`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	db := openTestDB(t)
	disc := discovery.NewStatic([]discovery.Target{{Name: "e101-9847", URL: srv.URL + "/metrics"}})
	scr := NewScraper(db, disc, Options{Now: func() int64 { return t0 }})

	if err := scr.ScrapeOnce(context.Background()); err != nil {
		t.Fatalf("ScrapeOnce: %v", err)
	}

	q, closeQ, err := db.Querier(t0-1, t0+1)
	if err != nil {
		t.Fatalf("Querier: %v", err)
	}
	defer closeQ()

	m, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, "node_load1")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	series := q.Select(m)
	if len(series) != 1 {
		t.Fatalf("node_load1 시리즈 개수 = %d, want 1", len(series))
	}

	lset := series[0].Labels()
	// __name__ 변환이 틀렸으면 애초에 위 Select 가 0개를 냈을 것이므로, 여기서는
	// exposition 라벨(node)과 스크레이퍼가 덮은 instance 라벨을 개별 단정한다.
	if got := lset.Get("node"); got != "e101" {
		t.Errorf("node 라벨 = %q, want e101", got)
	}
	if got := lset.Get(instanceLabel); got != "e101-9847" {
		t.Errorf("instance 라벨 = %q, want e101-9847 (target.Name 오버라이드 계약)", got)
	}

	it := series[0].Iterator()
	if !it.Next() {
		t.Fatalf("Iterator.Next() = false, want true")
	}
	gotT, gotV := it.At()
	// gotT 가 t0 와 다르면 Now() 주입이 무시되고 실제 wall clock 이 쓰였다는
	// 뜻이다 — 그 경우 애초에 [t0-1,t0+1] 창 밖이라 위 Select 에서 이미 0개로
	// 걸렸겠지만, 값 자체도 한 번 더 명시적으로 단정해 둔다.
	if gotT != t0 {
		t.Errorf("샘플 시각 = %d, want %d (nowMS 주입 계약)", gotT, t0)
	}
	if gotV != 1.5 {
		t.Errorf("샘플 값 = %v, want 1.5", gotV)
	}
	if it.Next() {
		t.Errorf("샘플이 1개여야 하는데 2개 이상 존재한다")
	}
	if err := it.Err(); err != nil {
		t.Errorf("Iterator.Err() = %v, want nil", err)
	}
}

// ---- ScrapeOnce: 실패 타겟 격리 + up/samples 메트릭 ----

func TestScrapeOnce_실패타겟_격리와_up메트릭(t *testing.T) {
	const t0 = int64(1_700_000_000_000)

	okMux := http.NewServeMux()
	okMux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `up 1`)
		fmt.Fprintln(w, `node_load1{node="e102"} 2.5`)
	})
	okSrv := httptest.NewServer(okMux)
	defer okSrv.Close()

	failMux := http.NewServeMux()
	failMux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "boom")
	})
	failSrv := httptest.NewServer(failMux)
	defer failSrv.Close()

	db := openTestDB(t)
	disc := discovery.NewStatic([]discovery.Target{
		{Name: "ok-target", URL: okSrv.URL + "/metrics"},
		{Name: "fail-target", URL: failSrv.URL + "/metrics"},
	})
	scr := NewScraper(db, disc, Options{Now: func() int64 { return t0 }})

	if err := scr.ScrapeOnce(context.Background()); err != nil {
		t.Fatalf("ScrapeOnce 반환 에러 = %v, want nil (타겟 실패는 격리되어야 한다)", err)
	}

	q, closeQ, err := db.Querier(t0-1, t0+1)
	if err != nil {
		t.Fatalf("Querier: %v", err)
	}
	defer closeQ()

	// 정상 타겟의 실 샘플(node_load1)이 착지했는지 — 격리가 안 되고 사이클
	// 전체가 죽는 구현이면 여기서 0개가 나와 실패한다.
	mLoad, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, "node_load1")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if got := q.Select(mLoad); len(got) != 1 {
		t.Fatalf("정상 타겟 node_load1 시리즈 개수 = %d, want 1", len(got))
	}

	if v := latestSample(t, q, metricUp, "fail-target"); v != 0 {
		t.Errorf("observatory_up{instance=fail-target} = %v, want 0", v)
	}
	if v := latestSample(t, q, metricUp, "ok-target"); v != 1 {
		t.Errorf("observatory_up{instance=ok-target} = %v, want 1", v)
	}
	// ok 타겟은 "up 1" + "node_load1{...} 2.5" 두 줄을 노출한다 → 실 샘플 수 2.
	// 이 값을 하드코딩하지 않고 타겟이 실제로 낸 라인 수와 맞춰, 구현이
	// 상수를 그냥 반환해도 통과하는 동어반복이 되지 않게 한다.
	if v := latestSample(t, q, metricScrapeSamples, "ok-target"); v != 2 {
		t.Errorf("observatory_scrape_samples{instance=ok-target} = %v, want 2", v)
	}
	// 실패 타겟도 매 사이클·타겟마다 samples=0 을 남긴다는 계약(design §4)까지
	// 확인한다 — "실패 타겟은 관측 메트릭 자체를 안 남긴다"로 잘못 구현하면
	// latestSample 이 시리즈 0개를 만나 Fatalf 로 잡아낸다.
	if v := latestSample(t, q, metricScrapeSamples, "fail-target"); v != 0 {
		t.Errorf("observatory_scrape_samples{instance=fail-target} = %v, want 0", v)
	}
}

// ---- ScrapeOnce: Discover 자체 실패 ----

func TestScrapeOnce_Discover실패시_에러반환_그러나_Ready는_true(t *testing.T) {
	db := openTestDB(t)
	wantErr := errors.New("발견 실패 주입")
	disc := discoverFunc(func(_ context.Context) ([]discovery.Target, error) { return nil, wantErr })
	scr := NewScraper(db, disc, Options{Now: func() int64 { return 1_700_000_000_000 }})

	err := scr.ScrapeOnce(context.Background())
	if err == nil {
		t.Fatal("ScrapeOnce() 에러 = nil, want non-nil (Discover 실패가 전파돼야 한다)")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("ScrapeOnce() 에러 = %v, want %v 를 wrap", err, wantErr)
	}
	if !scr.Ready() {
		t.Errorf("Discover 실패 후 Ready() = false, want true (완주=에러 무관 계약)")
	}
}

// ---- Ready ----

func TestReady_첫사이클_후_true(t *testing.T) {
	db := openTestDB(t)

	failMux := http.NewServeMux()
	failMux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	failSrv := httptest.NewServer(failMux)
	defer failSrv.Close()

	disc := discovery.NewStatic([]discovery.Target{{Name: "dead", URL: failSrv.URL + "/metrics"}})
	scr := NewScraper(db, disc, Options{Now: func() int64 { return 1_700_000_000_000 }})

	if scr.Ready() {
		t.Fatal("생성 직후 Ready() = true, want false")
	}

	// "실패-only" 사이클 — 유일한 타겟이 500 을 반환해 샘플이 하나도 안 붙는다.
	// "성공 시에만 ready" 로 잘못 구현하면(예: samples>0 조건부 Store) 아래
	// 단정이 실패한다 — dead-target 환경에서도 파드는 Ready 여야 한다.
	if err := scr.ScrapeOnce(context.Background()); err != nil {
		t.Fatalf("ScrapeOnce(실패-only 사이클) 에러 = %v, want nil (타겟 실패는 사이클 에러가 아니다)", err)
	}

	if !scr.Ready() {
		t.Error("실패-only 사이클 후 Ready() = false, want true")
	}
}

// ---- Run: 즉시 1회 + ctx 취소 종료 ----

func TestRun_즉시_1회_실행하고_ctx취소로_종료한다(t *testing.T) {
	db := openTestDB(t)

	called := make(chan struct{}, 1)
	disc := discoverFunc(func(_ context.Context) ([]discovery.Target, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return nil, nil
	})

	scr := NewScraper(db, disc, Options{
		// tick 이 테스트 창 안에서 절대 일어나지 않을 만큼 크게 잡는다 — 검증
		// 대상은 "즉시 1회 실행" 과 "ctx 취소 종료" 뿐, 재-tick 타이밍이 아니다.
		Interval: time.Hour,
		Now:      func() int64 { return 1_700_000_000_000 },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scr.Run(ctx)
		close(done)
	}()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() 이 즉시 1회 Discover(ScrapeOnce) 를 호출하지 않았다")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 취소 후에도 Run() 이 반환하지 않았다(ctx.Done() 미선택 의심)")
	}
}
