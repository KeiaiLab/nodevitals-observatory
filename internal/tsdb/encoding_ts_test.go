package tsdb

import "testing"

func TestTsEncoding_왕복(t *testing.T) {
	cases := map[string][]int64{
		"완전_균일_15초":  {1000, 16000, 31000, 46000, 61000},
		"살짝_지터":      {1000, 16000, 31050, 45900, 61010},
		"큰_점프":       {1000, 16000, 3600000, 3615000},
		"역행하지_않는_중복": {1000, 1000, 1000},
		"단일_샘플":      {1721800000000},
		"음수_시작":      {-5000, -4000, -3000},
		"경계_14비트":    {0, 1000, 1000 + 8192, 1000 + 8192 - 8191},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			var b bstream
			var enc tsEncoder
			for _, ts := range want {
				enc.append(&b, ts)
			}

			r := newBReader(b.bytes())
			var dec tsDecoder
			for i, w := range want {
				got, err := dec.next(r)
				if err != nil {
					t.Fatalf("샘플 %d 디코드 실패: %v", i, err)
				}
				if got != w {
					t.Fatalf("샘플 %d: got %d, want %d", i, got, w)
				}
			}
		})
	}
}

func TestTsEncoding_균일_간격은_샘플당_1비트(t *testing.T) {
	var b bstream
	var enc tsEncoder
	// 첫 2개(각 64비트) 이후 1000개는 dod==0 이라 1비트씩이어야 한다.
	for i := 0; i < 1002; i++ {
		enc.append(&b, int64(i)*15000)
	}
	// 16바이트(첫 2샘플) + 1000비트(=125바이트) = 141바이트 근방
	if got := len(b.bytes()); got > 145 {
		t.Fatalf("균일 간격 압축이 기대보다 나쁘다: %d bytes (want <= 145)", got)
	}
}
