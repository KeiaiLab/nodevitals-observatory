package tsdb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ApplyRetention 은 보존기간을 넘긴 블록 디렉터리를 통째로 지운다.
// 해상도에 따라 다른 기간을 적용하며, 삭제한 디렉터리 경로를 돌려준다.
//
// meta.json 을 읽을 수 없는 디렉터리는 **건드리지 않는다** — 쓰다 만 블록일
// 수도 있고 남의 디렉터리일 수도 있어서, 판단이 안 서면 지우지 않는 쪽이 안전하다.
func ApplyRetention(baseDir string, rawRetention, rollupRetention, now int64) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var deleted []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// WriteBlock 의 원자적 rename 창에서 크래시가 남긴 고아 .tmp 는
		// 완결된 meta.json 을 갖지만 정식 블록이 아니다. 보존 판정 대상에서
		// 제외한다 (Querier 도 같은 이유로 .tmp 를 건너뛴다).
		if strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		dir := filepath.Join(baseDir, e.Name())
		meta, err := ReadBlockMeta(dir)
		if err != nil {
			continue // 판단 불가 — 남겨 둔다
		}

		retention := rawRetention
		if meta.Resolution != ResolutionRaw {
			retention = rollupRetention
		}
		if retention <= 0 {
			continue // 0 이하는 "무제한 보존"으로 본다
		}
		if meta.MaxTime < now-retention {
			if err := os.RemoveAll(dir); err != nil {
				return deleted, err
			}
			deleted = append(deleted, dir)
		}
	}
	return deleted, nil
}
