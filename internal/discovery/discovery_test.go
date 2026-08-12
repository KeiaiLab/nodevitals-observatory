package discovery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- Static ----

func TestStatic_Discover_고정목록을_그대로_낸다(t *testing.T) {
	want := []Target{
		{Name: "e101", URL: "http://10.0.7.101:9847/metrics"},
		{Name: "e102", URL: "http://10.0.7.102:9847/metrics"},
	}
	s := NewStatic(want)

	got, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("타겟 개수: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("타겟[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// NewStatic/Discover 가 슬라이스를 공유하면(복사 생략) 한쪽 변조가 다른 쪽에
// 새어나간다 — 계약("Discover 는 복사본 반환")을 실제로 어기는 구현이면 실패한다.
func TestStatic_Discover는_복사본을_반환한다(t *testing.T) {
	orig := []Target{{Name: "a", URL: "http://a:9847/metrics"}}
	s := NewStatic(orig)
	orig[0].Name = "mutated-after-new-static" // NewStatic 이 원본 슬라이스를 그대로 잡고 있으면 오염

	got1, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got1[0].Name != "a" {
		t.Fatalf("NewStatic 이 입력 슬라이스를 복사하지 않았다: got %q, want %q", got1[0].Name, "a")
	}

	got1[0].Name = "mutated-after-discover" // Discover 반환값이 내부 상태와 공유되면 다음 호출이 오염

	got2, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got2[0].Name != "a" {
		t.Fatalf("Discover 가 내부 상태를 공유 반환했다: got %q, want %q", got2[0].Name, "a")
	}
}

// ---- ParseStaticTargets ----

func TestParseStaticTargets(t *testing.T) {
	got, err := ParseStaticTargets("http://a:9847/metrics,http://b:9847/metrics")
	if err != nil {
		t.Fatalf("정상 입력인데 에러: %v", err)
	}
	want := []Target{
		{Name: "a:9847", URL: "http://a:9847/metrics"},
		{Name: "b:9847", URL: "http://b:9847/metrics"},
	}
	if len(got) != len(want) {
		t.Fatalf("타겟 개수: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("타겟[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}

	if _, err := ParseStaticTargets("://x"); err == nil {
		t.Fatal("깨진 URL(\"://x\")인데 에러가 없다")
	}

	// scheme 은 있으나 host 가 없는 경우 — url.Parse 자체는 에러를 내지 않으므로
	// 별도 Host 검증 분기가 실제로 도달·동작하는지 확인한다.
	if _, err := ParseStaticTargets("http://"); err == nil {
		t.Fatal("host 없는 URL(\"http://\")인데 에러가 없다")
	}

	got, err = ParseStaticTargets("")
	if err != nil {
		t.Fatalf("빈 문자열인데 에러: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("빈 문자열 결과: got %d 개, want 0", len(got))
	}
}

// ---- Kubernetes ----

func writeTokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKubernetes_fake_apiserver_URL과_토큰(t *testing.T) {
	tokenPath := writeTokenFile(t, "test-token-abc")

	var gotAuth, gotPath, gotSelector string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotSelector = r.URL.Query().Get("labelSelector")

		w.Header().Set("Content-Type", "application/json")
		// 5 파드로 필터 조건(phase==Running && podIP!="") 의 양쪽을 독립적으로 검증한다:
		//   pod-a: Running+IP  → 포함, Name=nodeName
		//   pod-b: Running+IP, nodeName 없음 → 포함, Name=metadata.name(대체)
		//   pod-c: Pending, IP 없음 → 제외(둘 다 거짓)
		//   pod-d: Running 이지만 IP 없음 → 제외(podIP 조건 단독 검증)
		//   pod-e: Succeeded 인데 IP 있음 → 제외(phase 조건 단독 검증)
		fmt.Fprint(w, `{"items":[
			{"metadata":{"name":"pod-a"},"spec":{"nodeName":"e101"},"status":{"phase":"Running","podIP":"10.0.7.101"}},
			{"metadata":{"name":"pod-b"},"spec":{},"status":{"phase":"Running","podIP":"10.0.7.102"}},
			{"metadata":{"name":"pod-c"},"spec":{"nodeName":"e103"},"status":{"phase":"Pending","podIP":""}},
			{"metadata":{"name":"pod-d"},"spec":{"nodeName":"e104"},"status":{"phase":"Running","podIP":""}},
			{"metadata":{"name":"pod-e"},"spec":{"nodeName":"e105"},"status":{"phase":"Succeeded","podIP":"10.0.7.105"}}
		]}`)
	}))
	defer srv.Close()

	k, err := NewKubernetes(KubeConfig{
		BaseURL:       srv.URL,
		TokenFile:     tokenPath,
		Namespace:     "platform-system",
		LabelSelector: "app.kubernetes.io/name=nodevitals",
		Port:          9847,
		Client:        srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewKubernetes: %v", err)
	}

	targets, err := k.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if gotAuth != "Bearer test-token-abc" {
		t.Errorf("Authorization 헤더: got %q, want %q", gotAuth, "Bearer test-token-abc")
	}
	if gotPath != "/api/v1/namespaces/platform-system/pods" {
		t.Errorf("요청 경로: got %q", gotPath)
	}
	if gotSelector != "app.kubernetes.io/name=nodevitals" {
		t.Errorf("labelSelector 쿼리: got %q", gotSelector)
	}

	if len(targets) != 2 {
		t.Fatalf("타겟 개수: got %d, want 2 (Running && podIP!=\"\" 인 파드만): %+v", len(targets), targets)
	}

	byName := map[string]string{}
	for _, tg := range targets {
		byName[tg.Name] = tg.URL
	}
	if got, want := byName["e101"], "http://10.0.7.101:9847/metrics"; got != want {
		t.Errorf("e101(nodeName) 타겟 URL: got %q, want %q", got, want)
	}
	if got, want := byName["pod-b"], "http://10.0.7.102:9847/metrics"; got != want {
		t.Errorf("pod-b(nodeName 없음 → metadata.name 대체) 타겟 URL: got %q, want %q", got, want)
	}
	for _, excluded := range []string{"e103", "e104", "e105", "pod-d", "pod-e"} {
		if _, ok := byName[excluded]; ok {
			t.Errorf("제외되어야 할 파드 %q 가 결과에 포함되었다: %+v", excluded, targets)
		}
	}
}

func TestKubernetes_비200은_에러(t *testing.T) {
	tokenPath := writeTokenFile(t, "tok")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden: no rbac")
	}))
	defer srv.Close()

	k, err := NewKubernetes(KubeConfig{
		BaseURL:   srv.URL,
		TokenFile: tokenPath,
		Namespace: "platform-system",
		Port:      9847,
		Client:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewKubernetes: %v", err)
	}

	targets, err := k.Discover(context.Background())
	if err == nil {
		t.Fatalf("Discover 가 nil 에러를 반환했다(403 응답인데): targets=%+v", targets)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("에러 메시지에 상태코드가 없다: %v", err)
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("에러 메시지에 응답 본문이 없다: %v", err)
	}
}

// 본문이 256바이트를 넘으면 잘려야 한다는 계약(§3) — 자르기 로직이 없는
// 구현이면(snippet := body 로 캡을 생략) 마커가 그대로 노출되어 실패한다.
func TestKubernetes_비200_본문은_256바이트로_잘린다(t *testing.T) {
	tokenPath := writeTokenFile(t, "tok")

	long := strings.Repeat("x", 300) + "TAIL-MARKER"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, long)
	}))
	defer srv.Close()

	k, err := NewKubernetes(KubeConfig{BaseURL: srv.URL, TokenFile: tokenPath, Client: srv.Client()})
	if err != nil {
		t.Fatalf("NewKubernetes: %v", err)
	}

	_, err = k.Discover(context.Background())
	if err == nil {
		t.Fatal("500 응답인데 에러가 없다")
	}
	if strings.Contains(err.Error(), "TAIL-MARKER") {
		t.Fatalf("본문이 256바이트로 잘리지 않았다(300바이트 뒤 마커가 노출됨): %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("상태코드가 메시지에 없다: %v", err)
	}
}

// Discover 는 매 호출마다 토큰 파일을 재독해야 한다(로테이션 대응) — 캐시로
// 퇴화한 구현이면 2차 요청도 구 토큰을 보내 이 테스트가 실패한다.
func TestKubernetes_토큰_재독(t *testing.T) {
	tokenPath := writeTokenFile(t, "token-v1")

	var gotAuths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuths = append(gotAuths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":[]}`)
	}))
	defer srv.Close()

	k, err := NewKubernetes(KubeConfig{
		BaseURL:   srv.URL,
		TokenFile: tokenPath,
		Namespace: "platform-system",
		Port:      9847,
		Client:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewKubernetes: %v", err)
	}

	if _, err := k.Discover(context.Background()); err != nil {
		t.Fatalf("1차 Discover: %v", err)
	}

	if err := os.WriteFile(tokenPath, []byte("token-v2"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := k.Discover(context.Background()); err != nil {
		t.Fatalf("2차 Discover: %v", err)
	}

	if len(gotAuths) != 2 {
		t.Fatalf("요청 횟수: got %d, want 2 (%v)", len(gotAuths), gotAuths)
	}
	if gotAuths[0] != "Bearer token-v1" {
		t.Errorf("1차 요청 토큰: got %q, want %q", gotAuths[0], "Bearer token-v1")
	}
	if gotAuths[1] != "Bearer token-v2" {
		t.Errorf("2차 요청 토큰(재독 되었어야 함): got %q, want %q", gotAuths[1], "Bearer token-v2")
	}
}

// NewKubernetes 는 CAFile 읽기 실패를 Discover 시점까지 미루지 않고 즉시 반환해야
// 한다(fail fast 계약, §3).
func TestNewKubernetes_CA파일_읽기실패는_즉시_에러(t *testing.T) {
	_, err := NewKubernetes(KubeConfig{
		CAFile: filepath.Join(t.TempDir(), "no-such-ca.crt"),
	})
	if err == nil {
		t.Fatal("존재하지 않는 CAFile 인데 NewKubernetes 가 에러를 내지 않았다")
	}
}

// Client 가 주입되면 CAFile 은 무시되어야 한다(§3) — 무시하지 않는 구현이면
// 존재하지 않는 CAFile 경로 때문에 이 테스트가 실패한다.
func TestNewKubernetes_Client_주입시_CAFile_무시(t *testing.T) {
	k, err := NewKubernetes(KubeConfig{
		CAFile: filepath.Join(t.TempDir(), "no-such-ca.crt"),
		Client: &http.Client{},
	})
	if err != nil {
		t.Fatalf("Client 가 주입되었는데도 CAFile 읽기를 시도해 에러가 났다: %v", err)
	}
	if k == nil {
		t.Fatal("반환된 *Kubernetes 가 nil")
	}
}
