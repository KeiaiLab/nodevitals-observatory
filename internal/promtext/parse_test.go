package promtext

import (
	"bufio"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// TestParse_라벨_이스케이프_3종 은 백슬래시·큰따옴표·개행 이스케이프가 정확히
// 복원되는지 확인한다. 원본은 raw 문자열(백틱)이라 Go 자체의 이스케이프
// 해석을 거치지 않는다 — 실제 바이트가 곧 Prometheus exposition 규격의
// 이스케이프 시퀀스다:
//
//	a="x\\y"  -> 바이트: x \ \ y   -> 디코드: x \ y  (이스케이프된 백슬래시 1개)
//	b="q\"z"  -> 바이트: q \ " z   -> 디코드: q " z  (이스케이프된 큰따옴표)
//	c="l1\nl2"-> 바이트: l1 \ n l2 -> 디코드: l1 <LF> l2 (이스케이프된 개행)
func TestParse_라벨_이스케이프_3종(t *testing.T) {
	input := `m{a="x\\y",b="q\"z",c="l1\nl2"} 1` + "\n"

	series, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: 예기치 않은 에러: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("시리즈 개수: got %d, want 1", len(series))
	}
	s := series[0]

	if got, want := s.Labels["a"], `x\y`; got != want {
		t.Errorf(`라벨 a (\\ 이스케이프): got %q, want %q`, got, want)
	}
	if got, want := s.Labels["b"], `q"z`; got != want {
		t.Errorf(`라벨 b (\" 이스케이프): got %q, want %q`, got, want)
	}
	if got, want := s.Labels["c"], "l1\nl2"; got != want {
		t.Errorf(`라벨 c (\n 이스케이프): got %q, want %q`, got, want)
	}
	if !strings.Contains(s.Labels["c"], "\n") {
		t.Error("라벨 c 에 실제 개행 문자가 없다 — 리터럴 \\n 두 글자로 남아있으면 안 된다")
	}
}

// TestParse_NaN_Inf_와_타임스탬프 는 특수값 3종(NaN/+Inf/-Inf)과 타임스탬프
// 유무 분기(있음/없음/있음)를 모두 실행한다.
func TestParse_NaN_Inf_와_타임스탬프(t *testing.T) {
	input := "m1 NaN 1690000000000\nm2 +Inf\nm3 -Inf 5\n"

	series, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: 예기치 않은 에러: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("시리즈 개수: got %d, want 3", len(series))
	}
	m1, m2, m3 := series[0], series[1], series[2]

	if !math.IsNaN(m1.Value) {
		t.Errorf("m1.Value: got %v, want NaN", m1.Value)
	}
	if m1.TimestampMS != 1690000000000 {
		t.Errorf("m1.TimestampMS: got %d, want 1690000000000", m1.TimestampMS)
	}

	if !math.IsInf(m2.Value, 1) {
		t.Errorf("m2.Value: got %v, want +Inf", m2.Value)
	}
	if m2.TimestampMS != 0 {
		t.Errorf("m2.TimestampMS(타임스탬프 없음): got %d, want 0", m2.TimestampMS)
	}

	if !math.IsInf(m3.Value, -1) {
		t.Errorf("m3.Value: got %v, want -Inf", m3.Value)
	}
	if m3.TimestampMS != 5 {
		t.Errorf("m3.TimestampMS: got %d, want 5", m3.TimestampMS)
	}
}

// TestParse_형식오류는_에러 는 constraints §테스트 함정("에러 경로 실행 의무")에
// 따라, parse.go 가 반환하는 모든 에러 분기를 개별 입력으로 도달시키고
// 각 분기 고유의 메시지 부분 문자열로 "맞는 분기가 실행됐는지"까지 단정한다
// (단순 err!=nil 만 보면 엉뚱한 분기가 에러를 내도 통과하는 동어반복이 된다).
func TestParse_형식오류는_에러(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantSubstr string
	}{
		// 설계 계약이 지정한 2개 필수 픽스처.
		{"라벨_블록_미닫힘", `m{a="1" 2`, "예기치 않은 문자"},
		{"값_없는_줄", `m{}`, "값이 없다"},
		// 아래는 구현이 실제로 만드는 나머지 에러 분기 — 각각 미도달 상태로
		// 남지 않도록 자체 보강.
		{"메트릭_이름_없음", `{a="1"} 2`, "메트릭 이름이 비어 있다"},
		{"라벨_이름_없음", `m{="1"} 2`, "라벨 이름이 비어 있다"},
		{"라벨_등호_없음", `m{a} 2`, `'=' 이 없다`},
		{"라벨값_따옴표로_시작안함", `m{a=1} 2`, "큰따옴표로 시작하지 않는다"},
		{"라벨값_따옴표_안닫힘_EOF", `m{a="unterminated} 2`, "값의 큰따옴표가 닫히지 않았다"},
		{"라벨_블록이_EOF까지_안닫힘", `m{a="1"`, "라벨 블록이 '}'"},
		{"이스케이프_문자로_끝남", `m{a="x\`, "이스케이프 문자로 끝났다"},
		{"알수없는_이스케이프", `m{a="x\tb"} 1`, "알 수 없는 이스케이프"},
		{"값_파싱_실패", `m abc`, "값 파싱 실패"},
		{"타임스탬프_파싱_실패", `m 1 abc`, "타임스탬프 파싱 실패"},
		{"타임스탬프_뒤_잉여문자", `m 1 5 extra`, "처리되지 않은 내용"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			series, err := Parse(strings.NewReader(c.input + "\n"))
			if err == nil {
				t.Fatalf("입력 %q: 에러가 나야 하는데 성공했다 (series=%v)", c.input, series)
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("에러 메시지에 %q 가 없다: %v", c.wantSubstr, err)
			}
			if !strings.Contains(err.Error(), "1번째 줄") {
				t.Errorf("에러 메시지에 줄번호(1번째 줄)가 없다: %v", err)
			}
		})
	}
}

// TestParse_여러줄_에러_줄번호가_정확하다 는 lineNo 추적이 하드코딩(예: 항상 1)이
// 아니라 실제 스캔 진행에 연동됨을 별도로 확인한다 — 위 표의 모든 케이스가
// 단일 줄이라 이 분기(2줄 이상 진행 후 에러)는 그것만으로 검증되지 않는다.
func TestParse_여러줄_에러_줄번호가_정확하다(t *testing.T) {
	input := "a 1\nb 2\nc{x=1} 3\n" // 1·2번째 줄은 정상, 3번째 줄만 라벨값 따옴표 누락

	_, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatal("3번째 줄에서 에러가 나야 한다")
	}
	if !strings.Contains(err.Error(), "3번째 줄") {
		t.Fatalf("에러 메시지에 3번째 줄 표기가 없다: %v", err)
	}
	if strings.Contains(err.Error(), "1번째 줄") || strings.Contains(err.Error(), "2번째 줄") {
		t.Fatalf("엉뚱한 줄번호가 찍혔다(하드코딩 의심): %v", err)
	}
}

// TestParse_주석과_라벨없는_평문 은 `# HELP`/`# TYPE` 사이에 낀 라벨 없는 평문
// 한 줄이 주석은 결과에서 빠진 채 정확히 파싱되는지 확인한다.
func TestParse_주석과_라벨없는_평문(t *testing.T) {
	input := "# HELP up 스크레이프 대상 상태\n# TYPE up gauge\nup 1\n"

	series, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: 예기치 않은 에러: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("주석 줄이 결과에 섞여 들어갔다: got %d 개, want 1", len(series))
	}
	s := series[0]
	if s.Name != "up" {
		t.Errorf("Name: got %q, want %q", s.Name, "up")
	}
	if len(s.Labels) != 0 {
		t.Errorf("Labels: got %v, want 빈 라벨", s.Labels)
	}
	if s.Value != 1 {
		t.Errorf("Value: got %v, want 1", s.Value)
	}
}

// TestParse_공백만있는_줄도_빈줄로_취급 은 "빈 줄" 판정이 원본 문자열(line=="")
// 이 아니라 TrimSpace 결과에 근거함을 확인한다 — 공백/탭만 있는 줄을 빈
// 문자열과 다르게 취급하는 구현이면 이 테스트가 실패한다.
func TestParse_공백만있는_줄도_빈줄로_취급(t *testing.T) {
	input := "# c\n   \nup 1\n\t\nup 2\n"

	series, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: 예기치 않은 에러: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("공백만 있는 줄이 파싱을 방해했다: got %d, want 2", len(series))
	}
}

// TestParse_실제_nodevitals_픽스처 는 프롬프트가 지정한 실제 /metrics 형식
// (nodevitals_hw_* 자체 메트릭 + node_exporter 계열 node_* 메트릭)을 HELP/TYPE
// 주석·빈 줄과 함께 섞어 라벨 맵·값까지 개별 단정한다.
func TestParse_실제_nodevitals_픽스처(t *testing.T) {
	input := `# HELP nodevitals_hw_temp_celsius 하드웨어 온도 센서 값(섭씨)
# TYPE nodevitals_hw_temp_celsius gauge
nodevitals_hw_temp_celsius{node="e101",tier="core",device="hwmon0"} 42.5

# HELP node_cpu_seconds_total CPU 가 각 모드에서 소비한 누적 초
# TYPE node_cpu_seconds_total counter
node_cpu_seconds_total{cpu="0",mode="idle"} 12345.6
node_cpu_seconds_total{cpu="0",mode="user"} 678.9
`
	series, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: 예기치 않은 에러: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("시리즈 개수: got %d, want 3 (주석/빈줄 제외)", len(series))
	}

	temp := series[0]
	if temp.Name != "nodevitals_hw_temp_celsius" {
		t.Errorf("Name: got %q", temp.Name)
	}
	if want := (map[string]string{"node": "e101", "tier": "core", "device": "hwmon0"}); !reflect.DeepEqual(temp.Labels, want) {
		t.Errorf("Labels: got %v, want %v", temp.Labels, want)
	}
	if temp.Value != 42.5 {
		t.Errorf("Value: got %v, want 42.5", temp.Value)
	}
	if temp.TimestampMS != 0 {
		t.Errorf("TimestampMS: got %d, want 0 (타임스탬프 없음)", temp.TimestampMS)
	}

	idle := series[1]
	if idle.Name != "node_cpu_seconds_total" {
		t.Errorf("Name: got %q", idle.Name)
	}
	if want := (map[string]string{"cpu": "0", "mode": "idle"}); !reflect.DeepEqual(idle.Labels, want) {
		t.Errorf("Labels: got %v, want %v", idle.Labels, want)
	}
	if idle.Value != 12345.6 {
		t.Errorf("Value: got %v, want 12345.6", idle.Value)
	}

	user := series[2]
	if want := (map[string]string{"cpu": "0", "mode": "user"}); !reflect.DeepEqual(user.Labels, want) {
		t.Errorf("Labels: got %v, want %v", user.Labels, want)
	}
	if user.Value != 678.9 {
		t.Errorf("Value: got %v, want 678.9", user.Value)
	}
}

// TestParse_라벨블록_trailing_comma_허용 은 Prometheus 명세상 유효한 라벨 리스트
// trailing comma(`m{a="1",} 1`)가 에러 없이 파싱되는지 확인한다 — 적대검증
// 지적사항 회귀 테스트: scanLabelBlock 이 콤마 소비 후 재루프에서 '}' 를
// 라벨 이름 시작 문자로 오인해 "라벨 이름이 비어 있다" 에러를 내던 결함.
// 콤마 뒤 공백이 낀 경우(`m{a="1", } 1`)까지 별도 입력으로 함께 확인해
// 새로 추가한 skipSpaces 분기까지 실행시킨다(동어반복 방지).
func TestParse_라벨블록_trailing_comma_허용(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"콤마_직후_닫힘", `m{a="1",} 1`},
		{"콤마_공백_닫힘", `m{a="1", } 1`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			series, err := Parse(strings.NewReader(c.input + "\n"))
			if err != nil {
				t.Fatalf("Parse(%q): trailing comma 인데 에러가 났다: %v", c.input, err)
			}
			if len(series) != 1 {
				t.Fatalf("시리즈 개수: got %d, want 1", len(series))
			}
			s := series[0]
			if s.Name != "m" {
				t.Errorf("Name: got %q, want %q", s.Name, "m")
			}
			if want := (map[string]string{"a": "1"}); !reflect.DeepEqual(s.Labels, want) {
				t.Errorf("Labels: got %v, want %v", s.Labels, want)
			}
			if s.Value != 1 {
				t.Errorf("Value: got %v, want 1", s.Value)
			}
		})
	}
}

// TestParse_빈_입력은_빈_결과 는 0바이트 입력이 에러 없이 빈 결과를 내는지
// 확인한다 (스캔 루프가 한 번도 안 돈 경계 케이스).
func TestParse_빈_입력은_빈_결과(t *testing.T) {
	series, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse(빈 입력): 예기치 않은 에러: %v", err)
	}
	if len(series) != 0 {
		t.Fatalf("빈 입력인데 시리즈가 나왔다: %v", series)
	}
}

// TestParse_줄이_1MiB_초과면_스캔에러 는 Parse 가 sc.Err() 를 확인하는 분기
// (bufio.Scanner 버퍼 상한 초과)가 실제로 도달 가능하고 bufio.ErrTooLong 을
// %w 로 래핑함을 errors.Is 로 단정한다 — constraints §테스트 함정의
// "특정 에러는 errors.Is 로 단정" 지침 준수.
func TestParse_줄이_1MiB_초과면_스캔에러(t *testing.T) {
	huge := strings.Repeat("x", maxLineBytes+1024)

	_, err := Parse(strings.NewReader(huge + "\n"))
	if err == nil {
		t.Fatal("1MiB 초과 줄인데 에러가 안 났다")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("bufio.ErrTooLong 이 래핑되어 있지 않다: %v", err)
	}
}
