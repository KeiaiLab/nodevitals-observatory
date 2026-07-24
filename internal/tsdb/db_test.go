package tsdb

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDB_넣은_샘플을_되읽는다(t *testing.T) {
	db, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 100; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}

	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("시리즈: got %d, want 1", len(got))
	}
	if s := collect(t, got[0]); len(s) != 100 {
		t.Fatalf("샘플: got %d, want 100", len(s))
	}
}

// M1 완료 기준 ①: 무작위 샘플 왕복 동일성.
func TestDB_무작위_샘플_왕복_동일성(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}

	rng := rand.New(rand.NewSource(42))
	type key struct{ node, metric string }
	want := map[key][]sample{}

	nodes := []string{"e101", "e102", "e21"}
	metrics := []string{"node_load1", "node_memory_MemFree_bytes", "gpu_temp"}
	for _, n := range nodes {
		for _, mt := range metrics {
			ls := NewLabels(Label{MetricName, mt}, Label{"node", n})
			var ts int64
			for i := 0; i < 500; i++ {
				ts += 14000 + rng.Int63n(3000) // 지터 있는 간격
				v := rng.Float64() * 1000
				if err := db.Append(ls, ts, v); err != nil {
					t.Fatalf("Append: %v", err)
				}
				want[key{n, mt}] = append(want[key{n, mt}], sample{ts, v})
			}
		}
	}

	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	for k, ws := range want {
		mn, _ := NewMatcher(MatchEqual, "node", k.node)
		mm, _ := NewMatcher(MatchEqual, MetricName, k.metric)
		got := q.Select(mn, mm)
		if len(got) != 1 {
			t.Fatalf("%v: 시리즈 %d개", k, len(got))
		}
		gs := collect(t, got[0])
		if len(gs) != len(ws) {
			t.Fatalf("%v: 샘플 %d, want %d", k, len(gs), len(ws))
		}
		for i := range ws {
			if gs[i] != ws[i] {
				t.Fatalf("%v 샘플 %d: got %+v, want %+v", k, i, gs[i], ws[i])
			}
		}
	}
	closeQ()
	db.Close()
}

// M1 완료 기준 ②: 크래시 주입 후 WAL 복구 무손실.
func TestDB_크래시_후_재오픈시_데이터가_살아있다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 200; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Sync(); err != nil {
		t.Fatal(err)
	}
	// Close 를 부르지 않는다 = 프로세스가 죽은 상황.

	db2, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatalf("재오픈: %v", err)
	}
	defer db2.Close()

	q, closeQ, err := db2.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("복구된 시리즈: got %d, want 1", len(got))
	}
	s := collect(t, got[0])
	if len(s) != 200 {
		t.Fatalf("복구된 샘플: got %d, want 200", len(s))
	}
	for i := range s {
		if s[i].t != int64(i)*15000 || s[i].v != float64(i) {
			t.Fatalf("샘플 %d: got %+v", i, s[i])
		}
	}
}

func TestDB_Compact가_블록과_롤업을_만든다(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions(dir)
	db, err := Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	// 2시간(블록 길이)을 넘는 구간을 채운다.
	for i := 0; i < 600; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	now := int64(600) * 15000
	if err := db.Compact(now); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// head 는 비었고 블록에서 읽혀야 한다.
	if db.head.NumSeries() != 0 {
		t.Fatalf("Compact 후 head 가 비어야 한다: %d", db.head.NumSeries())
	}

	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()

	m, _ := NewMatcher(MatchEqual, "node", "e101")
	raw, _ := NewMatcher(MatchEqual, RollupLabel, "")
	got := q.Select(m, raw)
	if len(got) != 1 {
		t.Fatalf("raw 시리즈: got %d, want 1", len(got))
	}
	if s := collect(t, got[0]); len(s) != 600 {
		t.Fatalf("Compact 후 raw 샘플: got %d, want 600", len(s))
	}

	// 롤업 시리즈도 존재해야 한다.
	sumM, _ := NewMatcher(MatchEqual, RollupLabel, "sum")
	rollup := q.Select(m, sumM)
	if len(rollup) != 1 {
		t.Fatalf("롤업 sum 시리즈: got %d, want 1", len(rollup))
	}
	// 600샘플 × 15초 = 150분 → 5분 버킷 30개
	if s := collect(t, rollup[0]); len(s) != 30 {
		t.Fatalf("롤업 버킷: got %d, want 30", len(s))
	}
}

func TestDB_Compact가_보존기간_초과_블록을_지운다(t *testing.T) {
	dir := t.TempDir()
	const day = int64(24 * 60 * 60 * 1000)
	opts := DefaultOptions(dir)
	opts.RawRetention = 7 * day
	opts.RollupRetention = 90 * day

	db, err := Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		db.Append(ls, int64(i)*15000, float64(i))
	}
	// 아주 먼 미래를 now 로 주면 방금 만든 블록이 전부 만료된다.
	if err := db.Compact(1000 * day); err != nil {
		t.Fatal(err)
	}

	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	if got := q.Select(m); len(got) != 0 {
		t.Fatalf("보존기간이 지난 데이터가 남았다: %d 시리즈", len(got))
	}
}

func TestDB_Compact_후_WAL이_비워진다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	ls := NewLabels(Label{"node", "e101"})
	for i := 0; i < 50; i++ {
		db.Append(ls, int64(i)*15000, float64(i))
	}
	if err := db.Compact(int64(50) * 15000); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// WAL 을 재생해도 아무것도 안 나와야 한다 (블록으로 옮겨졌으므로).
	n := 0
	if err := ReplayWAL(walDir(dir),
		func(uint64, Labels) error { return nil },
		func(ss []RefSample) error { n += len(ss); return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("Compact 후 WAL 에 샘플이 남았다: %d", n)
	}
}

// --- 아래는 브리프에 없던 추가 테스트다 (공통 지침 "테스트 함정" 점검 결과).

// Querier 가 반환하는 close 함수를 실제로 부르지 않으면 블록 파일 핸들이
// 새는데, 그 반대 방향(제대로 부르면 확실히 닫힌다)을 증명해 배선이 진짜
// 연결돼 있는지 검증한다. head 시리즈만 있으면 청크가 이미 메모리에 있어
// close 의 영향을 관찰할 수 없으므로, 먼저 Compact 로 블록을 만들어야 한다.
// 만약 DB.Querier 가 반환한 closeAll 이 실제로 Block.Close 를 부르지 않는
// 회귀가 생기면, close 뒤 이터레이션도 계속 성공해 이 테스트가 실패한다.
func TestDB_Querier_close후_블록_청크를_읽으면_에러가_난다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Compact(int64(10) * 15000); err != nil {
		t.Fatal(err)
	}

	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	// Compact 가 raw 블록과 함께 롤업 시리즈(sum/count/min/max)도 만들어
	// 같은 node="e101" 라벨을 공유한다 — raw 매처(RollupLabel="")로 좁혀야
	// 시리즈가 정확히 1개만 나온다 (브리프의 TestDB_Compact가_블록과_롤업을_만든다 와 동일 패턴).
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	raw, _ := NewMatcher(MatchEqual, RollupLabel, "")
	got := q.Select(m, raw)
	if len(got) != 1 {
		t.Fatalf("시리즈: got %d, want 1", len(got))
	}
	// 이터레이터는 여기서 만들지만 Next() 는 아직 부르지 않는다 — 청크 디스크
	// 읽기는 지연되므로, close 뒤에 처음 실제 파일 접근이 일어난다.
	it := got[0].Iterator()

	if err := closeQ(); err != nil {
		t.Fatalf("closeQ: %v", err)
	}

	for it.Next() {
	}
	if it.Err() == nil {
		t.Fatal("closeQ 이후에도 청크 읽기가 성공했다 — 파일 핸들이 닫히지 않은 것으로 보인다")
	}
}

// Compact 를 head 가 빈 상태(직전 Compact 직후)에서 다시 불러도 에러가
// 나거나 빈 블록이 추가로 생기면 안 된다. now 를 보존기간 안쪽으로 둬서
// 방금 만든 블록이 삭제되지 않게 한다.
func TestDB_Compact를_연속으로_두번_불러도_안전하다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 50; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	now := int64(50) * 15000
	if err := db.Compact(now); err != nil {
		t.Fatalf("첫 Compact: %v", err)
	}
	entries1, err := os.ReadDir(blocksDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	n1 := len(entries1)
	if n1 == 0 {
		t.Fatal("첫 Compact 가 블록을 만들지 않았다")
	}

	// head 가 빈 상태에서 다시 Compact — 에러가 없어야 하고, 빈 블록이
	// 추가로 생기면 안 된다.
	if err := db.Compact(now); err != nil {
		t.Fatalf("연속 Compact: %v", err)
	}
	entries2, err := os.ReadDir(blocksDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != n1 {
		t.Fatalf("빈 head 에 대한 Compact 가 블록 개수를 바꿨다: %d -> %d", n1, len(entries2))
	}

	// 질의 결과도 여전히 온전해야 한다.
	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()
	// 50샘플 × 15초 = 12.5분 → 5분 롤업 버킷에 걸쳐 sum/count/min/max 시리즈도
	// 함께 생기므로, raw 매처로 좁혀야 한다 (위 close 테스트와 동일 이유).
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	raw, _ := NewMatcher(MatchEqual, RollupLabel, "")
	got := q.Select(m, raw)
	if len(got) != 1 {
		t.Fatalf("시리즈: got %d, want 1", len(got))
	}
	if s := collect(t, got[0]); len(s) != 50 {
		t.Fatalf("샘플: got %d, want 50", len(s))
	}
}

// Open → Close → Open 왕복 후 같은 라벨셋으로 Append 하면, 복구된 head 의
// ref 를 재사용하면서도 knownRefs 가 그 ref 를 이미 안다고 기억해야 한다.
// knownRefs 복원이 빠지면 시리즈 레코드가 재오픈마다 중복으로 기록된다 —
// WAL 을 직접 재생해 series 레코드 개수로 그 사실을 잡아낸다.
func TestDB_재오픈시_knownRefs가_복원돼_시리즈_레코드가_중복되지_않는다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 5; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatalf("재오픈: %v", err)
	}
	// 같은 라벨셋 — 복구된 head 가 기존 ref 를 재사용해야 한다.
	if err := db2.Append(ls, int64(5)*15000, float64(5)); err != nil {
		t.Fatal(err)
	}
	if err := db2.Close(); err != nil {
		t.Fatal(err)
	}

	seriesRecords := 0
	samples := 0
	if err := ReplayWAL(walDir(dir),
		func(uint64, Labels) error { seriesRecords++; return nil },
		func(ss []RefSample) error { samples += len(ss); return nil }); err != nil {
		t.Fatal(err)
	}
	if seriesRecords != 1 {
		t.Fatalf("series 레코드: got %d, want 1 — knownRefs 복원 실패로 재오픈마다 중복 기록됐다", seriesRecords)
	}
	if samples != 6 {
		t.Fatalf("samples: got %d, want 6", samples)
	}
}

// DB.Querier 는 meta.json 의 시간 범위만으로 블록을 열지 말지 결정한다
// (OpenBlock 을 부르기 전에 걸러낸다). 이 필터가 깨지면 두 방향 중 하나가
// 틀린다: (a) 범위 밖 블록도 열려고 시도 — 손상된 블록이면 무관한 질의까지
// 에러가 난다. (b) 범위 안 블록을 건너뜀 — 있어야 할 데이터가 사라진다.
// index 파일을 손상시켜 "열림 여부"를 에러 유무로 관찰 가능하게 만든다.
func TestDB_Querier가_시간범위로_블록_오픈_여부를_결정한다(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}

	ls := NewLabels(Label{"node", "e101"})
	for i := 0; i < 20; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	blockEnd := int64(20) * 15000
	if err := db.Compact(blockEnd); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(blocksDir(dir))
	if err != nil || len(entries) == 0 {
		t.Fatalf("블록이 생성되지 않았다: %v", err)
	}
	blockPath := filepath.Join(blocksDir(dir), entries[0].Name())
	// index 를 매직바이트가 안 맞는 값으로 손상시켜 OpenBlock 이 반드시
	// 실패하게 만든다 (block.go ErrInvalidIndex).
	if err := os.WriteFile(filepath.Join(blockPath, "index"), []byte("손상된인덱스"), 0o644); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	// (a) 블록 시간 범위보다 한참 뒤만 조회 — 시간 필터가 먼저 걸러내
	// OpenBlock 을 아예 부르지 않아야 하므로, 손상돼 있어도 에러가 없어야 한다.
	farMin := blockEnd + 1_000_000
	_, closeFar, err := db2.Querier(farMin, farMin+1000)
	if err != nil {
		t.Fatalf("범위 밖 블록인데 손상 때문에 에러가 났다 — 시간 필터가 먼저 걸러내지 못했다: %v", err)
	}
	closeFar()

	// (b) 블록과 겹치는 범위를 조회하면 손상된 블록을 실제로 열려다
	// 에러가 나야 한다 (그래야 (a)가 "필터링돼서"이지 "corruption 이
	// 무해해서"가 아님을 확인할 수 있다).
	if _, _, err := db2.Querier(0, blockEnd); err == nil {
		t.Fatal("겹치는 범위인데 손상된 블록에서 에러가 나지 않았다 — 손상 방법이 무효했거나 필터가 범위 안 블록도 걸렀다")
	}
}

func TestDB_고아_tmp_블록은_질의에서_무시된다(t *testing.T) {
	// 진짜 중복 시나리오: WriteBlock 이 <dir>.tmp 에 다 쓰고 rename 하기 전에
	// 크래시하면, 완결된 .tmp 고아가 남고 WAL 은 아직 Truncate 되지 않았다.
	// 재오픈하면 head 가 그 WAL 을 재생해 같은 데이터를 갖는다 — 그 head 청크와
	// .tmp 블록이 같은 시간구간이라, .tmp 를 안 거르면 샘플이 두 번 나온다.
	dir := t.TempDir()
	db, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	for i := 0; i < 10; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}

	// 정식 블록 하나를 만든 뒤, 그 블록을 .tmp 로 복사해 고아를 모사한다.
	// (별도 DB 로 만들어 원본 db 의 head/WAL 은 그대로 살려 둔다 — 이것이
	//  "블록은 굳혀졌는데 WAL Truncate 전 크래시"를 재현한다.)
	stage := t.TempDir()
	sdb, err := Open(DefaultOptions(stage))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := sdb.Append(ls, int64(i)*15000, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sdb.Compact(int64(10) * 15000); err != nil {
		t.Fatal(err)
	}
	sdb.Close()

	sents, err := os.ReadDir(blocksDir(stage))
	if err != nil {
		t.Fatal(err)
	}
	var realBlock string
	for _, e := range sents {
		if e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			realBlock = e.Name()
			break
		}
	}
	if realBlock == "" {
		t.Fatal("정식 블록을 찾지 못했다")
	}
	// 원본 db 의 blocks 에 .tmp 고아로 심는다. db 의 head 에는 같은 10샘플이
	// 여전히 살아 있으므로(Compact 안 함), .tmp 를 안 거르면 중복된다.
	orphan := filepath.Join(blocksDir(dir), realBlock+".tmp")
	if err := copyDir(filepath.Join(blocksDir(stage), realBlock), orphan); err != nil {
		t.Fatal(err)
	}

	// 질의 결과가 두 배가 아니라 원래대로여야 한다.
	q, closeQ, err := db.Querier(0, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	defer closeQ()
	m, _ := NewMatcher(MatchEqual, "node", "e101")
	got := q.Select(m)
	if len(got) != 1 {
		t.Fatalf("시리즈: got %d, want 1 (고아 .tmp 가 중복 시리즈를 만들면 안 된다)", len(got))
	}
	n := 0
	it := got[0].Iterator()
	for it.Next() {
		n++
	}
	if n != 10 {
		t.Fatalf("샘플: got %d, want 10 (고아 .tmp 가 무시되지 않으면 head 재생분과 겹쳐 20 이 된다)", n)
	}
}

// copyDir 는 디렉터리를 얕게 복사한다. 블록은 평면 구조(파일만 담고
// 서브디렉터리가 없다)라 재귀가 필요 없다.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
