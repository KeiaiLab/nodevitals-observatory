package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/auth"
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/rules"
	"github.com/KeiaiLab/nodevitals-observatory/internal/storage"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

func newDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func add(t *testing.T, db *tsdb.DB, name string, kv map[string]string, ts int64, v float64) {
	t.Helper()
	m := map[string]string{labels.MetricName: name}
	for k, val := range kv {
		m[k] = val
	}
	if err := db.Append(labels.LabelsFromMap(m), ts, v); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

// 로그인해 세션 쿠키를 얻는다.
func loggedIn(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	jar, err := newJar()
	if err != nil {
		t.Fatalf("jar: %v", err)
	}
	c := &http.Client{Jar: jar}
	resp, err := c.Post(srv.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"secret-pass"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		t.Fatalf("login status=%d", resp.StatusCode)
	}
	return c
}

func newServer(t *testing.T, db *tsdb.DB, m *rules.Manager) *httptest.Server {
	t.Helper()
	a, err := auth.NewAuthenticator("admin", "secret-pass")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	srv := httptest.NewServer(NewServer(Deps{DB: db, Auth: a, Rules: m}))
	t.Cleanup(srv.Close)
	return srv
}

// 프로토타입의 질의는 셀렉터만 받았다. 이제 함수가 통해야 한다.
func TestQueryAcceptsFunctions(t *testing.T) {
	db := newDB(t)
	add(t, db, "x", map[string]string{"n": "a"}, nowMS(), 2)
	add(t, db, "x", map[string]string{"n": "b"}, nowMS(), 3)

	srv := newServer(t, db, nil)
	c := loggedIn(t, srv)

	resp, err := c.Get(srv.URL + "/api/v1/query?query=" + url.QueryEscape("sum(x)"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200 — 함수가 파싱되지 않는다", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "success" || body.Data.ResultType != "vector" {
		t.Fatalf("응답 %+v", body)
	}
	if len(body.Data.Result) != 1 {
		t.Fatalf("결과 %d 개, want 1 (sum 은 전체를 하나로)", len(body.Data.Result))
	}
	if got := body.Data.Result[0].Value[1]; got != "5" {
		t.Errorf("sum(x)=%v, want 5", got)
	}
}

func TestQueryRejectsUnsupportedSyntax(t *testing.T) {
	srv := newServer(t, newDB(t), nil)
	c := loggedIn(t, srv)
	resp, err := c.Get(srv.URL + "/api/v1/query?query=" + url.QueryEscape("topk(3, x)"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 400 {
		t.Errorf("status=%d, want 400 (미지원 문법은 거절)", resp.StatusCode)
	}
}

// 데이터 경로는 인증을 지나야 한다 — 실 클러스터 메트릭은 노드 이름·디바이스
// 시리얼까지 담고 있어 익명 열람 대상이 아니다.
func TestDataRoutesRequireAuth(t *testing.T) {
	srv := newServer(t, newDB(t), nil)
	for _, path := range []string{"/api/v1/query?query=x", "/api/v1/rules", "/api/v1/alerts", "/api/v1/labels"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s status=%d, want 401", path, resp.StatusCode)
		}
	}
}

func TestHealthzIsPublic(t *testing.T) {
	srv := newServer(t, newDB(t), nil)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthz status=%d", resp.StatusCode)
	}
}

// 룰 상태가 Prometheus 스키마로 나온다.
func TestRulesAndAlertsExposeState(t *testing.T) {
	db := newDB(t)
	ts := nowMS()
	add(t, db, "gpu_temp", map[string]string{"node": "e106"}, ts, 91)

	r, err := rules.NewRule("GpuTooHot", "gpu_temp > 85", 0,
		map[string]string{"severity": "critical"}, map[string]string{"summary": "과열"})
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	g := &rules.Group{Name: "hardware", Interval: time.Minute, Rules: []*rules.Rule{r}}
	m := rules.NewManager([]*rules.Group{g}, storage.New(db), nopSink{})
	m.EvalOnce(ts)

	srv := newServer(t, db, m)
	c := loggedIn(t, srv)

	resp, err := c.Get(srv.URL + "/api/v1/rules")
	if err != nil {
		t.Fatalf("GET rules: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rb struct {
		Data struct {
			Groups []struct {
				Name  string `json:"name"`
				Rules []struct {
					Name   string `json:"name"`
					State  string `json:"state"`
					Type   string `json:"type"`
					Alerts []struct {
						Labels map[string]string `json:"labels"`
						State  string            `json:"state"`
					} `json:"alerts"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	if len(rb.Data.Groups) != 1 || len(rb.Data.Groups[0].Rules) != 1 {
		t.Fatalf("rules 응답 %+v", rb)
	}
	rule := rb.Data.Groups[0].Rules[0]
	if rule.Name != "GpuTooHot" || rule.State != "firing" || rule.Type != "alerting" {
		t.Errorf("룰 %+v", rule)
	}
	if len(rule.Alerts) != 1 || rule.Alerts[0].Labels["severity"] != "critical" {
		t.Errorf("알림 %+v", rule.Alerts)
	}

	resp2, err := c.Get(srv.URL + "/api/v1/alerts")
	if err != nil {
		t.Fatalf("GET alerts: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	var ab struct {
		Data struct {
			Alerts []struct {
				Labels map[string]string `json:"labels"`
			} `json:"alerts"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&ab); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(ab.Data.Alerts) != 1 || ab.Data.Alerts[0].Labels["alertname"] != "GpuTooHot" {
		t.Errorf("alerts 응답 %+v", ab)
	}
}

// 룰이 없어도 빈 목록을 낸다 — null 이 나가면 프론트가 깨진다.
func TestRulesWithoutManagerReturnsEmptyList(t *testing.T) {
	srv := newServer(t, newDB(t), nil)
	c := loggedIn(t, srv)
	resp, err := c.Get(srv.URL + "/api/v1/rules")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rb struct {
		Data struct {
			Groups []any `json:"groups"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rb.Data.Groups == nil {
		t.Error("groups 가 null 이다 — 빈 배열이어야 한다")
	}
}

type nopSink struct{}

func (nopSink) Notify([]rules.Notification) error { return nil }

func nowMS() int64 { return time.Now().UnixMilli() }
