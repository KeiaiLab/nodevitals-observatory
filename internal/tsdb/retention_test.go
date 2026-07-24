package tsdb

import (
	"os"
	"path/filepath"
	"testing"
)

// makeBlockDir 는 meta.json 만 있는 가짜 블록 디렉터리를 만든다.
func makeBlockDir(t *testing.T, base string, minT, maxT int64, res string) string {
	t.Helper()
	h := NewHead()
	ls := NewLabels(Label{"node", "e101"}, Label{"res", res})
	h.Append(ls, minT, 1)
	h.Append(ls, maxT, 2)
	m, _ := NewMatcher(MatchEqual, "node", "e101")
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
