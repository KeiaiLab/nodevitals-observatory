package tsdb

import (
	"os"
	"path/filepath"
	"testing"
)

// buildHead 는 노드 2개 × 메트릭 2개 시리즈에 샘플을 채운 head 를 만든다.
func buildHead(t *testing.T, samples int) *Head {
	t.Helper()
	h := NewHead()
	for _, node := range []string{"e101", "e102"} {
		for _, metric := range []string{"node_load1", "gpu_temp"} {
			ls := NewLabels(Label{MetricName, metric}, Label{"node", node})
			for i := 0; i < samples; i++ {
				if _, err := h.Append(ls, int64(i)*15000, float64(i)+1); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return h
}

func allSeries(h *Head) []*memSeries {
	m, _ := NewMatcher(MatchRegexp, MetricName, ".*")
	return h.Select(m)
}

func TestWriteBlock_메타와_파일을_남긴다(t *testing.T) {
	h := buildHead(t, 50)
	base := t.TempDir()

	dir, err := WriteBlock(base, allSeries(h), ResolutionRaw)
	if err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
	for _, f := range []string{"meta.json", "index", "chunks"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s 가 없다: %v", f, err)
		}
	}

	meta, err := ReadBlockMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Series != 4 {
		t.Fatalf("Series: got %d, want 4", meta.Series)
	}
	if meta.Samples != 200 {
		t.Fatalf("Samples: got %d, want 200", meta.Samples)
	}
	if meta.MinTime != 0 || meta.MaxTime != 49*15000 {
		t.Fatalf("시간 범위: [%d,%d]", meta.MinTime, meta.MaxTime)
	}
	if meta.Resolution != ResolutionRaw {
		t.Fatalf("Resolution: got %q", meta.Resolution)
	}
}

func TestBlock_쓴_샘플을_그대로_읽는다(t *testing.T) {
	h := buildHead(t, 50)
	base := t.TempDir()
	dir, err := WriteBlock(base, allSeries(h), ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}

	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatalf("OpenBlock: %v", err)
	}
	defer b.Close()

	eq, _ := NewMatcher(MatchEqual, MetricName, "node_load1")
	nodeEq, _ := NewMatcher(MatchEqual, "node", "e101")
	got := b.Select(eq, nodeEq)
	if len(got) != 1 {
		t.Fatalf("매처 결과: got %d 시리즈, want 1", len(got))
	}

	n := 0
	for _, cr := range got[0].Chunks {
		c, err := b.Chunk(cr)
		if err != nil {
			t.Fatalf("Chunk: %v", err)
		}
		it := c.Iterator()
		for it.Next() {
			ts, v := it.At()
			if ts != int64(n)*15000 || v != float64(n)+1 {
				t.Fatalf("샘플 %d: got (%d,%v)", n, ts, v)
			}
			n++
		}
		if it.Err() != nil {
			t.Fatalf("이터레이터: %v", it.Err())
		}
	}
	if n != 50 {
		t.Fatalf("샘플 수: got %d, want 50", n)
	}
}

func TestBlock_여러_청크를_가진_시리즈도_왕복한다(t *testing.T) {
	// 청크 상한을 넘겨 시리즈당 청크가 2개 이상 되게 한다.
	h := buildHead(t, maxSamplesPerChunk+30)
	base := t.TempDir()
	dir, _ := WriteBlock(base, allSeries(h), ResolutionRaw)

	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	eq, _ := NewMatcher(MatchEqual, "node", "e102")
	got := b.Select(eq)
	if len(got) != 2 {
		t.Fatalf("node=e102: got %d 시리즈, want 2", len(got))
	}
	for _, s := range got {
		if len(s.Chunks) < 2 {
			t.Fatalf("청크가 2개 이상이어야 한다: %d", len(s.Chunks))
		}
		n := 0
		for _, cr := range s.Chunks {
			c, err := b.Chunk(cr)
			if err != nil {
				t.Fatal(err)
			}
			n += c.NumSamples()
		}
		if n != maxSamplesPerChunk+30 {
			t.Fatalf("샘플 수: got %d, want %d", n, maxSamplesPerChunk+30)
		}
	}
}

func TestWriteBlock_빈_시리즈_목록은_블록을_만들지_않는다(t *testing.T) {
	dir, err := WriteBlock(t.TempDir(), nil, ResolutionRaw)
	if err != nil {
		t.Fatalf("빈 목록은 에러가 아니어야 한다: %v", err)
	}
	if dir != "" {
		t.Fatalf("빈 목록은 빈 경로를 줘야 한다: %q", dir)
	}
}

func TestOpenBlock_손상된_인덱스를_거부한다(t *testing.T) {
	h := buildHead(t, 10)
	base := t.TempDir()
	dir, _ := WriteBlock(base, allSeries(h), ResolutionRaw)

	if err := os.WriteFile(filepath.Join(dir, "index"), []byte("깨짐"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBlock(dir); err == nil {
		t.Fatal("손상된 인덱스는 에러여야 한다")
	}
}

func TestOpenBlock_메타없음_에러(t *testing.T) {
	// meta.json 이 없으면 블록을 열 수 없어야 한다.
	// 이는 쓰다 만 블록(chunks/index만 있음)을 식별하는 방법이다.
	h := buildHead(t, 10)
	base := t.TempDir()
	dir, _ := WriteBlock(base, allSeries(h), ResolutionRaw)

	// meta.json 을 삭제한다
	metaPath := filepath.Join(dir, "meta.json")
	if err := os.Remove(metaPath); err != nil {
		t.Fatal(err)
	}

	// OpenBlock 은 실패해야 한다
	_, err := OpenBlock(dir)
	if err == nil {
		t.Fatal("meta.json 이 없으면 OpenBlock 은 에러여야 한다")
	}
}

func TestBlock_Close후_Chunk_에러(t *testing.T) {
	// Close() 후 Chunk() 을 호출하면 에러가 나야 한다.
	h := buildHead(t, 20)
	base := t.TempDir()
	dir, _ := WriteBlock(base, allSeries(h), ResolutionRaw)

	b, err := OpenBlock(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 먼저 청크를 정상적으로 읽는다
	eq, _ := NewMatcher(MatchEqual, MetricName, "node_load1")
	got := b.Select(eq)
	if len(got) == 0 || len(got[0].Chunks) == 0 {
		t.Fatal("테스트 데이터 부족")
	}

	// Close 한다
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	// 이제 Chunk() 을 호출하면 에러가 나야 한다
	_, err = b.Chunk(got[0].Chunks[0])
	if err == nil {
		t.Fatal("Close 후 Chunk 는 에러여야 한다")
	}
}

func TestWriteBlock_샘플0청크만_있으면_빈경로(t *testing.T) {
	// 모든 청크가 샘플 0이면 WriteBlock 은 빈 경로를 반환해야 한다.
	// 이를 위해 head 를 만든 뒤, 시리즈의 청크를 비운다.
	h := NewHead()
	for _, node := range []string{"e101", "e102"} {
		for _, metric := range []string{"node_load1", "gpu_temp"} {
			ls := NewLabels(Label{MetricName, metric}, Label{"node", node})
			// Append 를 호출하지 않으면 시리즈는 빈 청크만 가진다
			_ = h.getOrCreate(ls)
		}
	}

	series := allSeries(h)
	if len(series) == 0 {
		t.Fatal("테스트 준비: 시리즈가 필요하다")
	}

	base := t.TempDir()
	dir, err := WriteBlock(base, series, ResolutionRaw)
	if err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	// 시리즈가 샘플 0이므로 WriteBlock 은 빈 경로를 반환해야 한다
	if dir != "" {
		t.Fatalf("샘플 0 시리즈만 있으면 빈 경로를 줘야 한다: %q", dir)
	}
}
