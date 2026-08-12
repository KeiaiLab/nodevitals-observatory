// Command observatory 는 nodevitals 에이전트를 긁어 저장하고, 질의·알림·화면을
// 한 프로세스로 제공한다.
//
// 설정은 전부 환경변수다 — ConfigMap 하나로 끝나고, 파일 포맷을 하나 더
// 떠안지 않는다(룰 파일만 예외다. 구조가 있어야 하기 때문이다).
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/alerting"
	"github.com/KeiaiLab/nodevitals-observatory/internal/apiserver"
	"github.com/KeiaiLab/nodevitals-observatory/internal/auth"
	"github.com/KeiaiLab/nodevitals-observatory/internal/discovery"
	"github.com/KeiaiLab/nodevitals-observatory/internal/notify"
	"github.com/KeiaiLab/nodevitals-observatory/internal/rules"
	"github.com/KeiaiLab/nodevitals-observatory/internal/scrape"
	"github.com/KeiaiLab/nodevitals-observatory/internal/storage"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// version 은 빌드 시 -ldflags 로 주입된다. 소스에 릴리스 번호를 적어 두면
// bump 를 잊는 순간 이미지가 자기 버전을 틀리게 신고한다.
var version string

func main() {
	log := slog.Default()

	cfg := loadConfig()
	if err := run(cfg, log); err != nil {
		log.Error("기동 실패", "err", err)
		os.Exit(1)
	}
}

type config struct {
	dataDir         string
	listen          string
	scrapeMode      string
	scrapeNamespace string
	scrapeSelector  string
	scrapePort      int
	scrapeInterval  time.Duration
	staticTargets   string
	adminUser       string
	adminPassword   string
	compactInterval time.Duration
	rulesDir        string
	webhookURL      string
}

func loadConfig() config {
	return config{
		dataDir:         env("OBSERVATORY_DATA_DIR", "/data"),
		listen:          env("OBSERVATORY_LISTEN", ":9090"),
		scrapeMode:      env("OBSERVATORY_SCRAPE_MODE", "kubernetes"),
		scrapeNamespace: env("OBSERVATORY_SCRAPE_NAMESPACE", "platform-system"),
		scrapeSelector:  env("OBSERVATORY_SCRAPE_SELECTOR", "app.kubernetes.io/name=nodevitals"),
		scrapePort:      envInt("OBSERVATORY_SCRAPE_PORT", 9847),
		scrapeInterval:  envDur("OBSERVATORY_SCRAPE_INTERVAL", 15*time.Second),
		staticTargets:   env("OBSERVATORY_STATIC_TARGETS", ""),
		adminUser:       env("OBSERVATORY_ADMIN_USER", "admin"),
		adminPassword:   env("OBSERVATORY_ADMIN_PASSWORD", ""),
		compactInterval: envDur("OBSERVATORY_COMPACT_INTERVAL", time.Hour),
		rulesDir:        env("OBSERVATORY_RULES_DIR", ""),
		webhookURL:      env("OBSERVATORY_WEBHOOK_URL", ""),
	}
}

func run(cfg config, log *slog.Logger) error {
	db, err := tsdb.Open(tsdb.DefaultOptions(cfg.dataDir))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	a, err := auth.NewAuthenticator(cfg.adminUser, cfg.adminPassword)
	if err != nil {
		return err
	}

	d, err := makeDiscoverer(cfg)
	if err != nil {
		return err
	}

	scraper := scrape.NewScraper(db, d, scrape.Options{
		Interval: cfg.scrapeInterval,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go scraper.Run(ctx)
	go compactLoop(ctx, db, cfg.compactInterval, log)

	mgr, err := makeRuleManager(cfg, db, log)
	if err != nil {
		return err
	}
	if mgr != nil {
		go mgr.Run(ctx)
	}

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           apiserver.NewServer(apiserver.Deps{DB: db, Ready: scraper.Ready, Auth: a, Rules: mgr}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Info("observatory 기동", "version", versionOrUnknown(), "listen", cfg.listen,
		"dataDir", cfg.dataDir, "scrapeMode", cfg.scrapeMode, "rules", mgr != nil)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func makeDiscoverer(cfg config) (discovery.Discoverer, error) {
	if cfg.scrapeMode == "static" {
		targets, err := discovery.ParseStaticTargets(cfg.staticTargets)
		if err != nil {
			return nil, err
		}
		return discovery.NewStatic(targets), nil
	}
	return discovery.NewKubernetes(discovery.KubeConfig{
		Namespace:     cfg.scrapeNamespace,
		LabelSelector: cfg.scrapeSelector,
		Port:          cfg.scrapePort,
	})
}

// makeRuleManager 는 룰 디렉터리가 지정된 경우에만 매니저를 만든다. 룰이
// 없으면 알림 기능 없이 조회 전용으로 돈다 — 그것도 유효한 구성이다.
func makeRuleManager(cfg config, db *tsdb.DB, log *slog.Logger) (*rules.Manager, error) {
	if cfg.rulesDir == "" {
		return nil, nil
	}
	groups, err := rules.LoadDir(cfg.rulesDir)
	if err != nil {
		return nil, err
	}
	if cfg.webhookURL == "" {
		log.Warn("룰은 있으나 OBSERVATORY_WEBHOOK_URL 이 없다 — 평가는 하되 발송은 하지 않는다",
			"그룹", len(groups))
	}
	n := notify.New(notify.Config{URL: cfg.webhookURL})
	m := rules.NewManager(groups, storage.New(db), alerting.NewSink(n))
	m.SetLogger(log)

	var ruleCount int
	for _, g := range groups {
		ruleCount += len(g.Rules)
	}
	log.Info("알림 룰 로드", "그룹", len(groups), "룰", ruleCount)
	return m, nil
}

// compactLoop 은 주기적으로 블록을 압축한다. 압축은 head 전체를 슬라이스로
// 꺼내므로 순간 메모리가 정상값의 몇 배가 된다 — 주기를 짧게 잡을 이유가 없다.
func compactLoop(ctx context.Context, db *tsdb.DB, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if err := db.Compact(now.UnixMilli()); err != nil {
				log.Warn("압축 실패", "err", err)
			}
		}
	}
}

func versionOrUnknown() string {
	if version == "" {
		return "unknown"
	}
	return version
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}
