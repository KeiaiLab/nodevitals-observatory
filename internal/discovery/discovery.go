// Package discovery 는 스크레이프 대상(nodevitals 파드)을 찾는다. 두 가지 방법을
// 제공한다 — 고정 목록(Static)과 in-cluster Kubernetes API 폴링(Kubernetes). 둘 다
// client-go 없이 stdlib 만으로 동작한다(외부 의존 0 계약).
package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Target 은 스크레이프 대상 하나다. URL 은 완전한 metrics URL
// (예: http://10.0.7.101:9847/metrics), Name 은 instance 라벨 값.
type Target struct {
	Name string
	URL  string
}

// Discoverer 는 스크레이프 대상 목록을 낸다.
type Discoverer interface {
	Discover(ctx context.Context) ([]Target, error)
}

// ---- (a) static ----

// Static 은 고정된 타겟 목록을 그대로 내는 Discoverer 다.
type Static struct{ targets []Target }

var _ Discoverer = (*Static)(nil)

// NewStatic 은 고정 목록을 그대로 내는 Static 을 만든다. 인자 슬라이스를 복사해
// 보관한다 — 호출자가 이후 원본 슬라이스를 변조해도 내부 상태에 영향 없다.
func NewStatic(targets []Target) *Static {
	cp := make([]Target, len(targets))
	copy(cp, targets)
	return &Static{targets: cp}
}

// Discover 는 생성 시 받은 목록의 복사본을 반환한다(호출자 변조 격리) — 반환된
// 슬라이스를 호출자가 수정해도 다음 Discover 호출 결과에는 영향이 없다.
func (s *Static) Discover(_ context.Context) ([]Target, error) {
	out := make([]Target, len(s.targets))
	copy(out, s.targets)
	return out, nil
}

// ParseStaticTargets 는 콤마 구분 URL 목록("http://a:9847/metrics,http://b:9847/metrics")을
// Target 으로 변환한다. Name = url.Host. 빈 문자열은 빈 목록(에러 아님). 파싱 불가
// URL 이나 scheme/host 가 없는 URL 은 에러.
func ParseStaticTargets(s string) ([]Target, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	out := make([]Target, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u, err := url.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("discovery: 정적 타겟 URL 파싱 실패 %q: %w", p, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("discovery: 정적 타겟 URL 에 scheme 또는 host 가 없다 %q", p)
		}
		out = append(out, Target{Name: u.Host, URL: p})
	}
	return out, nil
}

// ---- (b) kubernetes (in-cluster, stdlib 직접 REST) ----

const (
	defaultBaseURL     = "https://kubernetes.default.svc"
	defaultTokenFile   = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultCAFile      = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	defaultMetricsPath = "/metrics"
)

// KubeConfig 는 Kubernetes 파드 발견 설정이다.
type KubeConfig struct {
	// BaseURL 기본 "https://kubernetes.default.svc". 테스트는 httptest.Server.URL 주입.
	BaseURL string
	// TokenFile 기본 "/var/run/secrets/kubernetes.io/serviceaccount/token".
	TokenFile string
	// CAFile 기본 "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt".
	// Client 가 nil 일 때만 사용해 TLS RootCAs 를 구성한다.
	CAFile string

	Namespace     string // 예: platform-system
	LabelSelector string // 예: app.kubernetes.io/name=nodevitals
	Port          int    // 예: 9847
	MetricsPath   string // 기본 "/metrics"

	// Client 주입 시 CAFile 무시(테스트: httptest 용 평문/자체서명 클라이언트).
	Client *http.Client
}

// Kubernetes 는 in-cluster kube-apiserver REST 호출로 파드를 발견하는 Discoverer 다.
type Kubernetes struct {
	cfg    KubeConfig // 기본값 채운 정규화본(Client 필드는 참고용, 실사용은 client)
	client *http.Client
}

var _ Discoverer = (*Kubernetes)(nil)

// NewKubernetes 는 기본값을 채우고 cfg.Client 가 nil 이면 CAFile 로 TLS pool 을
// 구성한 클라이언트를 만든다. CAFile 읽기·파싱 실패는 여기서 즉시 에러다 —
// Discover 호출 시점까지 미루지 않는다(fail fast).
func NewKubernetes(cfg KubeConfig) (*Kubernetes, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.TokenFile == "" {
		cfg.TokenFile = defaultTokenFile
	}
	if cfg.CAFile == "" {
		cfg.CAFile = defaultCAFile
	}
	if cfg.MetricsPath == "" {
		cfg.MetricsPath = defaultMetricsPath
	}

	client := cfg.Client
	if client == nil {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("discovery: CA 파일 읽기 실패 %q: %w", cfg.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("discovery: CA 파일에 유효한 인증서가 없다 %q", cfg.CAFile)
		}
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			},
		}
	}

	return &Kubernetes{cfg: cfg, client: client}, nil
}

// podList 는 kube-apiserver PodList 응답에서 필요한 필드만 담는 비공개 구조체다
// (client-go/k8s.io/api 금지 계약 — 외부 의존 0).
type podList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase string `json:"phase"`
			PodIP string `json:"podIP"`
		} `json:"status"`
	} `json:"items"`
}

// Discover 는 GET {BaseURL}/api/v1/namespaces/{ns}/pods?labelSelector={sel} 을
// 호출한다. Authorization: Bearer <TokenFile 내용 — 매 호출 재독(토큰 로테이션
// 대응)>. 응답 JSON 에서 status.phase=="Running" && status.podIP!="" 인 파드만
// Target 으로 변환한다: Name = spec.nodeName(hostNetwork 라 노드 식별이 유의미;
// 비면 metadata.name), URL = "http://" + net.JoinHostPort(podIP, port) + MetricsPath.
// 비-200 응답은 상태코드 + 본문 앞 256바이트를 담은 에러다.
func (k *Kubernetes) Discover(ctx context.Context) ([]Target, error) {
	token, err := os.ReadFile(k.cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("discovery: 토큰 파일 읽기 실패 %q: %w", k.cfg.TokenFile, err)
	}

	q := url.Values{}
	q.Set("labelSelector", k.cfg.LabelSelector)
	reqURL := fmt.Sprintf("%s/api/v1/namespaces/%s/pods?%s",
		strings.TrimRight(k.cfg.BaseURL, "/"),
		url.PathEscape(k.cfg.Namespace),
		q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery: 요청 생성 실패: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery: apiserver 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("discovery: 응답 본문 읽기 실패: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := body
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return nil, fmt.Errorf("discovery: apiserver 비정상 응답 %d: %s", resp.StatusCode, snippet)
	}

	var list podList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("discovery: 파드 목록 JSON 파싱 실패: %w", err)
	}

	targets := make([]Target, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Status.Phase != "Running" || item.Status.PodIP == "" {
			continue
		}
		name := item.Spec.NodeName
		if name == "" {
			name = item.Metadata.Name
		}
		targets = append(targets, Target{
			Name: name,
			URL:  "http://" + net.JoinHostPort(item.Status.PodIP, fmt.Sprintf("%d", k.cfg.Port)) + k.cfg.MetricsPath,
		})
	}
	return targets, nil
}
