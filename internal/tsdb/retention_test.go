package tsdb

import (
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"os"
	"path/filepath"
	"testing"
)

// makeBlockDir 는 meta.json 만 있는 가짜 블록 디렉터리를 만든다.
func makeBlockDir(t *testing.T, base string, minT, maxT int64, res string) string {
	t.Helper()
	h := NewHead()
	ls := labels.NewLabels(labels.Label{"node", "e101"}, labels.Label{"res", res})
	h.Append(ls, minT, 1)
	h.Append(ls, maxT, 2)
	m, _ := labels.NewMatcher(labels.MatchEqual, "node", "e101")
	dir, err := WriteBlock(base, h.Select(m), res)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestApplyRetention_만료된_블록만_지운다(t *testing.T) {
	base := t.TempDir()
	const day = int64(24 * 60 * 60 * 1000)
	now := int64(100 * day)

	fresh := makeBlockDir(t, base, now-day, now-day+1000, ResolutionRaw)              // raw, 1일 전 → 유지
	stale := makeBlockDir(t, base, now-10*day, now-10*day+1000, ResolutionRaw)        // raw, 10일 전 → 삭제
	rollupFresh := makeBlockDir(t, base, now-30*day, now-30*day+1000, Resolution5m)   // 롤업, 30일 → 유지
	rollupStale := makeBlockDir(t, base, now-120*day, now-120*day+1000, Resolution5m) // 롤업, 120일 → 삭제

	deleted, err := ApplyRetention(base, 7*day, 90*day, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 {
		t.Fatalf("삭제된 블록: got %d, want 2 (%v)", len(deleted), deleted)
	}

	for _, keep := range []string{fresh, rollupFresh} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("유지돼야 할 블록이 지워졌다: %s", keep)
		}
	}
	for _, gone := range []string{stale, rollupStale} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("삭제돼야 할 블록이 남았다: %s", gone)
		}
	}
}

func TestApplyRetention_meta가_없는_디렉터리는_건너뛴다(t *testing.T) {
	base := t.TempDir()
	junk := filepath.Join(base, "0000-0000-raw")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatal(err)
	}

	deleted, err := ApplyRetention(base, 1, 1, 1<<40)
	if err != nil {
		t.Fatalf("meta 없는 디렉터리에서 실패하면 안 된다: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("건너뛰어야 한다: %v", deleted)
	}
	if _, err := os.Stat(junk); err != nil {
		t.Fatal("판단 불가한 디렉터리를 지웠다")
	}
}

func TestApplyRetention_빈_디렉터리는_에러가_아니다(t *testing.T) {
	if _, err := ApplyRetention(filepath.Join(t.TempDir(), "없음"), 1, 1, 1); err != nil {
		t.Fatalf("미존재 디렉터리: %v", err)
	}
}

func TestApplyRetention_파일은_무시한다(t *testing.T) {
	base := t.TempDir()
	// 블록 디렉터리와 파일 혼재
	const day = int64(24 * 60 * 60 * 1000)
	now := int64(100 * day)

	block := makeBlockDir(t, base, now-10*day, now-10*day+1000, ResolutionRaw)

	// 파일 생성 (디렉터리 아님)
	f := filepath.Join(base, "some_file.txt")
	if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted, err := ApplyRetention(base, 7*day, 90*day, now)
	if err != nil {
		t.Fatal(err)
	}
	// 파일은 무시, 블록만 삭제
	if len(deleted) != 1 {
		t.Fatalf("파일은 무시해야 한다: deleted=%v", deleted)
	}
	if deleted[0] != block {
		t.Fatalf("블록 삭제: got %s, want %s", deleted[0], block)
	}
	// 파일은 남아 있어야 함
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("파일이 지워졌다: %v", err)
	}
}

func TestApplyRetention_무제한_보존(t *testing.T) {
	base := t.TempDir()
	const day = int64(24 * 60 * 60 * 1000)
	now := int64(100 * day)

	// retention <= 0 은 무제한 보존
	stale := makeBlockDir(t, base, now-120*day, now-120*day+1000, ResolutionRaw)

	// raw 보존 기간 = 0 (무제한) → 아무것도 삭제 안 함
	deleted, err := ApplyRetention(base, 0, 90*day, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("retention=0 은 무제한: 삭제된 것 %d개", len(deleted))
	}
	// 블록이 남아 있어야 함
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("유지돼야 할 블록이 지워졌다: %v", err)
	}

	// rollup 보존 기간 = -1 (무제한) → 아무것도 삭제 안 함
	staleRollup := makeBlockDir(t, base, now-120*day, now-120*day+1000, Resolution5m)
	deleted, err = ApplyRetention(base, 7*day, -1, now)
	if err != nil {
		t.Fatal(err)
	}
	// raw 블록 1개(stale)만 삭제, rollup은 유지
	if len(deleted) != 1 {
		t.Fatalf("rollup retention=-1: 삭제된 것 %d개 (1개여야 함)", len(deleted))
	}
	if _, err := os.Stat(staleRollup); err != nil {
		t.Fatalf("롤업 블록이 지워졌다: %v", err)
	}
}

func TestApplyRetention_고아_tmp_디렉터리는_건드리지_않는다(t *testing.T) {
	base := t.TempDir()
	const day = int64(24 * 60 * 60 * 1000)
	now := int64(100 * day)

	// 만료된(오래된) 시간범위를 가진 .tmp 고아를 직접 만든다.
	orphan := filepath.Join(base, "0000000000000-0000000001000-raw.tmp")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"version":"1","minTime":0,"maxTime":1000,"series":1,"samples":1,"resolution":"raw"}`
	if err := os.WriteFile(filepath.Join(orphan, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	deleted, err := ApplyRetention(base, 7*day, 90*day, now)
	if err != nil {
		t.Fatal(err)
	}
	// .tmp 는 판정 대상이 아니므로 삭제 목록에 없어야 하고, 여전히 존재해야 한다.
	for _, d := range deleted {
		if filepath.Base(d) == filepath.Base(orphan) {
			t.Fatalf(".tmp 고아를 보존 판정으로 지웠다: %v", deleted)
		}
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf(".tmp 고아가 사라졌다: %v", err)
	}
}
