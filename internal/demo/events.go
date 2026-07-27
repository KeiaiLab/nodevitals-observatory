package demo

// K8s 이벤트 스트림 — 알림(Alert)이 "이상"을 말한다면 이벤트는 "평범한 일상"을
// 말한다. 실제 클러스터 콘솔에는 파드 스케줄·이미지 풀·재시작·프로브 실패가
// 끊임없이 흐르고, 그 흐름이 없으면 화면이 정지한 모형처럼 보인다.
//
// 알림과 분리한 이유: 이벤트는 대개 조치 대상이 아니다. 둘을 섞으면 진짜
// 경보가 일상 로그에 묻힌다(실 운영에서 반복되는 실패).

import (
	"fmt"
	"strconv"
)

// ClusterEvent 는 K8s 이벤트 1건이다.
type ClusterEvent struct {
	At        int64  `json:"at"`
	Type      string `json:"type"`   // Normal | Warning
	Reason    string `json:"reason"` // Scheduled, Pulled, Started, ...
	Object    string `json:"object"` // pod/xxx, node/xxx
	Namespace string `json:"namespace,omitempty"`
	Node      string `json:"node,omitempty"`
	Message   string `json:"message"`
}

const maxEvents = 60

type eventKind struct {
	typ, reason string
	weight      float64
	message     func(pool, pod, node string) string
}

// 실제 클러스터 이벤트 분포에 가깝게 — 스케줄·이미지·시작이 대다수,
// 프로브 실패·축출·OOM 은 드물다.
var eventKinds = []eventKind{
	{"Normal", "Scheduled", 0.26, func(pool, pod, node string) string {
		return fmt.Sprintf("%s 를 %s 에 배치", pod, node)
	}},
	{"Normal", "Pulled", 0.18, func(pool, pod, _ string) string {
		return fmt.Sprintf("이미지 준비 완료 — harbor.keiailab.dev/serving/%s:v1", pool)
	}},
	{"Normal", "Started", 0.16, func(_, pod, _ string) string {
		return fmt.Sprintf("컨테이너 시작 — %s", pod)
	}},
	{"Normal", "Killing", 0.10, func(_, pod, _ string) string {
		return fmt.Sprintf("롤링 업데이트로 이전 파드 종료 — %s", pod)
	}},
	{"Normal", "ScalingReplicaSet", 0.08, func(pool, _, _ string) string {
		return fmt.Sprintf("HPA 스케일 조정 — %s 레플리카 변경", pool)
	}},
	{"Warning", "Unhealthy", 0.09, func(_, pod, _ string) string {
		return fmt.Sprintf("Readiness 프로브 실패 (HTTP 503) — %s", pod)
	}},
	{"Warning", "BackOff", 0.05, func(_, pod, _ string) string {
		return fmt.Sprintf("컨테이너 재시작 백오프 — %s", pod)
	}},
	{"Warning", "FailedScheduling", 0.05, func(pool, _, _ string) string {
		return fmt.Sprintf("배치 가능한 GPU 노드 없음 — %s 대기 중", pool)
	}},
	{"Warning", "OOMKilling", 0.03, func(_, pod, _ string) string {
		return fmt.Sprintf("컨테이너 메모리 한도 초과로 종료 — %s", pod)
	}},
}

func pickEventKind(r float64) eventKind {
	acc := 0.0
	for _, k := range eventKinds {
		acc += k.weight
		if r < acc {
			return k
		}
	}
	return eventKinds[0]
}

func (s *Scenario) pushEvent(e ClusterEvent) {
	s.events = append(s.events, e)
	if len(s.events) > maxEvents {
		s.events = s.events[len(s.events)-maxEvents:]
	}
}

// fireEvents 는 15s 틱마다 0~2건의 일상 이벤트를 낸다. 발생량은 일간 리듬을
// 따른다 — 업무시간에 배포·스케일이 잦고 새벽엔 잠잠하다.
func (s *Scenario) fireEvents(tMS int64) {
	tick := tMS / 15_000
	if tick == s.eventTick {
		return
	}
	s.eventTick = tick

	// 기준 0.55건/틱에 일간 리듬을 실어 0.2~1.2건 사이로 움직인다.
	rate := 0.55 * (1 + diurnalFactor(tMS)*1.6)
	n := 0
	for i := 0; i < 2; i++ {
		if unitFloat(fnvHash(s.cfg.Seed, "evrate", strconv.FormatInt(tick, 10), strconv.Itoa(i))) < rate/2 {
			n++
		}
	}
	for i := 0; i < n; i++ {
		s.pushEvent(s.makeEvent(tMS, tick, i))
	}
}

func (s *Scenario) makeEvent(tMS, tick int64, i int) ClusterEvent {
	salt := strconv.FormatInt(tick, 10) + ":" + strconv.Itoa(i)
	g := s.fleet.GPUs[int(fnvHash(s.cfg.Seed, "evgpu", salt)%uint64(len(s.fleet.GPUs)))]
	pool := g.Pool
	if pool == "" {
		pool = "llm-serving-a"
	}
	pod := fmt.Sprintf("%s-%04x-%03x", pool,
		fnvHash(s.cfg.Seed, "evpod", salt)%0xffff, fnvHash(s.cfg.Seed, "evpod2", salt)%0xfff)
	k := pickEventKind(unitFloat(fnvHash(s.cfg.Seed, "evkind", salt)))

	ns := "serving-" + pool
	return ClusterEvent{
		At: tMS, Type: k.typ, Reason: k.reason,
		Object: "pod/" + pod, Namespace: ns, Node: g.Instance,
		Message: k.message(pool, pod, g.Instance),
	}
}

// seedEvents 는 부팅 직후 이벤트 목록이 비어 보이지 않게 최근 이력을 만든다.
// 과거 시각으로 소급 생성해 "방금 켠 화면"이 아니라 "계속 돌던 클러스터"로
// 보이게 한다.
func (s *Scenario) seedEvents(nowMS int64) {
	for i := 24; i >= 1; i-- {
		t := nowMS - int64(i)*45_000
		s.pushEvent(s.makeEvent(t, t/15_000, 0))
	}
}
