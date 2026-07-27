package demo

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// GPU 모델 스펙 — 전력·메모리 상한이 신호 합성의 물리 근거다.
type gpuModelSpec struct {
	Name     string
	TDPWatts float64
	MemBytes float64
	IdleW    float64
}

var gpuModels = []gpuModelSpec{
	{Name: "NVIDIA H100", TDPWatts: 700, MemBytes: 80 * 1024 * 1024 * 1024, IdleW: 70},
	{Name: "NVIDIA A100", TDPWatts: 400, MemBytes: 80 * 1024 * 1024 * 1024, IdleW: 55},
	{Name: "NVIDIA L40S", TDPWatts: 350, MemBytes: 48 * 1024 * 1024 * 1024, IdleW: 40},
	{Name: "NVIDIA B200", TDPWatts: 1000, MemBytes: 192 * 1024 * 1024 * 1024, IdleW: 90},
}

// 서빙 풀 — 라벨 값은 ASCII(셀렉터 안전), 표시명은 /demo/state 에서만 노출.
type PoolInfo struct {
	ID      string
	Display string
	Tenant  string  // 소유 테넌트(팀) 라벨 값
	Weight  float64 // 플릿 용량 배분 비율(합 1.0)
}

// Weight 는 플릿 용량 배분 비율이다 — 실제 플랫폼에서 서빙 풀은 균등하지
// 않다(주력 LLM 이 크고 OCR 배치가 작다). 균등 배분이나 해시 충돌로 인한
// 0장 풀은 즉시 가짜로 읽힌다 — 용량 없는 서빙 풀은 존재할 수 없다.
var demoPools = []PoolInfo{
	{ID: "llm-serving-a", Display: "LLM 서빙 A", Tenant: "ai-platform", Weight: 0.22},
	{ID: "llm-serving-b", Display: "LLM 서빙 B", Tenant: "ai-platform", Weight: 0.14},
	{ID: "vision-inference", Display: "비전 추론", Tenant: "vision-ai", Weight: 0.13},
	{ID: "speech-inference", Display: "음성 추론", Tenant: "speech-ai", Weight: 0.09},
	{ID: "rag-api", Display: "RAG API", Tenant: "search-rec", Weight: 0.12},
	{ID: "ocr-batch", Display: "OCR 배치", Tenant: "doc-ai", Weight: 0.08},
	{ID: "embedding", Display: "임베딩", Tenant: "search-rec", Weight: 0.12},
	{ID: "recsys-serving", Display: "추천 서빙", Tenant: "recsys", Weight: 0.10},
}

// pickPool 은 가중치대로 풀을 고른다. 배분 단위는 **노드**다 — 실제로 한
// 노드의 GPU 8장은 같은 서빙 풀에 묶인다(모델 상주·NVLink 활용).
func pickPool(seed int64, instance string) PoolInfo {
	r := unitFloat(fnvHash(seed, "poolpick", instance))
	acc := 0.0
	for _, p := range demoPools {
		acc += p.Weight
		if r < acc {
			return p
		}
	}
	return demoPools[len(demoPools)-1]
}

// TenantInfo 는 테넌트(팀) 표시명이다.
type TenantInfo struct {
	ID      string
	Display string
}

var demoTenants = []TenantInfo{
	{ID: "ai-platform", Display: "AI 플랫폼팀"},
	{ID: "vision-ai", Display: "비전 AI팀"},
	{ID: "speech-ai", Display: "음성 AI팀"},
	{ID: "search-rec", Display: "검색추천팀"},
	{ID: "doc-ai", Display: "문서 AI팀"},
	{ID: "recsys", Display: "추천시스템팀"},
}

// 사용률 대역 — Sentinel 계열 관제 UI 의 5단 범례(Idle/<30/<80/<95/≥95)와
// 정합하는 분포를 만들기 위한 시드다. 값은 (기준선, 진폭 상한).
type utilBand struct {
	base, spread float64
}

var utilBands = []utilBand{
	{base: 4, spread: 4},   // 0: idle 성 저활용 (할당됐지만 거의 안 씀 — 회수 검토 대상)
	{base: 22, spread: 7},  // 1: 저활용 (<30)
	{base: 62, spread: 14}, // 2: 중간
	{base: 88, spread: 7},  // 3: 고부하
}

// bandWeights 는 할당 GPU 의 대역 분포다. idle 8% + low 12% 로 "할당 대비
// 실사용 갭"(효율 화면의 핵심 메시지)이 구조적으로 생기게 한다.
var bandWeights = []float64{0.08, 0.12, 0.50, 0.30}

// GPU 는 데모 플릿의 GPU 한 장이다. 신호 합성 파라미터(대역·위상·주기)를
// 생성 시점에 결정론으로 고정한다 — 같은 seed 는 항상 같은 플릿을 만든다.
type GPU struct {
	UUID      string
	Instance  string // 노드명 (실 에이전트의 instance 라벨 계약)
	Device    string // "gpu0" ...
	Model     string
	CSP       string
	Region    string // CSP 내 리전 — 공통 필터 차원(클러스터의 함수)
	AZ        string // 리전 내 가용영역 — 노드의 함수(장애 체인의 최상위 경계)
	Cluster   string
	NodePool  string // K8s 노드 풀 — 서빙 풀(Pool)과 다른 축이다
	Rack      string // 물리 랙 — 냉각·전원 공유 단위(발열 상관의 근거)
	Pool      string // 미할당이면 ""
	Tenant    string // 미할당이면 ""
	Allocated bool

	// 30일+ 장기 미사용 관리 대상(REQ 서사) — 0 이면 해당 없음.
	IdleDays int

	// 신호 합성 파라미터 (fleet 생성 시 고정)
	band             int
	phase1, phase2   float64
	period1, period2 float64 // ms
	eccProne         bool    // 정정 ECC 가 느리게 누적되는 개체

	// 시리즈 라벨 사전 계산 — 틱마다 4.9만 회 LabelsFromMap 을 부르는 대신
	// 생성 시 1회 계산해 재사용한다(정렬 계약은 LabelsFromMap 이 보장).
	seriesLabels map[string]tsdb.Labels
}

// Node 는 GPU 를 담는 노드다.
type Node struct {
	Instance string
	CSP      string
	Region   string
	// AZ 는 리전 내 가용영역이다 — 같은 AZ 는 전원·네트워크 장애 도메인을
	// 공유하므로 장애 영향 범위의 최상위 경계가 된다.
	AZ string
	// NodePool 은 K8s 노드 풀이다. 서빙 풀(GPU.Pool = 테넌트 워크로드)과
	// 완전히 다른 축이라 이름을 섞으면 안 된다.
	NodePool string
	Cluster  string
	Model    string
	// Rack 은 물리 랙 이름이다 — 같은 랙은 냉각·전원을 공유하므로 발열이
	// 상관된다(rackThermalBias). 자산 화면의 위치 정보이기도 하다.
	Rack string
	// PDU 는 랙 내 전원 분배 계통(A/B 이중화).
	PDU  string
	GPUs []*GPU
}

// ClusterInfo 는 클러스터 메타다.
type ClusterInfo struct {
	Region string
	Name   string
	CSP    string
	Model  string
}

// Fleet 은 결정론으로 생성된 데모 인벤토리 전체다.
type Fleet struct {
	Specs    []CSPSpec
	Nodes    []*Node
	GPUs     []*GPU
	Clusters []ClusterInfo
	Pools    []PoolInfo
	Tenants  []TenantInfo
	ByUUID   map[string]*GPU

	seed int64
}

// fnvHash 는 결정론 파생값의 공통 원천이다 — math/rand 상태를 두지 않고
// (seed, 문자열 키) → uint64 순수 함수로만 파생해, 생성 순서와 무관하게
// 같은 입력이 항상 같은 값을 내게 한다.
func fnvHash(seed int64, parts ...string) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(seed >> (8 * i))
	}
	_, _ = h.Write(buf[:])
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0xff})
	}
	return h.Sum64()
}

// unitFloat 은 해시를 [0,1) 실수로 바꾼다.
func unitFloat(h uint64) float64 {
	return float64(h%1_000_000) / 1_000_000
}

// BuildFleet 은 스펙과 seed 로 플릿을 결정론 생성한다. 같은 (spec, seed) 는
// 항상 동일한 인벤토리(uuid·배치·대역)를 만든다 — 테스트와 재기동 안정성의
// 전제다.
func BuildFleet(specs []CSPSpec, seed int64) *Fleet {
	f := &Fleet{
		Specs:   specs,
		Pools:   demoPools,
		Tenants: demoTenants,
		ByUUID:  map[string]*GPU{},
		seed:    seed,
	}

	for _, spec := range specs {
		// CSP 당 클러스터 수 — 700장당 1개꼴, 최소 1개.
		nClusters := spec.GPUs / 700
		if nClusters < 1 {
			nClusters = 1
		}
		perCluster := spec.GPUs / nClusters
		remainder := spec.GPUs - perCluster*nClusters

		for c := 0; c < nClusters; c++ {
			clusterName := fmt.Sprintf("%s-%s", spec.ID, string(rune('a'+c)))
			region := regionOf(spec.ID, c)
			// 클러스터 단위로 GPU 모델을 배정한다(현실 관행: 동일 세대 묶음).
			model := gpuModels[int(fnvHash(seed, "model", clusterName)%uint64(len(gpuModels)))]
			f.Clusters = append(f.Clusters, ClusterInfo{Region: region, Name: clusterName, CSP: spec.ID, Model: model.Name})

			count := perCluster
			if c == nClusters-1 {
				count += remainder
			}
			f.buildCluster(spec, clusterName, region, model, count)
		}
	}
	return f
}

// buildCluster 는 클러스터 하나(노드 8장 단위)를 채운다.
func (f *Fleet) buildCluster(spec CSPSpec, cluster, region string, model gpuModelSpec, gpuCount int) {
	const gpusPerNode = 8
	nodeIdx := 0
	for made := 0; made < gpuCount; {
		instance := fmt.Sprintf("%s-gpu-%03d", cluster, nodeIdx)
		rack := rackOf(cluster, nodeIdx)
		az := azOf(region, nodeIdx)
		nodePool := nodePoolOf(cluster, model.Name, nodeIdx)
		node := &Node{
			Instance: instance, CSP: spec.ID, Region: region, AZ: az,
			NodePool: nodePool, Cluster: cluster, Model: model.Name,
			Rack: rack,
			PDU:  []string{"A", "B"}[nodeIdx%2],
		}

		for d := 0; d < gpusPerNode && made < gpuCount; d++ {
			g := f.buildGPU(spec, cluster, region, az, nodePool, model, instance, rack, d)
			node.GPUs = append(node.GPUs, g)
			f.GPUs = append(f.GPUs, g)
			f.ByUUID[g.UUID] = g
			made++
		}
		f.Nodes = append(f.Nodes, node)
		nodeIdx++
	}
}

func (f *Fleet) buildGPU(spec CSPSpec, cluster, region, az, nodePool string, model gpuModelSpec, instance, rack string, device int) *GPU {
	key := fmt.Sprintf("%s/gpu%d", instance, device)
	h := fnvHash(f.seed, "gpu", key)

	g := &GPU{
		UUID:     fmt.Sprintf("GPU-%08x-%04x", uint32(h), uint16(h>>32)),
		Instance: instance,
		Device:   fmt.Sprintf("gpu%d", device),
		Model:    model.Name,
		CSP:      spec.ID,
		Region:   region,
		AZ:       az,
		NodePool: nodePool,
		Cluster:  cluster,
		Rack:     rack,
	}

	// 할당 여부 — 약 93% (Sentinel 실측 93.7% 근사). 미할당은 idle 신호만 낸다.
	g.Allocated = unitFloat(fnvHash(f.seed, "alloc", key)) < 0.93

	if g.Allocated {
		pool := pickPool(f.seed, instance)
		g.Pool = pool.ID
		g.Tenant = pool.Tenant
		g.band = pickBand(unitFloat(fnvHash(f.seed, "band", key)))
	} else {
		g.band = 0
	}

	// 장기 미사용 관리 대상 — 할당됐지만 idle 대역인 개체 중 일부(≈35%)에
	// 31~90일 미사용 일수를 부여한다. 효율 화면의 "30일+ 미사용" 테이블 소재.
	if g.Allocated && g.band == 0 && unitFloat(fnvHash(f.seed, "idle", key)) < 0.35 {
		g.IdleDays = 31 + int(fnvHash(f.seed, "idledays", key)%60)
	}

	// 신호 합성 파라미터 — 20~45분 주기 사인 2개 + 위상.
	g.phase1 = unitFloat(fnvHash(f.seed, "p1", key)) * 2 * math.Pi
	g.phase2 = unitFloat(fnvHash(f.seed, "p2", key)) * 2 * math.Pi
	g.period1 = float64(20*60_000) + unitFloat(fnvHash(f.seed, "t1", key))*25*60_000
	g.period2 = float64(6*60_000) + unitFloat(fnvHash(f.seed, "t2", key))*9*60_000
	g.eccProne = unitFloat(fnvHash(f.seed, "ecc", key)) < 0.01

	g.seriesLabels = buildSeriesLabels(g)
	return g
}

// regionOf 는 클러스터가 속한 리전이다 — CSP 마다 2개 리전에 번갈아 배치한다
// (이중 리전 운영이 현실 관행). 리전은 *클러스터의 함수* 이므로 시리즈 라벨에
// 넣어도 시리즈 수는 늘지 않는다 — 필터 차원만 하나 는다.
func regionOf(cspID string, clusterIdx int) string {
	return fmt.Sprintf("%s-kr%d", cspID, clusterIdx%2+1)
}

// azOf 는 노드가 속한 가용영역이다 — 리전마다 3개 AZ 에 라운드로빈으로 깐다.
func azOf(region string, nodeIdx int) string {
	return fmt.Sprintf("%s%c", region, 'a'+rune(nodeIdx%3))
}

// nodePoolOf 는 K8s 노드 풀이다. 현실 관행대로 클러스터 안에서 모델별 풀을
// 두고, 같은 모델도 온디맨드/스팟 2개로 나눈다.
func nodePoolOf(cluster, model string, nodeIdx int) string {
	slug := strings.ToLower(strings.ReplaceAll(model, " ", "-"))
	kind := "od"
	if nodeIdx%4 == 3 {
		kind = "spot"
	}
	return fmt.Sprintf("%s-%s-%s", cluster, slug, kind)
}

func pickBand(u float64) int {
	acc := 0.0
	for i, w := range bandWeights {
		acc += w
		if u < acc {
			return i
		}
	}
	return len(bandWeights) - 1
}

// modelSpecOf 는 모델명 → 스펙 역참조다.
func modelSpecOf(name string) gpuModelSpec {
	for _, m := range gpuModels {
		if m.Name == name {
			return m
		}
	}
	return gpuModels[0]
}

// buildSeriesLabels 는 GPU 한 장의 per-metric 라벨셋을 사전 계산한다.
// 메트릭 이름은 실 에이전트(nodevitals)와 동일 계약 — 데모와 실환경이 같은
// 프론트 코드를 공유하는 전제다. csp/cluster/pool/tenant 는 데모 확장 라벨이다
// (서버 집계가 없어 등호 매처 필터의 유일한 수단).
func buildSeriesLabels(g *GPU) map[string]tsdb.Labels {
	base := map[string]string{
		"instance":  g.Instance,
		"device":    g.Device,
		"gpu_uuid":  g.UUID,
		"gpu_model": g.Model,
		"tier":      "gpu",
		"csp":       g.CSP,
		"region":    g.Region,
		"cluster":   g.Cluster,
	}
	if g.Pool != "" {
		base["pool"] = g.Pool
		base["tenant"] = g.Tenant
	}
	out := make(map[string]tsdb.Labels, len(gpuMetricNames))
	for _, name := range gpuMetricNames {
		m := make(map[string]string, len(base)+1)
		for k, v := range base {
			m[k] = v
		}
		m[tsdb.MetricName] = name
		out[name] = tsdb.LabelsFromMap(m)
	}
	return out
}

// ReplaceUUID 는 노드 교체 서사(시나리오 replacing 단계)에서 victim GPU 의
// uuid 를 새 값으로 바꾼다 — 구 uuid 시리즈는 emit 이 멈춰 instant 5분 창에서
// 자연 소멸하고, 새 uuid 시리즈가 시작된다. 라벨 사전 계산도 갱신한다.
func (f *Fleet) ReplaceUUID(g *GPU, cycle int) {
	delete(f.ByUUID, g.UUID)
	h := fnvHash(f.seed, "replace", g.Instance, g.Device, fmt.Sprintf("%d", cycle))
	g.UUID = fmt.Sprintf("GPU-%08x-%04x", uint32(h), uint16(h>>32))
	g.seriesLabels = buildSeriesLabels(g)
	f.ByUUID[g.UUID] = g
}
