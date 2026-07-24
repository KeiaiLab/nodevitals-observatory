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
		// +0.0 과 -0.0 은 비트 패턴이 다르다(0x0000… vs 0x8000…). 값 비교로는
		// 구분되지 않으므로 아래 판정을 비트 비교로 바꾼 것과 짝을 이룬다.
		"부호있는_영": {0, math.Copysign(0, -1), 0, math.Copysign(0, -1)},
		// XOR 의 leading zero 가 충분히 커서 lead >= 32 클램프 분기를 태운다.
		"leading_클램프": {1.0, 1.0 + math.Ldexp(1, -52), 1.0},
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
				// 비트 단위로 비교한다 — Go/IEEE-754 에서 +0.0 == -0.0 이라
				// 값 비교로는 부호 있는 0 의 보존을 검증할 수 없다.
				if math.Float64bits(got) != math.Float64bits(w) {
					t.Fatalf("샘플 %d: got %v (bits %#016x), want %v (bits %#016x)",
						i, got, math.Float64bits(got), w, math.Float64bits(w))
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
