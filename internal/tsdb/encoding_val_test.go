package tsdb

import (
	"math"
	"testing"
)

func TestValEncoding_왕복(t *testing.T) {
	cases := map[string][]float64{
		"상수":       {42, 42, 42, 42},
		"천천히_증가":   {0.5, 0.51, 0.52, 0.53, 0.54},
		"카운터_단조증가": {1e6, 1e6 + 3, 1e6 + 9, 1e6 + 14},
		"큰_변동":     {1, 1e300, -1e-300, 0},
		"음수":       {-1.5, -2.5, -3.5},
		"영":        {0, 0, 0},
		"단일":       {3.14159},
		"특수값":      {math.Inf(1), math.Inf(-1), 0, math.MaxFloat64, math.SmallestNonzeroFloat64},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			var b bstream
			var enc valEncoder
			for _, v := range want {
				enc.append(&b, v)
			}

			r := newBReader(b.bytes())
			var dec valDecoder
			for i, w := range want {
				got, err := dec.next(r)
				if err != nil {
					t.Fatalf("샘플 %d 디코드 실패: %v", i, err)
				}
				if got != w {
					t.Fatalf("샘플 %d: got %v, want %v", i, got, w)
				}
			}
		})
	}
}

func TestValEncoding_NaN도_비트단위로_보존된다(t *testing.T) {
	nan := math.NaN()
	var b bstream
	var enc valEncoder
	enc.append(&b, 1.0)
	enc.append(&b, nan)

	r := newBReader(b.bytes())
	var dec valDecoder
	if _, err := dec.next(r); err != nil {
		t.Fatalf("첫 샘플: %v", err)
	}
	got, err := dec.next(r)
	if err != nil {
		t.Fatalf("두 번째 샘플: %v", err)
	}
	if !math.IsNaN(got) {
		t.Fatalf("NaN 이 보존되지 않았다: %v", got)
	}
}

func TestValEncoding_상수값은_샘플당_1비트(t *testing.T) {
	var b bstream
	var enc valEncoder
	for i := 0; i < 1001; i++ {
		enc.append(&b, 42.0)
	}
	// 8바이트(첫 샘플) + 1000비트(=125바이트) = 133바이트 근방
	if got := len(b.bytes()); got > 137 {
		t.Fatalf("상수값 압축이 기대보다 나쁘다: %d bytes (want <= 137)", got)
	}
}
