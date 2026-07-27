// fidelity_test.go — "실제 구동되는 서비스와 유사한가"를 지키는 계약들.
// 합성 데이터가 가짜로 읽히는 지점은 대개 정해져 있다: 낮/밤이 없고, 장애가
// 무작위로 흩어지고, 하드웨어 지표만 있고, 서로 다른 화면이 다른 이야기를 한다.
// 아래 테스트는 그 네 가지가 되돌아오지 않게 고정한다.
package demo

import (
	"math"
	"testing"
)

func TestDiurnal_낮밤_리듬이_있다(t *testing.T) {
	// KST 자정 기준 epoch — 하루를 훑어 피크/골 시각을 확인한다.
	base := int64(1_767_225_600_000) - kstOffsetMS // 2026-01-01 00:00 KST

	var peakH, troughH float64
	peak, trough := -9.0, 9.0
	for h := 0; h < 24; h++ {
		f := diurnalFactor(base + int64(h)*3_600_000)
		if f > peak {
			peak, peakH = f, float64(h)
		}
		if f < trough {
			trough, troughH = f, float64(h)
		}
	}
	// 업무시간(11~17시)에 피크, 새벽~이른아침(04~08시)에 골이어야 한다.
	if peakH < 11 || peakH > 17 {
		t.Fatalf("일간 피크가 %0.f시 — 업무시간(11~17시)이 아니다", peakH)
	}
	if troughH < 4 || troughH > 8 {
		t.Fatalf("일간 골이 %0.f시 — 이른 아침(04~08시)이 아니다", troughH)
	}
	if peak-trough < 0.25 {
		t.Fatalf("일간 진폭 %.3f — 낮밤 차이가 눈에 띄지 않는다", peak-trough)
	}

	// 새벽 배치 봉우리 — 03시가 01시보다 높아야 한다.
	if diurnalFactor(base+3*3_600_000) <= diurnalFactor(base+1*3_600_000) {
		t.Fatalf("새벽 배치 봉우리가 없다(03시 ≤ 01시)")
	}
}

func TestRack_온도가_랙단위로_상관된다(t *testing.T) {
	cfg := testConfig(t, "a:클라우드 A:256")
	f := BuildFleet(cfg.Fleet, cfg.Seed)

	// 랙별 평균 온도를 모아 편차가 유의미한지 본다 — 전부 독립 랜덤이면
	// 랙 평균이 거의 같아진다(실제 데이터센터는 그렇지 않다).
	byRack := map[string][]float64{}
	for _, g := range f.GPUs {
		sig := computeSignals(g, t0, cfg.Seed)
		byRack[g.Rack] = append(byRack[g.Rack], sig.Temp)
	}
	if len(byRack) < 4 {
		t.Fatalf("랙이 %d개 — 랙 구분이 되지 않는다", len(byRack))
	}
	means := make([]float64, 0, len(byRack))
	for _, temps := range byRack {
		sum := 0.0
		for _, v := range temps {
			sum += v
		}
		means = append(means, sum/float64(len(temps)))
	}
	min, max := means[0], means[0]
	for _, m := range means {
		min, max = math.Min(min, m), math.Max(max, m)
	}
	if max-min < 2.5 {
		t.Fatalf("랙 평균 온도 편차 %.1f°C — 랙 상관이 없다(개별 무작위로 보인다)", max-min)
	}

	// 같은 랙의 두 GPU 는 랙 편차를 공유해야 한다.
	var rack string
	for r, temps := range byRack {
		if len(temps) >= 8 {
			rack = r
			break
		}
	}
	if rack != "" {
		b1 := rackThermalBias(cfg.Seed, rack, t0)
		b2 := rackThermalBias(cfg.Seed, rack, t0)
		if b1 != b2 {
			t.Fatalf("같은 랙·시각의 편차가 다르다 — 결정론 위반")
		}
	}
}

func TestServing_하드웨어와_인과로_연결된다(t *testing.T) {
	pool := PoolInfo{ID: "llm-serving-a", Display: "LLM 서빙 A", Tenant: "ai-platform"}

	low := computeServing(pool, 100, 30, t0, 7)
	high := computeServing(pool, 100, 95, t0, 7)

	if high.RPS <= low.RPS {
		t.Fatalf("사용률이 올랐는데 RPS 가 늘지 않았다: %.1f → %.1f", low.RPS, high.RPS)
	}
	if high.QueueDepth <= low.QueueDepth {
		t.Fatalf("고부하인데 큐가 깊어지지 않았다: %.1f → %.1f", low.QueueDepth, high.QueueDepth)
	}
	if high.P95MS <= low.P95MS {
		t.Fatalf("큐가 깊어졌는데 p95 지연이 늘지 않았다: %.0f → %.0f", low.P95MS, high.P95MS)
	}
	if !(high.P50MS < high.P95MS && high.P95MS < high.P99MS) {
		t.Fatalf("백분위 순서가 깨졌다: p50=%.0f p95=%.0f p99=%.0f", high.P50MS, high.P95MS, high.P99MS)
	}
	if high.ErrorRatePct <= low.ErrorRatePct {
		t.Fatalf("포화 구간인데 에러율이 오르지 않았다: %.3f → %.3f", low.ErrorRatePct, high.ErrorRatePct)
	}
	if !high.Saturated || low.Saturated {
		t.Fatalf("포화 판정 오류: low=%v high=%v", low.Saturated, high.Saturated)
	}

	// 임베딩 풀은 LLM 보다 훨씬 빠르고 요청량이 많아야 한다(프로파일 차이).
	emb := computeServing(PoolInfo{ID: "embedding"}, 100, 95, t0, 7)
	if emb.P50MS >= high.P50MS || emb.RPS <= high.RPS {
		t.Fatalf("풀 프로파일이 구분되지 않는다 — 임베딩 p50=%.0f rps=%.0f vs LLM p50=%.0f rps=%.0f",
			emb.P50MS, emb.RPS, high.P50MS, high.RPS)
	}
	// 임베딩은 토큰 생성이 없다.
	if emb.TokensPerSec != 0 {
		t.Fatalf("임베딩 풀에 토큰 생성이 있다: %.1f", emb.TokensPerSec)
	}
}

func TestSLO_가용률과_예산이_상태를_따른다(t *testing.T) {
	healthy := computeSLO(7000, 2, 10, 20)
	if healthy.AvailabilityPct <= 99.9 {
		t.Fatalf("장애 2장인데 가용률 %.3f%%", healthy.AvailabilityPct)
	}
	if healthy.Breaching {
		t.Fatalf("정상 상태인데 SLO 위반 판정")
	}

	degraded := computeSLO(7000, 200, 10, 20)
	if !degraded.Breaching {
		t.Fatalf("가용률 %.2f%% 인데 위반 판정이 아니다", degraded.AvailabilityPct)
	}
	// 예산은 상시 불가 비율에 반응해야 한다 — 이게 없으면 패널이 늘 100% 다.
	clean := computeSLO(7000, 2, 5, 20)
	dirty := computeSLO(7000, 20, 5, 20)
	if dirty.ErrorBudgetRemainingPct >= clean.ErrorBudgetRemainingPct-5 {
		t.Fatalf("불가 GPU 가 10배인데 예산이 유의미하게 줄지 않았다: %.1f → %.1f",
			clean.ErrorBudgetRemainingPct, dirty.ErrorBudgetRemainingPct)
	}
	// 인시던트가 늘면 예산이 줄어야 한다.
	few := computeSLO(7000, 2, 5, 20)
	many := computeSLO(7000, 2, 40, 20)
	if many.ErrorBudgetRemainingPct >= few.ErrorBudgetRemainingPct {
		t.Fatalf("인시던트가 늘었는데 예산이 줄지 않았다: %.1f → %.1f",
			few.ErrorBudgetRemainingPct, many.ErrorBudgetRemainingPct)
	}
	// MTBF 는 인시던트 수에 반비례한다.
	if many.MTBFHours >= few.MTBFHours {
		t.Fatalf("MTBF 가 인시던트에 반비례하지 않는다")
	}
}

func TestSnapshot_서빙_SLO_이벤트가_노출된다(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:64,b:클라우드 B:32")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	// 한 틱 emit 해야 파생 상태(서빙·SLO)가 채워진다.
	if err := e.emitFast(t0); err != nil {
		t.Fatalf("emitFast: %v", err)
	}
	snap := e.buildSnapshot(t0)

	if len(snap.Serving) != len(e.fleet.Pools) {
		t.Fatalf("서빙 통계 %d개, want %d", len(snap.Serving), len(e.fleet.Pools))
	}
	served := false
	for _, s := range snap.Serving {
		if s.GPUs > 0 && s.RPS > 0 {
			served = true
		}
	}
	if !served {
		t.Fatalf("모든 풀의 RPS 가 0 — 서빙 계층이 죽어 있다")
	}
	if snap.SLO.TargetPct != sloTargetPct || snap.SLO.AvailabilityPct <= 0 {
		t.Fatalf("SLO 미노출: %+v", snap.SLO)
	}
	if len(snap.Events) == 0 {
		t.Fatalf("K8s 이벤트가 비었다 — 부팅 시드가 동작하지 않는다")
	}
	last := snap.Events[0]
	if last.Reason == "" || last.Object == "" || last.Node == "" {
		t.Fatalf("이벤트 필드가 비었다: %+v", last)
	}
}

func TestNodeAsset_자산대장이_채워진다(t *testing.T) {
	db := newTestDB(t)
	cfg := testConfig(t, "a:클라우드 A:64")
	e := NewEngine(db, cfg, func() int64 { return t0 })

	node := e.fleet.Nodes[0]
	asset, ok := e.NodeAssetByInstance(node.Instance)
	if !ok {
		t.Fatalf("노드 자산 조회 실패: %s", node.Instance)
	}
	if asset.Rack == "" || asset.PDU == "" || asset.SerialNumber == "" || asset.InstalledAt == "" {
		t.Fatalf("자산 필드 누락: %+v", asset)
	}
	if len(asset.Labels) == 0 || asset.KubeletVersion == "" {
		t.Fatalf("K8s 메타 누락: labels=%d kubelet=%q", len(asset.Labels), asset.KubeletVersion)
	}
	if len(asset.GPUs) != len(node.GPUs) {
		t.Fatalf("GPU 자산 %d개, want %d", len(asset.GPUs), len(node.GPUs))
	}
	g := asset.GPUs[0]
	if g.Serial == "" || g.VBIOS == "" || g.DriverVersion == "" || g.PcieGen == "" {
		t.Fatalf("GPU 자산 필드 누락: %+v", g)
	}
	// 같은 클러스터는 같은 드라이버(롤아웃 단위) — 구성 드리프트 서사의 전제.
	for _, x := range asset.GPUs {
		if x.DriverVersion != g.DriverVersion {
			t.Fatalf("같은 노드에서 드라이버가 갈린다: %s vs %s", g.DriverVersion, x.DriverVersion)
		}
	}
	if _, ok := e.NodeAssetByInstance("없는-노드"); ok {
		t.Fatalf("없는 노드가 조회됐다")
	}
}

func TestPool_모든_서빙풀에_용량이_있다(t *testing.T) {
	specs, err := ParseFleetSpec(defaultFleetSpec)
	if err != nil {
		t.Fatalf("기본 플릿 스펙: %v", err)
	}
	f := BuildFleet(specs, 42)

	count := map[string]int{}
	for _, g := range f.GPUs {
		if g.Allocated {
			count[g.Pool]++
		}
	}
	// 용량 0인 서빙 풀은 존재할 수 없다 — 화면에 "0장" 풀이 보이는 순간
	// 배분이 해시 충돌로 만들어졌다는 게 드러난다.
	for _, p := range demoPools {
		if count[p.ID] == 0 {
			t.Fatalf("서빙 풀 %q 에 GPU 가 0장 — 용량 없는 풀은 있을 수 없다", p.Display)
		}
	}

	// 가중치 순서가 대체로 지켜져야 한다(주력 LLM 이 OCR 배치보다 크다).
	if count["llm-serving-a"] <= count["ocr-batch"] {
		t.Fatalf("가중 배분이 반영되지 않았다: llm-serving-a=%d ocr-batch=%d",
			count["llm-serving-a"], count["ocr-batch"])
	}
	// 한 노드의 GPU 는 같은 풀이어야 한다(모델 상주·NVLink 활용 관행).
	for _, n := range f.Nodes {
		first := ""
		for _, g := range n.GPUs {
			if !g.Allocated {
				continue
			}
			if first == "" {
				first = g.Pool
			} else if g.Pool != first {
				t.Fatalf("노드 %s 안에서 풀이 갈린다: %s vs %s", n.Instance, first, g.Pool)
			}
		}
	}
}
