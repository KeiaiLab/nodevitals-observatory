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
		// dod 가 각 티어의 정확한 경계값이 되도록 델타를 역산한 케이스.
		// t0=0, t1=t0+base, t2=t1+(base+dod) 이면 두 번째 dod 가 그 값이 된다.
		// (bitRange 범위는 비대칭이다 — 상한 +2^(n-1), 하한 -(2^(n-1)-1))
		"dod_14비트_상한_8192":    {0, 15000, 15000 + 15000 + 8192},
		"dod_14비트_하한_-8191":   {0, 15000, 15000 + 15000 - 8191},
		"dod_17비트_상한_65536":   {0, 100000, 100000 + 100000 + 65536},
		"dod_17비트_하한_-65535":  {0, 100000, 100000 + 100000 - 65535},
		"dod_20비트_상한_524288":  {0, 1000000, 1000000 + 1000000 + 524288},
		"dod_20비트_하한_-524287": {0, 1000000, 1000000 + 1000000 - 524287},
		"dod_20비트_초과_64비트폴백":  {0, 1000000, 1000000 + 1000000 + 524289},
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

func TestTsDecoder_잘린_스트림에서_에러를_낸다(t *testing.T) {
	var b bstream
	var enc tsEncoder
	for _, ts := range []int64{1000, 16000, 31000} {
		enc.append(&b, ts)
	}

	// 첫 샘플의 64비트조차 못 채우도록 자른다.
	truncated := b.bytes()[:4]
	r := newBReader(truncated)
	var dec tsDecoder
	if _, err := dec.next(r); err == nil {
		t.Fatal("잘린 스트림에서 에러가 나야 한다")
	}
}
