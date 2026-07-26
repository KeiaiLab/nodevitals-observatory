// Command observatory runs the nodevitals fleet collector and query API.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/apiserver"
	"github.com/KeiaiLab/nodevitals-observatory/internal/auth"
	"github.com/KeiaiLab/nodevitals-observatory/internal/demo"
	"github.com/KeiaiLab/nodevitals-observatory/internal/discovery"
	"github.com/KeiaiLab/nodevitals-observatory/internal/scrape"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// defaultCompactInterval 은 -compact-interval/OBSERVATORY_COMPACT_INTERVAL 의
// 기본값이자 runCompactLoop 가 0 이하 interval 을 받았을 때 강등하는 안전값이다
// (time.NewTicker 는 non-positive interval 에서 panic 하므로 반드시 >0 이어야
// 한다 — scrape.defaultInterval 과 동일한 안전 강등 계약).
const defaultCompactInterval = time.Hour

func main() {
	// 플래그 우선순위 = flag > env > 기본값 — flag 기본값 자리에 envOr 계열을
	// 끼워 넣어 flag.Parse 가 최종적으로 명령행 인자로 덮어쓸 수 있게 한다
	// (m2-design.md §6 계약). scrape-port/-interval/compact-interval 은 기반
	// 타입이 int/time.Duration 이라 flag.Int/flag.Duration 을 쓴다 — 값 검증을
	// flag 패키지에 위임할 수 있어 flag.String 획일화보다 안전하다.
	dataDir := flag.String("data-dir", envOr("OBSERVATORY_DATA_DIR", "/data"), "tsdb 데이터 디렉터리")
	listen := flag.String("listen", envOr("OBSERVATORY_LISTEN", ":9090"), "API listen 주소")
	scrapeMode := flag.String("scrape-mode", envOr("OBSERVATORY_SCRAPE_MODE", "kubernetes"), "kubernetes | static")
	scrapeNamespace := flag.String("scrape-namespace", envOr("OBSERVATORY_SCRAPE_NAMESPACE", "platform-system"), "kubernetes 모드 대상 네임스페이스")
	scrapeSelector := flag.String("scrape-selector", envOr("OBSERVATORY_SCRAPE_SELECTOR", "app.kubernetes.io/name=nodevitals"), "kubernetes 모드 파드 labelSelector")
	scrapePort := flag.Int("scrape-port", envIntOr("OBSERVATORY_SCRAPE_PORT", 9847), "타겟 /metrics 포트")
	staticTargets := flag.String("static-targets", envOr("OBSERVATORY_STATIC_TARGETS", ""), "static 모드 콤마 구분 URL 목록")
	scrapeInterval := flag.Duration("scrape-interval", envDurationOr("OBSERVATORY_SCRAPE_INTERVAL", 15*time.Second), "스크레이프 주기")
	compactInterval := flag.Duration("compact-interval", envDurationOr("OBSERVATORY_COMPACT_INTERVAL", defaultCompactInterval), "tsdb Compact 주기")
	demoMode := flag.Bool("demo", envBoolOr("OBSERVATORY_DEMO", false), "GPU 플릿 데모 모드 — 합성 플릿 emit, 스크레이프 비활성")
	flag.Parse()

	// 1. tsdb open — 실패는 즉시 종료(fail fast). 데모 모드는 보존기간을
	// 축소한다(raw 48h/롤업 7d) — 합성 4.9만 시리즈의 디스크·head 상한 통제.
	opts := tsdb.DefaultOptions(*dataDir)
	if *demoMode {
		const hourMS = int64(3600 * 1000)
		opts.RawRetention = 48 * hourMS
		opts.RollupRetention = 7 * 24 * hourMS
	}
	db, err := tsdb.Open(opts)
	if err != nil {
		slog.Error("tsdb open 실패", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 2. 데이터 공급원 분기 — 데모 모드는 스크레이퍼를 아예 만들지 않는다
	// (합성 플릿에 실 데이터가 섞이는 것을 구조적으로 차단). cmd 는 wall clock
	// 예외 허용 구간(m2-design.md §0)이라 nowFn 주입이 여기서만 일어난다.
	var (
		wg         sync.WaitGroup
		ready      func() bool
		serverOpts []apiserver.ServerOption
	)
	if *demoMode {
		demoCfg, cfgErr := demo.ConfigFromEnv(os.LookupEnv)
		if cfgErr != nil {
			slog.Error("데모 설정 파싱 실패", "err", cfgErr)
			os.Exit(1)
		}
		engine := demo.NewEngine(db, demoCfg, func() int64 { return time.Now().UnixMilli() })
		wg.Add(1)
		go func() {
			defer wg.Done()
			engine.Run(ctx)
		}()
		ready = engine.Ready
		serverOpts = append(serverOpts, apiserver.WithDemo(engine))
		slog.Warn("데모 모드 활성 — 합성 GPU 플릿 데이터만 emit 한다(스크레이프 비활성)",
			"seed", demoCfg.Seed, "timeScale", demoCfg.TimeScale)
	} else {
		var disc discovery.Discoverer
		switch *scrapeMode {
		case "static":
			targets, err := discovery.ParseStaticTargets(*staticTargets)
			if err != nil {
				slog.Error("정적 타겟 파싱 실패", "err", err)
				os.Exit(1)
			}
			disc = discovery.NewStatic(targets)
		case "kubernetes":
			d, err := discovery.NewKubernetes(discovery.KubeConfig{
				Namespace:     *scrapeNamespace,
				LabelSelector: *scrapeSelector,
				Port:          *scrapePort,
			})
			if err != nil {
				slog.Error("kubernetes discoverer 초기화 실패", "err", err)
				os.Exit(1)
			}
			disc = d
		default:
			slog.Error("알 수 없는 scrape-mode", "mode", *scrapeMode, "known", "kubernetes, static")
			os.Exit(1)
		}

		scr := scrape.NewScraper(db, disc, scrape.Options{
			Interval: *scrapeInterval,
			Now:      func() int64 { return time.Now().UnixMilli() },
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			scr.Run(ctx)
		}()
		ready = scr.Ready
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runCompactLoop(ctx, db, *compactInterval)
	}()

	// 4. 인증 — admin credential 은 env 로 주입(m4-constraints.md "인증").
	// 미설정 시 랜덤 비밀번호를 생성해 기동 로그에 1회만 출력한다(Grafana 관례).
	adminUser := envOr("OBSERVATORY_ADMIN_USER", "admin")
	adminPassword := os.Getenv("OBSERVATORY_ADMIN_PASSWORD")
	if adminPassword == "" {
		generated, genErr := generateRandomPassword()
		if genErr != nil {
			slog.Error("admin 비밀번호 생성 실패", "err", genErr)
			os.Exit(1)
		}
		adminPassword = generated
		slog.Info("OBSERVATORY_ADMIN_PASSWORD 미설정 — 임의 비밀번호를 생성했다(재기동 시 회전)",
			"user", adminUser, "password", adminPassword)
	}
	authenticator, err := auth.NewAuthenticator(adminUser, adminPassword)
	if err != nil {
		slog.Error("authenticator 초기화 실패", "err", err)
		os.Exit(1)
	}

	// 5. API 서버 — query 가 무거워 write timeout 을 nodevitals(10s) 보다 넉넉히.
	srv := &http.Server{
		Addr:              *listen,
		Handler:           apiserver.NewServer(db, ready, authenticator, serverOpts...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "err", err)
		}
	}()

	slog.Info("observatory started",
		"listen", *listen, "dataDir", *dataDir, "demo", *demoMode, "scrapeMode", *scrapeMode, "scrapeInterval", *scrapeInterval)

	// 6. graceful shutdown — SIGINT/SIGTERM 수신까지 대기.
	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown 실패", "err", err)
	}

	// 스크레이퍼(또는 데모 엔진)·compact 루프는 공유 ctx 취소로 스스로 끝난다 —
	// 완주를 기다려 db.Close() 와의 경합을 막는다.
	wg.Wait()

	// SIGTERM 창 내 db.Compact 재시도는 위험(§6 계약) — 시도하지 않고 Sync+Close 만.
	if err := db.Sync(); err != nil {
		slog.Error("tsdb sync 실패", "err", err)
	}
	if err := db.Close(); err != nil {
		slog.Error("tsdb close 실패", "err", err)
	}
}

// runCompactLoop 는 interval 마다 db.Compact 를 호출한다. ctx 취소 시 진행 중인
// tick 을 새로 시작하지 않고 즉시 반환한다(SIGTERM 창 내 Compact 회피 — 위
// main 의 shutdown 계약과 동일 근거). 에러는 경고만 남기고 다음 tick 에 재시도한다
// (m2-design.md §6 — "프로세스 생존"). interval<=0(오설정 configmap 등) 은
// time.NewTicker 가 panic 하므로 defaultCompactInterval 로 강등한다 —
// scrape.NewScraper 의 Interval<=0 강등과 대칭 계약(fail-fast 대신 안전 강등).
func runCompactLoop(ctx context.Context, db *tsdb.DB, interval time.Duration) {
	if interval <= 0 {
		slog.Warn("compact-interval 이 0 이하 — 기본값으로 강등",
			"given", interval, "default", defaultCompactInterval)
		interval = defaultCompactInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			if err := db.Compact(time.Now().UnixMilli()); err != nil {
				slog.Warn("tsdb compact 실패", "err", err)
			}
		}
	}
}

// envOr 는 환경변수 key 가 설정돼 있으면 그 값을, 아니면 def 를 반환한다.
func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// envIntOr 는 envOr 의 정수 버전 — 파싱 실패는 기동을 막을 만큼 치명적이지
// 않으므로(스크레이프 대상 설정 오류일 뿐) 기본값으로 안전 강등한다.
func envIntOr(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envDurationOr 는 envOr 의 time.Duration 버전(time.ParseDuration 문법).
func envDurationOr(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// envBoolOr 는 envOr 의 불리언 버전 — strconv.ParseBool 문법("1"/"true" 등).
// 파싱 실패는 기본값으로 안전 강등한다.
func envBoolOr(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// generateRandomPassword 는 OBSERVATORY_ADMIN_PASSWORD 미설정 시 최초 기동에
// 쓸 임의 비밀번호를 만든다. auth.HashPassword 등 내부 헬퍼는 비공개
// (unexported)라 이 파일이 직접 stdlib crypto/rand 로 생성한다(외부 의존 0,
// m2-constraints.md 계승). 18B 무작위값을 base64.RawURLEncoding 으로 인코딩해
// 24자 문자열을 낸다.
func generateRandomPassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("임의 비밀번호 생성 실패: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
