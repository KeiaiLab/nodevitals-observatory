package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type capture struct {
	mu     sync.Mutex
	bodies [][]byte
	status int32
	calls  int32
}

func (c *capture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&c.calls, 1)
		st := int(atomic.LoadInt32(&c.status))
		if st == 0 {
			st = 200
		}
		if st != 200 {
			w.WriteHeader(st)
			return
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		c.mu.Lock()
		c.bodies = append(c.bodies, buf)
		c.mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mkAlert(name, sev string) Alert {
	return Alert{
		Labels:      map[string]string{"alertname": name, "severity": sev},
		Annotations: map[string]string{"summary": name + " 발생"},
		Status:      StatusFiring,
	}
}

func TestSendsFiringAlert(t *testing.T) {
	c := &capture{}
	srv := c.server(t)
	n := New(Config{URL: srv.URL, Timeout: 2 * time.Second})

	if err := n.Send([]Alert{mkAlert("A", "critical")}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := atomic.LoadInt32(&c.calls); got != 1 {
		t.Fatalf("호출 %d 회, want 1", got)
	}
	c.mu.Lock()
	body := c.bodies[0]
	c.mu.Unlock()

	var payload struct {
		Alerts []struct {
			Labels map[string]string `json:"labels"`
			Status string            `json:"status"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("페이로드 파싱: %v (본문 %q)", err, body)
	}
	if len(payload.Alerts) != 1 || payload.Alerts[0].Labels["alertname"] != "A" {
		t.Errorf("페이로드 %+v", payload)
	}
	if payload.Alerts[0].Status != "firing" {
		t.Errorf("status=%q, want firing", payload.Alerts[0].Status)
	}
}

// 같은 알림을 repeat 안에 다시 보내면 안 된다 — 30초마다 같은 내용이 오면
// 사람이 알림을 끄게 되고, 그게 가장 나쁜 결과다.
func TestSuppressesRepeatWithinInterval(t *testing.T) {
	c := &capture{}
	srv := c.server(t)
	n := New(Config{URL: srv.URL, RepeatInterval: time.Hour, Timeout: 2 * time.Second})

	a := mkAlert("A", "critical")
	_ = n.Send([]Alert{a})
	_ = n.Send([]Alert{a})

	if got := atomic.LoadInt32(&c.calls); got != 1 {
		t.Errorf("호출 %d 회, want 1 (repeat 안에서는 억제)", got)
	}
}

// 해소는 억제 대상이 아니다 — 울린 알림이 풀렸다는 사실은 반드시 가야 한다.
func TestResolvedIsAlwaysSent(t *testing.T) {
	c := &capture{}
	srv := c.server(t)
	n := New(Config{URL: srv.URL, RepeatInterval: time.Hour, Timeout: 2 * time.Second})

	a := mkAlert("A", "critical")
	_ = n.Send([]Alert{a})

	r := a
	r.Status = StatusResolved
	_ = n.Send([]Alert{r})

	if got := atomic.LoadInt32(&c.calls); got != 2 {
		t.Errorf("호출 %d 회, want 2 (해소는 억제하지 않는다)", got)
	}
}

// 5xx 는 재시도하고, 소진하면 드롭 카운터를 올린다. 조용히 사라지면 알림이
// 안 온 것을 아무도 모른다.
func TestRetriesThenCountsDrop(t *testing.T) {
	c := &capture{}
	atomic.StoreInt32(&c.status, 500)
	srv := c.server(t)
	n := New(Config{URL: srv.URL, MaxAttempts: 3, Timeout: time.Second, backoffBase: time.Millisecond})

	err := n.Send([]Alert{mkAlert("A", "critical")})
	if err == nil {
		t.Error("전부 실패했는데 에러가 나오지 않았다")
	}
	if got := atomic.LoadInt32(&c.calls); got != 3 {
		t.Errorf("시도 %d 회, want 3", got)
	}
	if n.Dropped() != 1 {
		t.Errorf("드롭 카운터=%d, want 1", n.Dropped())
	}
	if n.Sent() != 0 {
		t.Errorf("전송 카운터=%d, want 0", n.Sent())
	}
}

// 실패했다가 성공하면 드롭이 아니다.
func TestRecoversBeforeExhausting(t *testing.T) {
	c := &capture{}
	atomic.StoreInt32(&c.status, 500)
	srv := c.server(t)
	n := New(Config{URL: srv.URL, MaxAttempts: 5, Timeout: time.Second, backoffBase: time.Millisecond})

	go func() {
		time.Sleep(20 * time.Millisecond)
		atomic.StoreInt32(&c.status, 200)
	}()

	if err := n.Send([]Alert{mkAlert("A", "critical")}); err != nil {
		t.Fatalf("복구했는데 에러: %v", err)
	}
	if n.Dropped() != 0 {
		t.Errorf("드롭=%d, want 0", n.Dropped())
	}
	if n.Sent() != 1 {
		t.Errorf("전송=%d, want 1", n.Sent())
	}
}

// 빈 목록은 아무것도 보내지 않는다 — 빈 알림이 가면 수신 측이 혼란스럽다.
func TestEmptyListSendsNothing(t *testing.T) {
	c := &capture{}
	srv := c.server(t)
	n := New(Config{URL: srv.URL, Timeout: time.Second})
	if err := n.Send(nil); err != nil {
		t.Fatalf("Send(nil): %v", err)
	}
	if got := atomic.LoadInt32(&c.calls); got != 0 {
		t.Errorf("호출 %d 회, want 0", got)
	}
}
