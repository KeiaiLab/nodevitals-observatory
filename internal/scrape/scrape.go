// Package scrape 는 discovery.Discoverer 가 낸 타겟들의 /metrics 를 주기적으로
// GET 해 promtext 로 파싱하고 tsdb.DB 에 적재한다. 타겟 하나의 실패가 다른
// 타겟의 적재를 막지 않도록 격리한다(m2-design.md §4 계약).
package scrape

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/discovery"
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/promtext"
)

const (
	defaultInterval = 15 * time.Second
	defaultTimeout  = 10 * time.Second

	// 자체 관측 메트릭 이름 + 두 라벨 매핑에 공통으로 쓰는 instance 라벨 이름.
	metricUp            = "observatory_up"
	metricScrapeSamples = "observatory_scrape_samples"
	instanceLabel       = "instance"
)

// Options 는 Scraper 동작을 결정한다.
type Options struct {
	Interval time.Duration // Run 루프 주기. 0 이면 15s.
	Timeout  time.Duration // 타겟당 HTTP 타임아웃. 0 이면 10s.
	Client   *http.Client  // nil 이면 &http.Client{Timeout: Timeout}
	// Now 는 밀리초 epoch 을 낸다. nil 이면 time.Now 기반(cmd 전용) — 테스트는
	// 반드시 고정 클록을 주입해야 한다(결정론 계약).
	Now func() int64
}

// Appender 는 스크레이퍼가 저장 계층에 요구하는 전부다. tsdb.DB 가 이
// 시그니처를 만족하므로 별도 어댑터가 필요 없고, 테스트는 저장 엔진 없이
// 가짜 하나로 끝난다.
type Appender interface {
	Append(lset labels.Labels, t int64, v float64) error
}

// Scraper 는 하나의 discovery.Discoverer 로 얻은 타겟들을 주기적으로 긁어
// Appender 에 적재한다.
type Scraper struct {
	db   Appender
	d    discovery.Discoverer
	opts Options

	ready atomic.Bool
}

// NewScraper 는 opts 의 미설정 필드를 기본값으로 채운 뒤 Scraper 를 만든다.
func NewScraper(db Appender, d discovery.Discoverer, opts Options) *Scraper {
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: opts.Timeout}
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Scraper{db: db, d: d, opts: opts}
}

// ScrapeOnce 는 Discover → 타겟별 GET → promtext.Parse → Append 1 사이클을
// 수행한다. 타겟 간 실패는 격리한다 — 실패한 타겟은 observatory_up=0 만
// 남기고 다음 타겟으로 진행한다. 반환 에러는 Discover 실패처럼 사이클
// 전체가 불가능한 경우만이다.
func (s *Scraper) ScrapeOnce(ctx context.Context) error {
	// Ready 는 "첫 사이클이 완주(에러 무관)했는가" 계약이다 — Discover 가
	// 실패해도 시도 자체는 끝난 것으로 보고 readyz 를 열어야 dead-target
	// 환경에서도 API 조회가 가능하다. defer 로 모든 반환 경로를 덮는다.
	defer s.ready.Store(true)

	targets, err := s.d.Discover(ctx)
	if err != nil {
		return fmt.Errorf("scrape: 타겟 발견 실패: %w", err)
	}

	now := s.opts.Now()
	for _, target := range targets {
		s.scrapeTarget(ctx, target, now)
	}
	return nil
}

// scrapeTarget 은 타겟 하나를 긁어 저장하고, 성공/실패와 무관하게 자체 관측
// 메트릭(observatory_up, observatory_scrape_samples)을 남긴다. 타겟 실패를
// 이 함수 밖으로 전파하지 않는다 — 실패 격리는 ScrapeOnce 의 계약이다.
func (s *Scraper) scrapeTarget(ctx context.Context, target discovery.Target, now int64) {
	n, err := s.scrapeAndStore(ctx, target, now)

	up := 1.0
	if err != nil {
		slog.Warn("scrape: 타겟 실패", "target", target.Name, "url", target.URL, "error", err)
		up = 0
		n = 0
	}
	s.appendSelfMetric(metricUp, target.Name, now, up)
	s.appendSelfMetric(metricScrapeSamples, target.Name, now, float64(n))
}

// scrapeAndStore 는 타겟의 /metrics 를 GET 해 파싱하고 DB 에 적재한다.
// 반환값은 성공적으로 Append 된 샘플 수다. GET·비200·파싱 실패만 에러로
// 반환한다 — 개별 샘플의 Append 실패는 그 샘플만 건너뛰고 계속한다(부분
// 성공이 전체 실패보다 낫다).
func (s *Scraper) scrapeAndStore(ctx context.Context, target discovery.Target, now int64) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("요청 생성 실패: %w", err)
	}

	resp, err := s.opts.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("비정상 응답 %d", resp.StatusCode)
	}

	series, err := promtext.Parse(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("파싱 실패: %w", err)
	}

	n := 0
	for _, sr := range series {
		lset := buildTargetLabels(sr, target.Name)
		if err := s.db.Append(lset, now, sr.Value); err != nil {
			slog.Warn("scrape: append 실패", "target", target.Name, "metric", sr.Name, "error", err)
			continue
		}
		n++
	}
	return n, nil
}

// buildTargetLabels 는 파싱된 시리즈 하나를 TSDB 라벨셋으로 변환한다.
// exposition 라벨을 베이스로 하되 __name__ 과 instance 는 스크레이퍼가
// 확정한 값으로 덮는다 — instance 는 타겟 exposition 자체의 동명 라벨보다
// 우선한다(M2 계약, m2-design.md §4 "스크레이퍼 관례").
func buildTargetLabels(sr promtext.Series, instance string) labels.Labels {
	m := make(map[string]string, len(sr.Labels)+2)
	for k, v := range sr.Labels {
		m[k] = v
	}
	m[labels.MetricName] = sr.Name
	m[instanceLabel] = instance
	return labels.LabelsFromMap(m)
}

// appendSelfMetric 은 observatory_up/observatory_scrape_samples 한 점을
// 남긴다. Append 자체가 실패해도(디스크 문제 등) 사이클을 막지 않고 경고만
// 남긴다 — 자체 관측 메트릭의 실패로 다른 타겟 처리를 중단할 이유가 없다.
func (s *Scraper) appendSelfMetric(name, instance string, t int64, v float64) {
	lset := labels.LabelsFromMap(map[string]string{
		labels.MetricName: name,
		instanceLabel:     instance,
	})
	if err := s.db.Append(lset, t, v); err != nil {
		slog.Warn("scrape: 자체 메트릭 append 실패", "metric", name, "instance", instance, "error", err)
	}
}

// Run 은 즉시 1회 ScrapeOnce 를 실행한 뒤 Interval 마다 반복한다. ctx 취소로
// 종료한다(에러 반환 없음) — 사이클 에러는 로그만 남기고 다음 tick 에
// 재시도한다.
func (s *Scraper) Run(ctx context.Context) {
	s.runOnce(ctx)

	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scraper) runOnce(ctx context.Context) {
	if err := s.ScrapeOnce(ctx); err != nil {
		slog.Error("scrape: 사이클 실패", "error", err)
	}
}

// Ready 는 첫 ScrapeOnce 가 완주(에러 무관)한 뒤 true 다 — apiserver
// /readyz 배선용.
func (s *Scraper) Ready() bool { return s.ready.Load() }
