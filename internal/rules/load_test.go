package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

const sampleRules = `# 하드웨어 알림
groups:
  - name: hardware
    interval: 30s
    rules:
      - alert: GpuXidCritical
        expr: increase(nodevitals_events_total{condition="gpu_xid_error"}[5m]) > 0
        for: 0s
        labels:
          severity: critical
          team: platform
        annotations:
          summary: "XID 오류"
      - alert: DiskWearout
        expr: nvme_percentage_used > 90
        for: 10m
        labels:
          severity: warning
`

func TestLoadParsesGroupsAndRules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "hw.yaml", sampleRules)

	groups, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("그룹 %d 개, want 1", len(groups))
	}
	g := groups[0]
	if g.Name != "hardware" {
		t.Errorf("name=%q", g.Name)
	}
	if g.Interval != 30*time.Second {
		t.Errorf("interval=%v, want 30s", g.Interval)
	}
	if len(g.Rules) != 2 {
		t.Fatalf("룰 %d 개, want 2", len(g.Rules))
	}

	r := g.Rules[0]
	if r.Name != "GpuXidCritical" {
		t.Errorf("alert=%q", r.Name)
	}
	if r.For != 0 {
		t.Errorf("for=%v, want 0", r.For)
	}
	if r.Labels["severity"] != "critical" || r.Labels["team"] != "platform" {
		t.Errorf("labels=%+v", r.Labels)
	}
	if r.Annotations["summary"] != "XID 오류" {
		t.Errorf("annotations=%+v", r.Annotations)
	}
	// 식에 중괄호·대괄호·따옴표가 섞여 있어도 온전해야 한다.
	if !strings.Contains(r.Expression(), `condition="gpu_xid_error"`) {
		t.Errorf("expr 이 잘렸다: %q", r.Expression())
	}

	if g.Rules[1].For != 10*time.Minute {
		t.Errorf("두 번째 룰 for=%v, want 10m", g.Rules[1].For)
	}
}

// 식이 틀린 룰이 하나 있으면 그 파일 전체를 거절한다 — 절반만 로드되면
// 어떤 알림이 살아 있는지 아무도 모른다.
func TestLoadRejectsFileWithBadExpression(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bad.yaml", `groups:
  - name: g
    rules:
      - alert: A
        expr: topk(3, x)
`)
	if _, err := LoadDir(dir); err == nil {
		t.Error("미지원 문법이 든 파일이 통과했다")
	}
}

func TestLoadRejectsRecordingRules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "rec.yaml", `groups:
  - name: g
    rules:
      - record: job:x:sum
        expr: sum(x)
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("recording rule 이 통과했다")
	}
	if !strings.Contains(err.Error(), "record") {
		t.Errorf("에러가 record 를 언급하지 않는다: %v", err)
	}
}

// 주석과 빈 줄이 섞여도 읽혀야 한다.
func TestLoadHandlesCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.yaml", `
# 맨 위 주석

groups:

  # 그룹 주석
  - name: g
    rules:
      - alert: A
        expr: x > 1   # 뒤 주석
`)
	groups, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Rules) != 1 {
		t.Fatalf("결과 %+v", groups)
	}
	// 값 뒤 주석은 떨어져야 하지만, 따옴표 안의 # 는 남아야 한다.
	if got := groups[0].Rules[0].Expression(); got != "x > 1" {
		t.Errorf("expr=%q, want %q", got, "x > 1")
	}
}

func TestLoadKeepsHashInsideQuotes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "q.yaml", `groups:
  - name: g
    rules:
      - alert: A
        expr: x > 1
        annotations:
          summary: "번호 #42"
`)
	groups, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := groups[0].Rules[0].Annotations["summary"]; got != "번호 #42" {
		t.Errorf("summary=%q — 따옴표 안의 # 가 주석으로 잘렸다", got)
	}
}

func TestLoadDirIgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes.txt", "이건 룰이 아니다")
	write(t, dir, "ok.yaml", `groups:
  - name: g
    rules:
      - alert: A
        expr: x > 1
`)
	groups, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("그룹 %d 개, want 1 (.txt 는 무시)", len(groups))
	}
}
