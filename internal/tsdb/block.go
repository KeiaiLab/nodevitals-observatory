package tsdb

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/KeiaiLab/nodevitals-observatory/internal/labels"
	"math"
	"os"
	"path/filepath"
)

// ResolutionRaw 는 원본 해상도 블록의 표식이다. 롤업 블록은 "5m" 등을 쓴다.
const ResolutionRaw = "raw"

var indexMagic = [4]byte{'N', 'V', 'I', 'X'}

var ErrInvalidIndex = errors.New("tsdb: 블록 인덱스가 손상됨")

// BlockMeta 는 블록 디렉터리의 meta.json 이다. DB 는 블록을 열지 않고
// 이 파일만으로 시간 범위를 알 수 있어야 한다 — 질의에 걸치는 블록만
// 여는 전략(querier)이 여기에 기댄다.
type BlockMeta struct {
	Version    string `json:"version"`
	MinTime    int64  `json:"minTime"`
	MaxTime    int64  `json:"maxTime"`
	Series     int    `json:"series"`
	Samples    int    `json:"samples"`
	Resolution string `json:"resolution"`
}

type chunkRef struct {
	Offset int64
	Length uint32
	MinT   int64
	MaxT   int64
}

type blockSeries struct {
	Ref    uint64
	Lset   labels.Labels
	Chunks []chunkRef
}

// WriteBlock 은 시리즈 목록을 불변 블록 디렉터리로 굳힌다. 반환값은 만들어진
// 디렉터리 경로이며, 시리즈가 없으면 빈 문자열을 준다(빈 블록을 만들지 않음).
//
// 디렉터리 이름은 `<minT>-<maxT>-<resolution>` 이라 이름만으로 정렬·식별된다.
func WriteBlock(baseDir string, series []*memSeries, resolution string) (string, error) {
	if len(series) == 0 {
		return "", nil
	}

	meta := BlockMeta{
		Version:    Version,
		MinTime:    math.MaxInt64,
		MaxTime:    math.MinInt64,
		Resolution: resolution,
	}

	var chunksBuf []byte
	entries := make([]blockSeries, 0, len(series))

	for _, s := range series {
		if len(s.chunks) == 0 {
			continue
		}
		bs := blockSeries{Ref: s.ref, Lset: s.lset}
		for _, c := range s.chunks {
			if c.NumSamples() == 0 {
				continue
			}
			raw := c.Bytes()
			bs.Chunks = append(bs.Chunks, chunkRef{
				Offset: int64(len(chunksBuf)),
				Length: uint32(len(raw)),
				MinT:   c.MinTime(),
				MaxT:   c.MaxTime(),
			})
			chunksBuf = append(chunksBuf, raw...)
			meta.Samples += c.NumSamples()
		}
		if len(bs.Chunks) == 0 {
			continue
		}
		if s.minT < meta.MinTime {
			meta.MinTime = s.minT
		}
		if s.maxT > meta.MaxTime {
			meta.MaxTime = s.maxT
		}
		entries = append(entries, bs)
	}
	if len(entries) == 0 {
		return "", nil
	}
	meta.Series = len(entries)

	dir := filepath.Join(baseDir, fmt.Sprintf("%013d-%013d-%s", meta.MinTime, meta.MaxTime, resolution))

	// index 직렬화
	idx := make([]byte, 0, 4096)
	idx = append(idx, indexMagic[:]...)
	idx = append(idx, 1) // 인덱스 포맷 버전
	idx = binary.BigEndian.AppendUint32(idx, uint32(len(entries)))
	for _, e := range entries {
		idx = binary.BigEndian.AppendUint64(idx, e.Ref)
		idx = binary.BigEndian.AppendUint16(idx, uint16(len(e.Lset)))
		for _, l := range e.Lset {
			idx = appendString(idx, l.Name)
			idx = appendString(idx, l.Value)
		}
		idx = binary.BigEndian.AppendUint16(idx, uint16(len(e.Chunks)))
		for _, c := range e.Chunks {
			idx = binary.BigEndian.AppendUint64(idx, uint64(c.Offset))
			idx = binary.BigEndian.AppendUint32(idx, c.Length)
			idx = binary.BigEndian.AppendUint64(idx, uint64(c.MinT))
			idx = binary.BigEndian.AppendUint64(idx, uint64(c.MaxT))
		}
	}

	// 완성된 블록을 원자적으로 노출한다. 임시 디렉터리에 세 파일을 다 쓴 뒤
	// rename 해야 "meta.json 이 있으면 완성된 블록" 이라는 계약이 재쓰기·중단
	// 상황에서도 지켜진다 — 같은 자리에 덮어쓰면 index 쓰기가 실패했을 때
	// 새 chunks 와 낡은 meta.json 이 섞인 블록이 남는다.
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return "", err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	// 실패하면 임시 디렉터리를 남기지 않는다.
	defer os.RemoveAll(tmp)

	if err := os.WriteFile(filepath.Join(tmp, "chunks"), chunksBuf, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, "index"), idx, 0o644); err != nil {
		return "", err
	}
	mj, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), mj, 0o644); err != nil {
		return "", err
	}

	// rename 은 대상이 비어 있어야 하므로 기존 블록을 먼저 치운다.
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func ReadBlockMeta(dir string) (BlockMeta, error) {
	var m BlockMeta
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

// Block 은 열린 블록이다. 인덱스는 메모리에 올리고 청크는 필요할 때만
// ReadAt 으로 읽는다 — 블록이 여럿 열려도 메모리가 청크 크기에 비례해
// 늘지 않게 하려는 설계다.
type Block struct {
	dir      string
	meta     BlockMeta
	series   []blockSeries
	postings *memPostings
	byRef    map[uint64]*blockSeries
	chunksF  *os.File
}

func OpenBlock(dir string) (*Block, error) {
	meta, err := ReadBlockMeta(dir)
	if err != nil {
		return nil, err
	}
	idx, err := os.ReadFile(filepath.Join(dir, "index"))
	if err != nil {
		return nil, err
	}
	if len(idx) < 9 || [4]byte{idx[0], idx[1], idx[2], idx[3]} != indexMagic {
		return nil, ErrInvalidIndex
	}
	if idx[4] != 1 {
		return nil, fmt.Errorf("%w: 알 수 없는 인덱스 포맷 버전 %d", ErrInvalidIndex, idx[4])
	}

	p := idx[5:]
	readU16 := func() (int, bool) {
		if len(p) < 2 {
			return 0, false
		}
		v := int(binary.BigEndian.Uint16(p[:2]))
		p = p[2:]
		return v, true
	}
	readStr := func() (string, bool) {
		n, ok := readU16()
		if !ok || len(p) < n {
			return "", false
		}
		s := string(p[:n])
		p = p[n:]
		return s, true
	}

	if len(p) < 4 {
		return nil, ErrInvalidIndex
	}
	numSeries := int(binary.BigEndian.Uint32(p[:4]))
	p = p[4:]

	b := &Block{
		dir:      dir,
		meta:     meta,
		postings: newMemPostings(),
		byRef:    map[uint64]*blockSeries{},
	}
	b.series = make([]blockSeries, 0, numSeries)

	for i := 0; i < numSeries; i++ {
		if len(p) < 8 {
			return nil, ErrInvalidIndex
		}
		var e blockSeries
		e.Ref = binary.BigEndian.Uint64(p[:8])
		p = p[8:]

		nl, ok := readU16()
		if !ok {
			return nil, ErrInvalidIndex
		}
		ls := make(labels.Labels, 0, nl)
		for j := 0; j < nl; j++ {
			name, ok1 := readStr()
			val, ok2 := readStr()
			if !ok1 || !ok2 {
				return nil, ErrInvalidIndex
			}
			ls = append(ls, labels.Label{Name: name, Value: val})
		}
		e.Lset = ls

		nc, ok := readU16()
		if !ok {
			return nil, ErrInvalidIndex
		}
		for j := 0; j < nc; j++ {
			if len(p) < 28 {
				return nil, ErrInvalidIndex
			}
			e.Chunks = append(e.Chunks, chunkRef{
				Offset: int64(binary.BigEndian.Uint64(p[0:8])),
				Length: binary.BigEndian.Uint32(p[8:12]),
				MinT:   int64(binary.BigEndian.Uint64(p[12:20])),
				MaxT:   int64(binary.BigEndian.Uint64(p[20:28])),
			})
			p = p[28:]
		}
		b.series = append(b.series, e)
	}

	for i := range b.series {
		b.postings.Add(b.series[i].Ref, b.series[i].Lset)
		b.byRef[b.series[i].Ref] = &b.series[i]
	}

	f, err := os.Open(filepath.Join(dir, "chunks"))
	if err != nil {
		return nil, err
	}
	b.chunksF = f
	return b, nil
}

func (b *Block) Meta() BlockMeta { return b.meta }
func (b *Block) Dir() string     { return b.dir }

func (b *Block) Close() error {
	if b.chunksF == nil {
		return nil
	}
	err := b.chunksF.Close()
	b.chunksF = nil
	return err
}

// Chunk 는 청크 참조가 가리키는 바이트를 읽어 청크로 되살린다.
func (b *Block) Chunk(cr chunkRef) (*Chunk, error) {
	buf := make([]byte, cr.Length)
	if _, err := b.chunksF.ReadAt(buf, cr.Offset); err != nil {
		return nil, err
	}
	return ChunkFromBytes(buf)
}

// Select 는 Head.Select 와 **같은 두 함수**(selectRefs·matchesAll)를 써서
// 동일한 질의 의미론을 보장한다.
func (b *Block) Select(ms ...*labels.Matcher) []*blockSeries {
	refs := selectRefs(b.postings, ms)
	out := make([]*blockSeries, 0, len(refs))
	for _, id := range refs {
		s := b.byRef[id]
		if s != nil && matchesAll(s.Lset, ms) {
			out = append(out, s)
		}
	}
	return out
}
