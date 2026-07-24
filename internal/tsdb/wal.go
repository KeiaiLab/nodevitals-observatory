package tsdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// WAL 레코드 포맷:
//
//	[1B type][4B payloadLen][payload][4B crc32]
//
// crc 는 type+len+payload 전체에 대해 계산한다. 크래시로 마지막 레코드가
// 잘리는 것은 **정상 상황**이므로, 재생기는 손상 지점에서 조용히 멈추고
// 그때까지의 결과를 성공으로 돌려준다 (Task 10 이 이 동작을 고정한다).
const (
	recSeries  byte = 1
	recSamples byte = 2
)

const defaultSegmentSize int64 = 32 << 20

var crcTable = crc32.MakeTable(crc32.Castagnoli)

type RefSample struct {
	Ref uint64
	T   int64
	V   float64
}

type WAL struct {
	mtx sync.Mutex

	dir     string
	f       *os.File
	segIdx  int
	size    int64
	segSize int64
}

func segmentName(dir string, idx int) string {
	return filepath.Join(dir, fmt.Sprintf("%08d", idx))
}

// listSegments 는 WAL 세그먼트를 번호 오름차순으로 낸다.
func listSegments(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if len(e.Name()) == 8 {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, n)
	}
	return out, nil
}

func OpenWAL(dir string, segSize int64) (*WAL, error) {
	if segSize <= 0 {
		segSize = defaultSegmentSize
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	segs, err := listSegments(dir)
	if err != nil {
		return nil, err
	}

	w := &WAL{dir: dir, segSize: segSize, segIdx: 1}
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		var idx int
		if _, err := fmt.Sscanf(filepath.Base(last), "%08d", &idx); err == nil {
			w.segIdx = idx
		}
	}
	if err := w.openSegment(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *WAL) openSegment() error {
	f, err := os.OpenFile(segmentName(w.dir, w.segIdx), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f = f
	w.size = st.Size()
	return nil
}

func (w *WAL) writeRecord(typ byte, payload []byte) error {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	if w.size >= w.segSize {
		if err := w.f.Close(); err != nil {
			return err
		}
		w.segIdx++
		if err := w.openSegment(); err != nil {
			return err
		}
	}

	buf := make([]byte, 0, 5+len(payload)+4)
	buf = append(buf, typ)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(payload)))
	buf = append(buf, payload...)
	buf = binary.BigEndian.AppendUint32(buf, crc32.Checksum(buf, crcTable))

	n, err := w.f.Write(buf)
	w.size += int64(n)
	return err
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func (w *WAL) LogSeries(ref uint64, ls Labels) error {
	buf := make([]byte, 0, 64)
	buf = binary.BigEndian.AppendUint64(buf, ref)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(ls)))
	for _, l := range ls {
		buf = appendString(buf, l.Name)
		buf = appendString(buf, l.Value)
	}
	return w.writeRecord(recSeries, buf)
}

func (w *WAL) LogSamples(ss []RefSample) error {
	if len(ss) == 0 {
		return nil
	}
	buf := make([]byte, 0, 2+len(ss)*24)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(ss)))
	for _, s := range ss {
		buf = binary.BigEndian.AppendUint64(buf, s.Ref)
		buf = binary.BigEndian.AppendUint64(buf, uint64(s.T))
		buf = binary.BigEndian.AppendUint64(buf, math.Float64bits(s.V))
	}
	return w.writeRecord(recSamples, buf)
}

func (w *WAL) Sync() error {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	return w.f.Sync()
}

func (w *WAL) Close() error {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Truncate 는 블록 flush 가 끝나 더 이상 필요 없어진 WAL 을 통째로 버리고
// 다음 세그먼트 번호로 새로 시작한다.
func (w *WAL) Truncate() error {
	w.mtx.Lock()
	defer w.mtx.Unlock()

	if w.f != nil {
		if err := w.f.Close(); err != nil {
			return err
		}
		w.f = nil
	}
	segs, err := listSegments(w.dir)
	if err != nil {
		return err
	}
	for _, s := range segs {
		if err := os.Remove(s); err != nil {
			return err
		}
	}
	w.segIdx++
	return w.openSegment()
}

// parseSeries / parseSamples 는 payload 를 되읽는다. 길이가 모자라면
// 손상으로 보고 에러를 낸다 — 재생기가 그 지점에서 멈춘다.
func parseSeries(p []byte) (uint64, Labels, error) {
	if len(p) < 10 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	ref := binary.BigEndian.Uint64(p[:8])
	n := int(binary.BigEndian.Uint16(p[8:10]))
	p = p[10:]

	ls := make(Labels, 0, n)
	readStr := func() (string, error) {
		if len(p) < 2 {
			return "", io.ErrUnexpectedEOF
		}
		l := int(binary.BigEndian.Uint16(p[:2]))
		p = p[2:]
		if len(p) < l {
			return "", io.ErrUnexpectedEOF
		}
		s := string(p[:l])
		p = p[l:]
		return s, nil
	}
	for i := 0; i < n; i++ {
		name, err := readStr()
		if err != nil {
			return 0, nil, err
		}
		val, err := readStr()
		if err != nil {
			return 0, nil, err
		}
		ls = append(ls, Label{Name: name, Value: val})
	}
	return ref, ls, nil
}

func parseSamples(p []byte) ([]RefSample, error) {
	if len(p) < 2 {
		return nil, io.ErrUnexpectedEOF
	}
	n := int(binary.BigEndian.Uint16(p[:2]))
	p = p[2:]
	if len(p) < n*24 {
		return nil, io.ErrUnexpectedEOF
	}
	out := make([]RefSample, n)
	for i := 0; i < n; i++ {
		off := i * 24
		out[i] = RefSample{
			Ref: binary.BigEndian.Uint64(p[off : off+8]),
			T:   int64(binary.BigEndian.Uint64(p[off+8 : off+16])),
			V:   math.Float64frombits(binary.BigEndian.Uint64(p[off+16 : off+24])),
		}
	}
	return out, nil
}

// ReplayWAL 은 세그먼트를 번호순으로 읽어 콜백을 호출한다. 손상된 레코드를
// 만나면 **그 지점에서 멈추고 nil 을 반환한다** — 크래시로 마지막 쓰기가
// 잘린 상황이 정상이기 때문이다. 콜백이 낸 에러는 그대로 전파한다.
func ReplayWAL(dir string, onSeries func(uint64, Labels) error, onSamples func([]RefSample) error) error {
	segs, err := listSegments(dir)
	if err != nil {
		return err
	}
	for _, seg := range segs {
		data, err := os.ReadFile(seg)
		if err != nil {
			return err
		}
		for off := 0; ; {
			if off+5 > len(data) {
				break // 헤더도 안 남음 — 정상 종료 또는 절단
			}
			typ := data[off]
			plen := int(binary.BigEndian.Uint32(data[off+1 : off+5]))
			end := off + 5 + plen + 4
			if plen < 0 || end > len(data) {
				break // 페이로드/CRC 가 잘림
			}
			want := binary.BigEndian.Uint32(data[end-4 : end])
			if crc32.Checksum(data[off:end-4], crcTable) != want {
				break // CRC 불일치 — 이 지점부터 신뢰 불가
			}
			payload := data[off+5 : off+5+plen]

			switch typ {
			case recSeries:
				ref, ls, err := parseSeries(payload)
				if err != nil {
					return nil // 손상 — 여기서 멈춘다
				}
				if err := onSeries(ref, ls); err != nil {
					return err
				}
			case recSamples:
				ss, err := parseSamples(payload)
				if err != nil {
					return nil
				}
				if err := onSamples(ss); err != nil {
					return err
				}
			default:
				return nil // 알 수 없는 타입 — 손상으로 본다
			}
			off = end
		}
	}
	return nil
}
