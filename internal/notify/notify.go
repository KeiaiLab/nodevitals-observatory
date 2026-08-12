// Package notify 는 알림을 외부로 내보낸다.
//
// 채널은 webhook 하나뿐이다. 메일·채팅 분배는 수신 측이 이미 잘 하는 일이고,
// 그것까지 품으면 SMTP·OAuth·템플릿이 따라와 "차트 하나 = 파드 하나" 가
// 무거워진다.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status 는 알림이 시작됐는지 풀렸는지다.
type Status string

const (
	StatusFiring   Status = "firing"
	StatusResolved Status = "resolved"
)

// Alert 는 내보낼 알림 하나다.
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Status      Status            `json:"status"`
	Value       float64           `json:"value,omitempty"`
}

// Config 는 발송기 설정이다. 0 인 값은 기본치로 채워진다.
type Config struct {
	URL string
	// RepeatInterval 안에는 같은 알림을 다시 보내지 않는다. 30초마다 같은
	// 내용이 오면 사람이 알림 자체를 꺼 버리고, 그게 가장 나쁜 결과다.
	RepeatInterval time.Duration
	MaxAttempts    int
	Timeout        time.Duration

	// backoffBase 는 테스트에서 대기를 줄이기 위한 것이다. 비워 두면 기본치.
	backoffBase time.Duration
}

// Notifier 는 webhook 으로 알림을 보낸다. 동시 호출에 안전하다.
type Notifier struct {
	cfg    Config
	client *http.Client

	mu       sync.Mutex
	lastSent map[string]time.Time // 지문 → 마지막 발송 시각

	sentN    int64
	droppedN int64
}

func New(cfg Config) *Notifier {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.RepeatInterval <= 0 {
		cfg.RepeatInterval = 4 * time.Hour
	}
	if cfg.backoffBase <= 0 {
		cfg.backoffBase = 200 * time.Millisecond
	}
	return &Notifier{
		cfg:      cfg,
		client:   &http.Client{Timeout: cfg.Timeout},
		lastSent: map[string]time.Time{},
	}
}

// Sent 는 성공한 발송 횟수, Dropped 는 재시도를 소진해 버린 횟수다. 둘 다
// /metrics 로 나가야 한다 — 발송이 조용히 실패하면 알림이 오지 않은 것을
// 아무도 모른다.
func (n *Notifier) Sent() int64    { n.mu.Lock(); defer n.mu.Unlock(); return n.sentN }
func (n *Notifier) Dropped() int64 { n.mu.Lock(); defer n.mu.Unlock(); return n.droppedN }

// Send 는 알림 묶음을 한 번의 요청으로 보낸다. RepeatInterval 안에 이미 보낸
// firing 알림은 빠지고, resolved 는 언제나 나간다.
func (n *Notifier) Send(alerts []Alert) error {
	now := time.Now()
	out := n.filter(alerts, now)
	if len(out) == 0 {
		return nil
	}

	body, err := json.Marshal(map[string]any{"alerts": out})
	if err != nil {
		return fmt.Errorf("notify: 페이로드 직렬화: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < n.cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(n.backoff(attempt))
		}
		lastErr = n.post(body)
		if lastErr == nil {
			n.markSent(out, now)
			return nil
		}
	}

	n.mu.Lock()
	n.droppedN++
	n.mu.Unlock()
	return fmt.Errorf("notify: %d 회 시도 후 포기(%d 건 유실): %w", n.cfg.MaxAttempts, len(out), lastErr)
}

func (n *Notifier) post(body []byte) error {
	req, err := http.NewRequest(http.MethodPost, n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

// backoff 는 지수 증가에 지터를 섞는다. 지터가 없으면 여러 알림이 같은
// 순간에 재시도를 몰아 수신 측을 다시 넘어뜨린다.
func (n *Notifier) backoff(attempt int) time.Duration {
	d := n.cfg.backoffBase << (attempt - 1)
	jitter := time.Duration(time.Now().UnixNano() % int64(n.cfg.backoffBase))
	return d + jitter
}

func (n *Notifier) filter(alerts []Alert, now time.Time) []Alert {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := make([]Alert, 0, len(alerts))
	for _, a := range alerts {
		if a.Status == StatusResolved {
			// 해소는 억제하지 않는다 — 울린 알림이 풀렸다는 사실은 반드시
			// 가야 한다. 보낸 기록도 지워 다음 발생이 새 알림이 되게 한다.
			delete(n.lastSent, fingerprint(a.Labels))
			out = append(out, a)
			continue
		}
		fp := fingerprint(a.Labels)
		if last, ok := n.lastSent[fp]; ok && now.Sub(last) < n.cfg.RepeatInterval {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (n *Notifier) markSent(alerts []Alert, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sentN++
	for _, a := range alerts {
		if a.Status == StatusFiring {
			n.lastSent[fingerprint(a.Labels)] = now
		}
	}
}

// fingerprint 는 라벨셋의 안정적인 키다.
func fingerprint(lbls map[string]string) string {
	keys := make([]string, 0, len(lbls))
	for k := range lbls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte(0)
		sb.WriteString(lbls[k])
		sb.WriteByte(1)
	}
	return sb.String()
}
