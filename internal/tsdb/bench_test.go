package tsdb

import (
	"fmt"
	"testing"
)

// M1 완료 기준 ③: 압축 바이트/포인트 측정. 실측 e101 노드의 시리즈 특성
// (느리게 변하는 게이지 + 단조 증가 카운터)을 흉내 낸다.
func TestCompressionRatio_바이트당_포인트를_보고한다(t *testing.T) {
	const samples = 10000

	cases := map[string]func(i int) float64{
		"상수_게이지": func(i int) float64 { return 42 },
		"느린_게이지": func(i int) float64 { return 40 + float64(i%20)*0.1 },
		"단조_카운터": func(i int) float64 { return float64(i) * 1000 },
		"잡음_게이지": func(i int) float64 { return 50 + float64((i*7919)%1000)/1000 },
	}

	for name, gen := range cases {
		var total int
		c := NewChunk()
		chunks := 1
		for i := 0; i < samples; i++ {
			if c.Full() {
				total += len(c.Bytes())
				c = NewChunk()
				chunks++
			}
			if err := c.Append(int64(i)*15000, gen(i)); err != nil {
				t.Fatal(err)
			}
		}
		total += len(c.Bytes())

		bpp := float64(total) / float64(samples)
		t.Log(fmt.Sprintf("%s: %d bytes / %d samples = %.2f bytes/point (청크 %d개)",
			name, total, samples, bpp, chunks))

		// 설계 문서 §8.2 는 ~1.5 bytes/point 를 가정한다. 잡음 게이지는
		// 그보다 나쁠 수 있으나 4를 넘으면 용량 산정이 무너진다.
		if bpp > 4.0 {
			t.Fatalf("%s: %.2f bytes/point — 용량 산정 가정(1.5)에서 너무 벗어난다", name, bpp)
		}
	}
}

func BenchmarkDBAppend(b *testing.B) {
	db, err := Open(DefaultOptions(b.TempDir()))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ls := NewLabels(Label{MetricName, "node_load1"}, Label{"node", "e101"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Append(ls, int64(i)*15000, float64(i)); err != nil {
			b.Fatal(err)
		}
	}
}
