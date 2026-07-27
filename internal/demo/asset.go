package demo

// 자산 대장 — 드릴다운이 "숫자 몇 개"에 그치면 관제 화면이 아니라 그래프
// 위젯으로 보인다. 실제 운영자가 노드를 열면 드라이버·펌웨어·시리얼·MIG·
// PCIe 링크·물리 위치·K8s 라벨/테인트를 본다. 그 정보가 있어야 "이 노드를
// 교체 요청한다"는 판단이 화면 안에서 성립한다.
//
// 자산 값은 저장하지 않고 (seed, 키)에서 결정론으로 파생한다 — 7,000 GPU 에
// 문자열 필드를 얹지 않으면서도 항상 같은 값을 낸다(드릴다운은 노드 단위라
// 요청당 8장만 계산한다).

import (
	"fmt"
	"time"
)

// 플릿에 섞여 있는 드라이버·CUDA 조합. 실제 플릿은 롤아웃 중이라 버전이
// 균일하지 않다 — 이 불균일이 "구성 드리프트" 서사의 근거이기도 하다.
var driverMatrix = []struct {
	Driver string
	CUDA   string
}{
	{"570.86.15", "12.8"},
	{"570.124.06", "12.8"},
	{"565.57.01", "12.7"},
	{"550.144.03", "12.4"},
}

// GPUAsset 은 GPU 한 장의 자산 정보다.
type GPUAsset struct {
	UUID          string `json:"uuid"`
	Device        string `json:"device"`
	Model         string `json:"model"`
	Serial        string `json:"serial"`
	VBIOS         string `json:"vbios"`
	DriverVersion string `json:"driverVersion"`
	CudaVersion   string `json:"cudaVersion"`
	// MIG 프로파일 — "비활성" 또는 "1g.10gb x7" 형태.
	MigProfile string `json:"migProfile"`
	PcieGen    string `json:"pcieGen"`
	PcieWidth  string `json:"pcieWidth"`
	// NVLink 상태 — 링크 수와 상태.
	NvlinkLinks  int    `json:"nvlinkLinks"`
	NvlinkStatus string `json:"nvlinkStatus"`
	Pool         string `json:"pool"`
	Tenant       string `json:"tenant"`
	Allocated    bool   `json:"allocated"`
	// 현재 이 GPU 를 쓰는 파드(미할당이면 빈 값).
	Pod string `json:"pod,omitempty"`
}

// NodeAsset 은 노드 자산 + 소속 GPU 자산이다(드릴다운 응답).
type NodeAsset struct {
	Instance string `json:"instance"`
	CSP      string `json:"csp"`
	Cluster  string `json:"cluster"`
	Rack     string `json:"rack"`
	PDU      string `json:"pdu"`
	Model    string `json:"model"`
	GPUCount int    `json:"gpuCount"`
	// K8s 노드 정보.
	KubeletVersion string   `json:"kubeletVersion"`
	ContainerRT    string   `json:"containerRuntime"`
	OSImage        string   `json:"osImage"`
	KernelVersion  string   `json:"kernelVersion"`
	Labels         []string `json:"labels"`
	Taints         []string `json:"taints"`
	// 자산 관리 정보.
	SerialNumber string `json:"serialNumber"`
	InstalledAt  string `json:"installedAt"`
	AgeDays      int    `json:"ageDays"`
	WarrantyEnds string `json:"warrantyEnds"`
	// 상태 요약.
	Ready        bool       `json:"ready"`
	Cordoned     bool       `json:"cordoned"`
	AgentMissing bool       `json:"agentMissing"`
	GPUs         []GPUAsset `json:"gpus"`
}

// buildNodeAsset 은 노드 자산 대장을 만든다(요청 시 1회 계산).
// nowMS 는 설치 경과일 산출 기준 — 순수 함수 유지를 위해 인자로 받는다.
func (e *Engine) buildNodeAsset(node *Node, nowMS int64) NodeAsset {
	seed := e.cfg.Seed
	key := node.Instance

	dm := driverMatrix[int(fnvHash(seed, "driver", node.Cluster)%uint64(len(driverMatrix)))]
	// 노드 나이 — 180~1,100일. 오래된 노드일수록 구 드라이버일 개연이 크지만
	// 여기서는 클러스터 단위 롤아웃을 우선한다(현실의 관리 방식).
	ageDays := 180 + int(fnvHash(seed, "age", key)%920)
	installed := time.UnixMilli(nowMS).UTC().AddDate(0, 0, -ageDays)

	asset := NodeAsset{
		Instance: node.Instance, CSP: node.CSP, Cluster: node.Cluster,
		Rack: node.Rack, PDU: node.PDU, Model: node.Model, GPUCount: len(node.GPUs),
		KubeletVersion: "v1.31.4",
		ContainerRT:    "containerd://1.7.24",
		OSImage:        "Ubuntu 22.04.5 LTS",
		KernelVersion:  "5.15.0-127-generic",
		Labels: []string{
			"nvidia.com/gpu.product=" + shortModel(node.Model),
			"nvidia.com/gpu.count=" + fmt.Sprint(len(node.GPUs)),
			"topology.kubernetes.io/zone=" + node.Cluster,
			"keiailab.dev/rack=" + node.Rack,
			"keiailab.dev/pdu=" + node.PDU,
			"node-role.kubernetes.io/gpu=",
		},
		SerialNumber: fmt.Sprintf("KAI-%s-%06d", upperCSP(node.CSP), fnvHash(seed, "nodesn", key)%1_000_000),
		InstalledAt:  installed.Format("2006-01-02"),
		AgeDays:      ageDays,
		WarrantyEnds: installed.AddDate(3, 0, 0).Format("2006-01-02"),
		Ready:        true,
	}

	// 시나리오 상태 반영 — 수집 중단·격리는 자산 화면에서도 보여야 한다.
	if e.scen.NodeMissing(node.Instance) {
		asset.AgentMissing = true
		asset.Taints = append(asset.Taints, "node.kubernetes.io/unreachable:NoExecute")
	}
	if e.scen.victim != nil && e.scen.victim.Instance == node.Instance && e.scen.isolated() {
		asset.Cordoned = true
		asset.Taints = append(asset.Taints,
			"keiailab.dev/gpu-fault=true:NoSchedule",
			"node.kubernetes.io/unschedulable:NoSchedule")
	}

	faults := e.scen.FaultSet()
	for _, g := range node.GPUs {
		gk := g.Instance + "/" + g.Device
		ga := GPUAsset{
			UUID: g.UUID, Device: g.Device, Model: g.Model,
			Serial:        fmt.Sprintf("%010d", fnvHash(seed, "gpusn", gk)%10_000_000_000),
			VBIOS:         fmt.Sprintf("96.00.%02X.00.%02X", fnvHash(seed, "vbios", gk)%60, fnvHash(seed, "vbios2", gk)%99),
			DriverVersion: dm.Driver, CudaVersion: dm.CUDA,
			MigProfile:   migProfileOf(seed, gk, g.Model),
			PcieGen:      pcieGenOf(g.Model),
			PcieWidth:    "x16",
			NvlinkLinks:  nvlinkCountOf(g.Model),
			NvlinkStatus: "정상",
			Pool:         g.Pool, Tenant: g.Tenant, Allocated: g.Allocated,
		}
		if _, bad := faults[g.UUID]; bad {
			ga.NvlinkStatus = "저하 — 링크 오류 감지"
		}
		if g.Allocated {
			ga.Pod = fmt.Sprintf("%s-%04x", g.Pool, fnvHash(seed, "pod", gk)%0xffff)
		}
		asset.GPUs = append(asset.GPUs, ga)
	}
	return asset
}

// migProfileOf — MIG 는 일부 A100/H100 에서만 켠다(추론 다중 테넌트 목적).
func migProfileOf(seed int64, key, model string) string {
	if model == "NVIDIA L40S" || model == "NVIDIA B200" {
		return "비활성"
	}
	switch r := unitFloat(fnvHash(seed, "mig", key)); {
	case r < 0.08:
		return "1g.10gb × 7"
	case r < 0.14:
		return "2g.20gb × 3"
	case r < 0.18:
		return "3g.40gb × 2"
	default:
		return "비활성"
	}
}

func pcieGenOf(model string) string {
	if model == "NVIDIA B200" {
		return "Gen5"
	}
	return "Gen4"
}

func nvlinkCountOf(model string) int {
	switch model {
	case "NVIDIA B200":
		return 18
	case "NVIDIA H100":
		return 18
	case "NVIDIA A100":
		return 12
	default:
		return 0 // L40S 는 NVLink 미탑재
	}
}

func shortModel(model string) string {
	switch model {
	case "NVIDIA H100":
		return "NVIDIA-H100-80GB-HBM3"
	case "NVIDIA A100":
		return "NVIDIA-A100-SXM4-80GB"
	case "NVIDIA L40S":
		return "NVIDIA-L40S"
	case "NVIDIA B200":
		return "NVIDIA-B200-192GB-HBM3e"
	}
	return model
}

func upperCSP(csp string) string {
	out := make([]rune, 0, len(csp))
	for _, r := range csp {
		if r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		out = append(out, r)
	}
	return string(out)
}
