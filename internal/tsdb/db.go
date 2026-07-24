package tsdb

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Options 는 DB 동작을 결정한다. 기간은 모두 밀리초다.
type Options struct {
	Dir             string
	SegmentSize     int64
	BlockDuration   int64
	RawRetention    int64
	RollupRetention int64
}

// DefaultOptions 는 설계 문서 §8 의 기본값이다 — 2시간 블록, raw 7일,
// 롤업 90일.
func DefaultOptions(dir string) Options {
	const (
		hour = int64(60 * 60 * 1000)
		day  = 24 * hour
	)
	return Options{
		Dir:             dir,
		SegmentSize:     defaultSegmentSize,
		BlockDuration:   2 * hour,
		RawRetention:    7 * day,
		RollupRetention: 90 * day,
	}
}

func walDir(dir string) string    { return filepath.Join(dir, "wal") }
func blocksDir(dir string) string { return filepath.Join(dir, "blocks") }

// DB 는 WAL·head·블록을 묶은 저장 엔진의 공개 표면이다.
type DB struct {
	mtx sync.Mutex

	opts Options
	head *Head
	wal  *WAL

	// knownRefs 는 이번 WAL 세그먼트에 시리즈 레코드를 이미 쓴 ref 다.
	// 시리즈 레코드가 없으면 재생 시 라벨셋을 모르므로 샘플이 버려진다.
	knownRefs map[uint64]struct{}
}

func Open(opts Options) (*DB, error) {
	if opts.SegmentSize <= 0 {
		opts.SegmentSize = defaultSegmentSize
	}
	if err := os.MkdirAll(blocksDir(opts.Dir), 0o755); err != nil {
		return nil, err
	}

	// 재시작 시 WAL 을 재생해 head 를 복원한다.
	head, err := RecoverHead(walDir(opts.Dir))
	if err != nil {
		return nil, err
	}
	w, err := OpenWAL(walDir(opts.Dir), opts.SegmentSize)
	if err != nil {
		return nil, err
	}

	db := &DB{opts: opts, head: head, wal: w, knownRefs: map[uint64]struct{}{}}
	// 복구된 시리즈는 이미 WAL 에 레코드가 있다.
	for ref := range head.series {
		db.knownRefs[ref] = struct{}{}
	}
	return db, nil
}

// Append 는 샘플 하나를 넣는다.
//
// head 에 먼저 붙이고 WAL 에 기록한다 — ref 를 알아야 WAL 레코드를 쓸 수
// 있기 때문이다. 이 순서 때문에 "head 에는 있는데 WAL 에는 없는" 창이
// 잠깐 생기지만, 그 창에서 죽으면 어차피 head 도 함께 사라지므로 복구
// 결과는 일관된다.
func (db *DB) Append(lset Labels, t int64, v float64) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	ref, err := db.head.Append(lset, t, v)
	if err != nil {
		return err
	}
	if _, ok := db.knownRefs[ref]; !ok {
		if err := db.wal.LogSeries(ref, lset); err != nil {
			return err
		}
		db.knownRefs[ref] = struct{}{}
	}
	return db.wal.LogSamples([]RefSample{{Ref: ref, T: t, V: v}})
}

func (db *DB) Sync() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()
	return db.wal.Sync()
}

// Querier 는 [mint,maxt] 에 걸치는 블록만 열어 head 와 함께 조회한다.
// 두 번째 반환값은 연 블록을 닫는 함수이며 반드시 호출해야 한다.
func (db *DB) Querier(mint, maxt int64) (*Querier, func() error, error) {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	entries, err := os.ReadDir(blocksDir(db.opts.Dir))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}

	var blocks []*Block
	closeAll := func() error {
		var firstErr error
		for _, b := range blocks {
			if err := b.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// WriteBlock 이 <dir>.tmp 에 쓰고 rename 하므로, meta.json 완료~rename
		// 완료 사이 크래시가 남긴 고아 .tmp 는 완결된 블록처럼 보이지만
		// 정식 블록이 아니다. 열면 같은 데이터가 (아직 안 지워진) WAL 재생분과
		// 중복되므로 건너뛴다.
		if strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		dir := filepath.Join(blocksDir(db.opts.Dir), e.Name())
		meta, err := ReadBlockMeta(dir)
		if err != nil {
			continue // 쓰다 만 블록 — 건너뛴다
		}
		if meta.MaxTime < mint || meta.MinTime > maxt {
			continue
		}
		b, err := OpenBlock(dir)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		blocks = append(blocks, b)
	}

	return NewQuerier(mint, maxt, db.head, blocks), closeAll, nil
}

// Compact 는 head 를 블록으로 굳히고, 롤업 블록을 만들고, WAL 을 비우고,
// 보존기간이 지난 블록을 지운다. now 는 보존 판정 기준 시각이다.
//
// 시간을 인자로 받는 이유는 테스트 가능성이다 — 내부에서 time.Now 를 부르면
// 보존 로직을 결정론적으로 검증할 수 없다.
func (db *DB) Compact(now int64) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	// 매처 없이 부르면 Head.Select 는 전체 시리즈를 낸다 (Task 8 계약).
	series := db.head.Select()
	if len(series) > 0 {
		if _, err := WriteBlock(blocksDir(db.opts.Dir), series, ResolutionRaw); err != nil {
			return err
		}
		if rolled := RollupSeries(series, rollupInterval); len(rolled) > 0 {
			if _, err := WriteBlock(blocksDir(db.opts.Dir), rolled, Resolution5m); err != nil {
				return err
			}
		}
		db.head.Reset()
		db.knownRefs = map[uint64]struct{}{}
		if err := db.wal.Truncate(); err != nil {
			return err
		}
	}

	_, err := ApplyRetention(blocksDir(db.opts.Dir), db.opts.RawRetention, db.opts.RollupRetention, now)
	return err
}

func (db *DB) Close() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()
	return db.wal.Close()
}
