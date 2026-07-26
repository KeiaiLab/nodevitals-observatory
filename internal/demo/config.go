// Package demo 는 GPU 플릿 데모 모드를 구현한다 — 멀티클라우드 GPU 플릿
// (기본 7,000장)을 합성해 tsdb 에 실시간 주입하고, 무음 장애 → 승인 격리 →
// 번인 검증 → 재투입의 운영 시나리오를 상태머신으로 재현한다.
//
// 이 패키지는 범용 "GPU fleet demo mode" 다. 특정 고객·브랜드·서비스명을
// 코드/기본값에 포함하지 않는다(공개 OSS 규약). CSP 표시명·규모는 전부
// OBSERVATORY_DEMO_FLEET env 로 재정의 가능하다.
//
// 시간 규약(common-constraints.md): 엔진 로직은 전부 밀리초 epoch 를 인자로
// 받는다. wall clock 은 cmd/observatory 가 주입하는 nowFn 에만 존재한다.
package demo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CSPSpec 은 데모 플릿의 CSP 하나를 정의한다. Adapter 는 시연 서사용 구분이다 —
// "native"(실제 연동 시늉) / "mock"(Mock Adapter 배지). 첫 항목이 native 다.
type CSPSpec struct {
	ID      string // 라벨 값 (ASCII — 셀렉터 파서 안전)
	Display string // 사람용 표시명 (/demo/state 에서만 노출)
	GPUs    int
	Adapter string // "native" | "mock"
}

// Config 는 데모 엔진의 전체 설정이다.
type Config struct {
	Seed        int64
	Fleet       []CSPSpec
	BackfillAgg time.Duration // 집계 시리즈 백필 창
	BackfillGPU time.Duration // per-GPU 시리즈 백필 창
	// TimeScale 은 시나리오 단계 duration 을 나누는 배속이다(리허설용).
	// 데이터 emit 주기(15s/60s)는 바꾸지 않는다 — instant query 5m 평가창
	// 계약 때문에 emit 주기는 배속과 무관하게 고정이어야 한다.
	TimeScale float64
}

// 기본값 — 플릿 합계 7,000 GPU. CSP 명칭은 퍼블릭 클라우드 일반명이다.
const (
	defaultSeed        = 42
	defaultFleetSpec   = "nhn:NHN Cloud:2800,kakao:Kakao Cloud:2100,naver:Naver Cloud:1400,onprem:On-Prem:700"
	defaultBackfillAgg = 24 * time.Hour
	defaultBackfillGPU = time.Hour
)

// ConfigFromEnv 는 lookup(보통 os.LookupEnv)으로 설정을 읽는다. 파싱 실패는
// 기동을 막을 만큼 치명적이지 않으므로(데모 표시 설정일 뿐) 기본값으로 안전
// 강등하되, 플릿 스펙 문법 오류만은 명시 에러를 낸다 — 규모가 통째로 틀어지면
// 시연 자체가 무의미해지기 때문이다.
func ConfigFromEnv(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		Seed:        defaultSeed,
		BackfillAgg: defaultBackfillAgg,
		BackfillGPU: defaultBackfillGPU,
		TimeScale:   1.0,
	}

	if v, ok := lookup("OBSERVATORY_DEMO_SEED"); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Seed = n
		}
	}
	spec := defaultFleetSpec
	if v, ok := lookup("OBSERVATORY_DEMO_FLEET"); ok && v != "" {
		spec = v
	}
	fleet, err := ParseFleetSpec(spec)
	if err != nil {
		return Config{}, err
	}
	cfg.Fleet = fleet

	if v, ok := lookup("OBSERVATORY_DEMO_BACKFILL_AGG"); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			cfg.BackfillAgg = d
		}
	}
	if v, ok := lookup("OBSERVATORY_DEMO_BACKFILL_GPU"); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			cfg.BackfillGPU = d
		}
	}
	if v, ok := lookup("OBSERVATORY_DEMO_TIMESCALE"); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.1 && f <= 60 {
			cfg.TimeScale = f
		}
	}
	return cfg, nil
}

// ParseFleetSpec 은 "id:표시명:GPU수,..." 문법을 파싱한다. 첫 항목이
// native 어댑터, 나머지는 mock 이다(시연 서사: 기준 CSP 1개만 실제 연동).
func ParseFleetSpec(spec string) ([]CSPSpec, error) {
	parts := strings.Split(spec, ",")
	out := make([]CSPSpec, 0, len(parts))
	for i, p := range parts {
		fields := strings.Split(strings.TrimSpace(p), ":")
		if len(fields) != 3 {
			return nil, fmt.Errorf("demo: 플릿 스펙 항목은 id:표시명:GPU수 3필드여야 한다: %q", p)
		}
		id := strings.TrimSpace(fields[0])
		display := strings.TrimSpace(fields[1])
		n, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("demo: 플릿 스펙 GPU 수가 양의 정수가 아니다: %q", p)
		}
		if id == "" || display == "" {
			return nil, fmt.Errorf("demo: 플릿 스펙 id/표시명이 비어 있다: %q", p)
		}
		adapter := "mock"
		if i == 0 {
			adapter = "native"
		}
		out = append(out, CSPSpec{ID: id, Display: display, GPUs: n, Adapter: adapter})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("demo: 플릿 스펙이 비어 있다")
	}
	return out, nil
}
