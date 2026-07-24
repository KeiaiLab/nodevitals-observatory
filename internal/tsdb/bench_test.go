package tsdb

import (
	"fmt"
	"testing"
)

// M1 완료 기준 ③: 압축 바이트/포인트 측정.
//
// float 의 "천천히 변한다"가 비트 수준에선 "많이 변한다"일 수 있다 — XOR
// 압축은 직전 값과 지금 값의 비트 패턴이 겹쳐야 이득을 보는데, 0.1/0.01
// 단위로 계속 요동치는 값은 유효 XOR 비트가 가수부 전체에 퍼져 거의
// 압축되지 않는다(실측 최악 ~6.7 bytes/point, encoding_val.go 는 Gorilla
// 알고리즘을 정확히 구현하고 있으므로 이는 구현 결함이 아니라 그 값
// 자체의 비트 특성이다). 실제 nodevitals 메트릭 대부분(온도·사용률·
// 메모리 바이트·카운터)은 정수 계단형이라 설계 §8.2 의 1.5 가정 부근이거나
// 더 좋다 — 그래서 "정수 계단형(strict, 임계 3.0)"과 "요동 float
// 최악(worst, 임계 8.0)"을 분리해서 검증한다.
func TestCompressionRatio_바이트당_포인트를_보고한다(t *testing.T) {
	const samples = 10000

	strict := map[string]func(i int) float64{
		"상수_게이지":   func(i int) float64 { return 42 },
		"온도_정수계단":  func(i int) float64 { return 42 + float64((i/7)%26) },
		"사용률_정수":   func(i int) float64 { return float64((i * 13) % 101) },
		"메모리_느린계단": func(i int) float64 { return 8e9 + float64((i/50)%1000)*1e6 },
		"단조_카운터":   func(i int) float64 { return float64(i) * 1000 },
	}
	worst := map[string]func(i int) float64{
		"요동_float_최악": func(i int) float64 { return 2 + float64(i%300)/100 },
	}

	measure := func(gen func(i int) float64) (float64, int) {
		var total, chunks int
		c := NewChunk()
		chunks = 1
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
		return float64(total) / float64(samples), chunks
	}

	for name, gen := range strict {
		bpp, chunks := measure(gen)
		t.Log(fmt.Sprintf("%s: %.2f bytes/point (청크 %d개)", name, bpp, chunks))
		if bpp > 3.0 {
			t.Fatalf("%s: %.2f bytes/point — 정수 계단형인데 압축이 나쁘다", name, bpp)
		}
	}
	for name, gen := range worst {
		bpp, chunks := measure(gen)
		t.Log(fmt.Sprintf("%s: %.2f bytes/point (청크 %d개)", name, bpp, chunks))
		if bpp > 8.0 {
			t.Fatalf("%s: %.2f bytes/point — 최악 케이스도 이 한도는 넘지 않아야 한다", name, bpp)
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
