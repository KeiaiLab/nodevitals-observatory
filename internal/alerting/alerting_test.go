package alerting

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"github.com/KeiaiLab/nodevitals-observatory/internal/notify"
	"github.com/KeiaiLab/nodevitals-observatory/internal/rules"
	"github.com/KeiaiLab/nodevitals-observatory/internal/storage"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// 룰 파일 → 로드 → 평가 → 발송까지 한 줄로 흐르는지 본다. 조각을 따로
// 검증한 뒤 남는 위험은 이음매뿐이고, 이 경로가 곧 제품의 알림 기능이다.
func TestRuleFileToWebhook(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies [][]byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		mu.Lock()
		bodies = append(bodies, buf)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	// 1) 룰 파일
	dir := t.TempDir()
	rf := filepath.Join(dir, "hw.yaml")
	if err := os.WriteFile(rf, []byte(`groups:
  - name: hardware
    interval: 30s
    rules:
      - alert: GpuTooHot
        expr: gpu_temperature_celsius > 85
        for: 0s
        labels:
          severity: critical
        annotations:
          summary: "GPU 과열"
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	groups, err := rules.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	// 2) 데이터
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	lset := labels.LabelsFromMap(map[string]string{
		labels.MetricName: "gpu_temperature_celsius",
		"node":            "e106",
		"gpu":             "0",
	})
	if err := db.Append(lset, 1000, 91); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 3) 조립
	n := notify.New(notify.Config{URL: srv.URL, Timeout: 2 * time.Second})
	m := rules.NewManager(groups, storage.New(db), NewSink(n))

	// 4) 평가
	m.EvalOnce(1000)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("발송 %d 건, want 1", len(bodies))
	}
	var payload struct {
		Alerts []struct {
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			Status      string            `json:"status"`
			Value       float64           `json:"value"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(bodies[0], &payload); err != nil {
		t.Fatalf("페이로드: %v (%q)", err, bodies[0])
	}
	if len(payload.Alerts) != 1 {
		t.Fatalf("알림 %d 건: %+v", len(payload.Alerts), payload)
	}
	a := payload.Alerts[0]
	if a.Labels["alertname"] != "GpuTooHot" {
		t.Errorf("alertname=%q", a.Labels["alertname"])
	}
	if a.Labels["severity"] != "critical" {
		t.Errorf("severity=%q", a.Labels["severity"])
	}
	if a.Labels["node"] != "e106" || a.Labels["gpu"] != "0" {
		t.Errorf("원본 라벨이 유지되지 않았다: %+v", a.Labels)
	}
	if a.Annotations["summary"] != "GPU 과열" {
		t.Errorf("summary=%q", a.Annotations["summary"])
	}
	if a.Status != "firing" || a.Value != 91 {
		t.Errorf("status=%q value=%v", a.Status, a.Value)
	}

	// 5) 상태가 안 바뀌면 다시 보내지 않는다
	m.EvalOnce(2000)
	if len(bodies) != 1 {
		t.Errorf("재평가 후 발송 %d 건, want 1", len(bodies))
	}
}
