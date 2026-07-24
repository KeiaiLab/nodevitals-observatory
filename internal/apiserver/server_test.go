// server_test.go 는 M2 조회 핸들러(query/series/labels/healthz/readyz) 회귀 +
// M4 인증 배선(auth.Middleware 로 보호된 API + 공개 예외 경로)을 검증한다.
// m4-design.md §6 계약: "기존 assert 무수정 통과(M2 회귀 0 증명)" — 아래 M2
// 테스트들은 원래 단정문을 그대로 두고 NewServer 시그니처 변경분(3번째 인자
// *auth.Authenticator)과 로그인 배선만 추가했다. auth_integration 계열 테스트는
// (대상 파일이 server.go/server_test.go 2개로 제한돼) 별도 파일 대신 본 파일
// 하단에 통합했다.
package apiserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/auth"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// newTestDB 는 임시 디렉터리에 tsdb 를 열고 t.Cleanup 으로 Close 를 예약한다.
func newTestDB(t *testing.T) *tsdb.DB {
	t.Helper()
	db, err := tsdb.Open(tsdb.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("tsdb.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	return db
}

func secParam(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
}

// ---- M4 인증 테스트 헬퍼 (m4-design.md §6) ----
//
// M2 부터 있던 조회 핸들러가 M4 부터 auth.Middleware 뒤로 보호되므로, 기존
// 테스트는 로그인 → 쿠키 획득 → 요청에 실어 보내는 절차가 필요하다.

const (
	testAdminUser = "admin"
	// 테스트 전용 평문 — 운영 크리덴셜이 아니다(어떤 로그·출력에도 실리지 않는다).
	testAdminPass = "observatory-test-only-pw-4f2b9c"
)

// newTestAuthenticator 는 결정론 시계를 주입한 테스트용 Authenticator 를 만든다
// (D4 — 세션 만료가 벽시계에 좌우되지 않게 한다).
func newTestAuthenticator(t *testing.T) *auth.Authenticator {
	t.Helper()
	a, err := auth.NewAuthenticator(testAdminUser, testAdminPass,
		auth.WithNow(func() time.Time { return time.Unix(1_700_000_000, 0) }))
	if err != nil {
		t.Fatalf("auth.NewAuthenticator: %v", err)
	}
	return a
}

// loginForTest 는 srv 에 관리자 크리덴셜로 로그인해 세션 쿠키를 반환한다.
// 실패하면 즉시 t.Fatalf 로 테스트를 중단한다.
func loginForTest(t *testing.T, srv *httptest.Server) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": testAdminUser, "password": testAdminPass})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/auth/login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("로그인 응답에 %s 쿠키가 없다", auth.CookieName)
	return nil
}

// getWithCookie 는 cookie(nil 허용 — 무인증 요청 시뮬레이션)를 실어 GET 을
// 보낸다. 기존 http.Get 호출부를 인증 경유로 바꾸기 위한 최소 헬퍼.
func getWithCookie(t *testing.T, rawURL string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	return resp
}

func TestQuery_instant_최신값과_스키마(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000) // 고정 ms epoch — 결정론

	lset := tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m", "k": "v"})
	outside := tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "outside_only", "k": "v"})

	// 창 = [t0-5m, t0]. 창 안에 2개(오래된 값 1, 최신 값 2) — 최신만 나와야 한다.
	if err := db.Append(lset, t0-4*60_000, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := db.Append(lset, t0-1*60_000, 2); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// 창 밖(6분 전, 5분 경계 초과) — 이 메트릭만 있는 시리즈는 결과 0건이어야 한다.
	if err := db.Append(outside, t0-6*60_000, 42); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := db.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	timeParam := secParam(t0)

	resp := getWithCookie(t, srv.URL+"/api/v1/query?query="+url.QueryEscape(`m{k="v"}`)+"&time="+timeParam, cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Status != "success" {
		t.Fatalf("status 필드 = %q, want success", body.Status)
	}
	if body.Data.ResultType != "vector" {
		t.Fatalf("resultType = %q, want vector", body.Data.ResultType)
	}
	if len(body.Data.Result) != 1 {
		t.Fatalf("result 길이 = %d, want 1: %+v", len(body.Data.Result), body.Data.Result)
	}

	r := body.Data.Result[0]
	wantEvalSec := float64(t0) / 1000
	gotEvalSec, ok := r.Value[0].(float64)
	if !ok || gotEvalSec != wantEvalSec {
		t.Fatalf("value[0] = %v, want %v(평가시각 — 샘플 시각이 아니다)", r.Value[0], wantEvalSec)
	}
	gotVal, ok := r.Value[1].(string)
	if !ok || gotVal != "2" {
		t.Fatalf("value[1] = %v, want \"2\"(최신 샘플 — 첫 샘플 \"1\" 이면 안 된다)", r.Value[1])
	}
	if r.Metric["k"] != "v" || r.Metric[tsdb.MetricName] != "m" {
		t.Fatalf("metric 라벨 불일치: %+v", r.Metric)
	}

	// 창 밖 시리즈만 있는 메트릭 — result 0건.
	resp2 := getWithCookie(t, srv.URL+"/api/v1/query?query="+url.QueryEscape(`outside_only{k="v"}`)+"&time="+timeParam, cookie)
	defer resp2.Body.Close()
	var body2 struct {
		Data struct {
			Result []any `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body2.Data.Result) != 0 {
		t.Fatalf("outside_only result 길이 = %d, want 0(5분 창 밖이라 배제돼야 한다)", len(body2.Data.Result))
	}
}

func TestQuery_selector_파서_경계(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)

	// 값에 리터럴 `"` 를 담아 이스케이프 복원을 검증한다: 저장값 = a"b
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m", "k": `a"b`}), t0, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)
	timeParam := secParam(t0)

	cases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"이스케이프_매칭성공", `m{k="a\"b"}`, http.StatusOK},
		{"정규식연산자_거절", `m{k=~"x"}`, http.StatusBadRequest},
		{"부정연산자_거절", `m{k!="x"}`, http.StatusBadRequest},
		{"부정정규식연산자_거절", `m{k!~"x"}`, http.StatusBadRequest},
		{"라벨블록_미닫힘", `m{k="v"`, http.StatusBadRequest},
		{"빈셀렉터_거절", ``, http.StatusBadRequest},
		{"빈중괄호만_거절", `{}`, http.StatusBadRequest},
		{"닫힘뒤_불필요문자", `m{k="v"}extra`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := getWithCookie(t, srv.URL+"/api/v1/query?query="+url.QueryEscape(tc.query)+"&time="+timeParam, cookie)
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			var body struct {
				Status    string `json:"status"`
				ErrorType string `json:"errorType"`
				Data      struct {
					Result []any `json:"result"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if tc.wantStatus == http.StatusOK {
				if body.Status != "success" || len(body.Data.Result) != 1 {
					t.Fatalf("성공 케이스인데 응답이 다르다: %+v", body)
				}
			} else {
				if body.Status != "error" || body.ErrorType != "bad_data" {
					t.Fatalf("실패 케이스인데 status/errorType 이 다르다: %+v", body)
				}
			}
		})
	}
}

func TestQuery_time_비유한값과_형식오류_거절(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	// NaN/Inf 는 strconv.ParseFloat 자체는 성공시키므로 별도 가드가 없으면
	// 통과해버린다 — 명시 거절 대상. RFC3339 는 M3 미지원 → ParseFloat 실패로
	// 자연히 400 (m2-design.md §5 명시 요구사항).
	for _, tv := range []string{"NaN", "Inf", "-Inf", "not-a-number", "2026-07-24T00:00:00Z"} {
		t.Run(tv, func(t *testing.T) {
			resp := getWithCookie(t, srv.URL+"/api/v1/query?query=m&time="+url.QueryEscape(tv), cookie)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("time=%q 인데 status = %d, want 400", tv, resp.StatusCode)
			}
		})
	}
}

func TestSeries_dedup_과_match필수(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)

	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m1", "k": "v"}), t0, 1); err != nil {
		t.Fatalf("Append m1: %v", err)
	}
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m2", "k": "v"}), t0, 2); err != nil {
		t.Fatalf("Append m2: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	// match[]=m1 (이름 전용 형태) 과 match[]={k="v"} (이름 생략 형태) 은 m1 에서
	// 겹친다 — dedup 없으면 3건(m1 중복 + m2)이 나온다.
	reqURL := srv.URL + "/api/v1/series?match[]=" + url.QueryEscape("m1") +
		"&match[]=" + url.QueryEscape(`{k="v"}`) +
		"&start=" + secParam(t0-60_000) + "&end=" + secParam(t0+60_000)

	resp := getWithCookie(t, reqURL, cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("series 개수 = %d, want 2(m1 이 중복되면 3 이 나온다): %+v", len(body.Data), body.Data)
	}
	names := map[string]bool{}
	for _, m := range body.Data {
		names[m[tsdb.MetricName]] = true
	}
	if !names["m1"] || !names["m2"] {
		t.Fatalf("m1/m2 가 모두 있어야 한다: %+v", body.Data)
	}

	// match[] 부재 → 400.
	resp2 := getWithCookie(t, srv.URL+"/api/v1/series", cookie)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("match[] 부재인데 status = %d, want 400", resp2.StatusCode)
	}
}

func TestReadyz_반영(t *testing.T) {
	db := newTestDB(t)

	// /healthz·/readyz 는 M4 에서도 공개 예외라 로그인 없이 그대로 검증한다
	// (m4-design.md §3 — 인증 예외 경로).
	t.Run("false_이면_503", func(t *testing.T) {
		srv := httptest.NewServer(NewServer(db, func() bool { return false }, newTestAuthenticator(t)))
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		gotBody, _ := io.ReadAll(resp.Body)
		if string(gotBody) != "not ready" {
			t.Fatalf("body = %q, want %q", gotBody, "not ready")
		}

		// healthz 는 readyz 상태와 무관하게 항상 200 이어야 한다(readyz 를
		// 복붙한 구현이면 여기서 503 이 나와 실패한다).
		hresp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer hresp.Body.Close()
		if hresp.StatusCode != http.StatusOK {
			t.Fatalf("healthz status = %d, want 200(readyz=false 와 무관)", hresp.StatusCode)
		}
		hBody, _ := io.ReadAll(hresp.Body)
		if string(hBody) != "ok" {
			t.Fatalf("healthz body = %q, want %q", hBody, "ok")
		}
	})

	t.Run("true_이면_200", func(t *testing.T) {
		srv := httptest.NewServer(NewServer(db, func() bool { return true }, newTestAuthenticator(t)))
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		gotBody, _ := io.ReadAll(resp.Body)
		if string(gotBody) != "ok" {
			t.Fatalf("body = %q, want %q", gotBody, "ok")
		}
	})

	t.Run("nil_이면_항상_200", func(t *testing.T) {
		srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/readyz")
		if err != nil {
			t.Fatalf("GET /readyz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200(ready=nil 이면 항상 true 계약)", resp.StatusCode)
		}
	})
}

// TestLabels_시간창_반영 은 adversarial 리뷰 지적사항(§5, "Querier 시간창
// 미지정")을 검증한다: head 는 시간 필터가 없어(tsdb.Head.Select 계약) 항상
// 포함되지만, Compact 로 블록에 굳은 데이터는 start/end 가 블록의 [minTime,
// maxTime] 밖이면 db.Querier 의 블록 선별에서 제외돼야 한다. start/end 가
// db.Querier 로 제대로 전달되지 않으면(예: 하드코딩된 [0, math.MaxInt64]) 이
// 테스트가 실패한다.
func TestLabels_시간창_반영(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)
	const twoDays = int64(2 * 24 * 3600_000)

	// t0 시점 샘플을 Compact 로 블록에 굳힌다 — meta.MinTime=meta.MaxTime=t0.
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m_old", "old_only": "1"}), t0, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := db.Compact(t0); err != nil { // now=t0 → age 0, RawRetention(7일) 안전하게 미만
		t.Fatalf("Compact: %v", err)
	}

	// 2일 뒤 샘플은 Compact 이후 head 에만 존재.
	t1 := t0 + twoDays
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m_new", "recent_only": "1"}), t1, 2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	hasLabel := func(data []string, name string) bool {
		for _, d := range data {
			if d == name {
				return true
			}
		}
		return false
	}

	// t1 주변 1시간 창만 요청 — t0 블록(2일 전)은 창 밖이라 배제돼야 한다.
	narrowURL := srv.URL + "/api/v1/labels?start=" + secParam(t1-3600_000) + "&end=" + secParam(t1)
	resp := getWithCookie(t, narrowURL, cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var narrow struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&narrow); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hasLabel(narrow.Data, "recent_only") {
		t.Fatalf("recent_only(head) 는 창과 무관하게 있어야 한다: %v", narrow.Data)
	}
	if hasLabel(narrow.Data, "old_only") {
		t.Fatalf("old_only(2일 전 블록) 는 좁은 창에서 배제돼야 한다: %v", narrow.Data)
	}

	// t0 이전부터 t1 까지 넓은 창 — 블록이 포함돼 old_only 도 나와야 한다.
	wideURL := srv.URL + "/api/v1/labels?start=" + secParam(t0-3600_000) + "&end=" + secParam(t1)
	resp2 := getWithCookie(t, wideURL, cookie)
	defer resp2.Body.Close()
	var wide struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&wide); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hasLabel(wide.Data, "old_only") {
		t.Fatalf("넓은 창에서는 old_only(블록) 가 있어야 한다: %v", wide.Data)
	}
	if !hasLabel(wide.Data, "recent_only") {
		t.Fatalf("넓은 창에서도 recent_only(head) 가 있어야 한다: %v", wide.Data)
	}

	// start/end 형식 오류 → 400.
	resp3 := getWithCookie(t, srv.URL+"/api/v1/labels?start=not-a-number", cookie)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Fatalf("start 형식 오류인데 status = %d, want 400", resp3.StatusCode)
	}
}

func TestHealthz_기본(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	gotBody, _ := io.ReadAll(resp.Body)
	if string(gotBody) != "ok" {
		t.Fatalf("body = %q, want %q", gotBody, "ok")
	}
}

// ==== M4: 인증 배선 통합 테스트 (m4-design.md §6) ====
//
// 설계는 이 계열을 apiserver/auth_integration_test.go 로 분리하지만, 본 태스크의
// 대상 파일이 server.go/server_test.go 2개로 고정돼 있어 같은 파일에 통합한다.

// TestAuth_E2E_로그인_쿠키로_보호된API_왕복_로그아웃후_재차단 은 m4-design.md §6
// 의 E2E 왕복 계약을 검증한다: 쿠키 없이 401(M2 에러 스키마 그대로) → 로그인 →
// 쿠키로 200(M2 성공 스키마 그대로) → 로그아웃 → 동일 쿠키로 재요청하면 다시
// 401. "로그인 성공만 확인하고 세션 무효화는 확인 안 함" 함정을 피하려고
// 4단계를 한 흐름으로 묶는다(단계 중 하나라도 생략하면 동어반복 검증이 된다).
func TestAuth_E2E_로그인_쿠키로_보호된API_왕복_로그아웃후_재차단(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m"}), t0, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := db.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	queryURL := srv.URL + "/api/v1/query?query=m&time=" + secParam(t0)

	// 1) 쿠키 없이 401 + M2 에러 JSON 포맷(errorType=unauthorized).
	respNoAuth := getWithCookie(t, queryURL, nil)
	defer respNoAuth.Body.Close()
	if respNoAuth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("무인증 status = %d, want 401", respNoAuth.StatusCode)
	}
	var errBody struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
	}
	if err := json.NewDecoder(respNoAuth.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errBody.Status != "error" || errBody.ErrorType != "unauthorized" {
		t.Fatalf("무인증 에러 바디 = %+v, want status=error errorType=unauthorized", errBody)
	}

	// 2) 로그인 → 쿠키 → 200(M2 응답 스키마 유지 확인).
	cookie := loginForTest(t, srv)
	respAuth := getWithCookie(t, queryURL, cookie)
	defer respAuth.Body.Close()
	if respAuth.StatusCode != http.StatusOK {
		t.Fatalf("인증됨 status = %d, want 200", respAuth.StatusCode)
	}
	var okBody struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
		} `json:"data"`
	}
	if err := json.NewDecoder(respAuth.Body).Decode(&okBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if okBody.Status != "success" || okBody.Data.ResultType != "vector" {
		t.Fatalf("인증됨 바디 = %+v, want status=success resultType=vector", okBody)
	}

	// 3) 로그아웃 → 세션 즉시 무효화.
	logoutReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatalf("NewRequest logout: %v", err)
	}
	logoutReq.AddCookie(cookie)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", logoutResp.StatusCode)
	}

	// 4) 동일 쿠키로 재요청 → 다시 401(세션이 실제로 삭제됐는지 — 로그아웃이
	// 쿠키만 지우고 서버측 세션을 안 지우면 이 단계가 200 으로 새 나간다).
	respAfterLogout := getWithCookie(t, queryURL, cookie)
	defer respAfterLogout.Body.Close()
	if respAfterLogout.StatusCode != http.StatusUnauthorized {
		t.Fatalf("로그아웃 후 status = %d, want 401", respAfterLogout.StatusCode)
	}
}

// TestAuth_공개경로는_무인증으로_통과 는 /healthz·/readyz·GET /(콘솔 정적 자산)
// 가 쿠키 없이도 200 인지 확인한다(m4-design.md §3 공개 예외 경로).
func TestAuth_공개경로는_무인증으로_통과(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz", "/"} {
		resp := getWithCookie(t, srv.URL+path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s 무인증 status = %d, want 200", path, resp.StatusCode)
		}
	}
}

// TestAuth_시리즈_라벨_보호 는 series/labels 도 query 와 동일하게 미인증 401 임을
// 확인한다 — query 하나만 테스트하고 나머지 2개 보호 배선을 놓치는 함정 방지
// (m4-design.md §3 "보호 = /api/v1/{query,series,labels}").
func TestAuth_시리즈_라벨_보호(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	for _, path := range []string{
		"/api/v1/series?match[]=m",
		"/api/v1/labels",
	} {
		resp := getWithCookie(t, srv.URL+path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s 무인증 status = %d, want 401", path, resp.StatusCode)
		}
	}
}

// TestAuth_잘못된_쿠키값은_401 은 세션에 등록된 적 없는 토큰 문자열을 쿠키로
// 보내면(예: 위조·만료 후 잔존 브라우저 쿠키) 401 로 거절되는지 본다 — "쿠키가
// 있기만 하면 통과"하는 실수(쿠키 존재 여부만 보고 Validate 를 안 부르는 버그)
// 를 잡는다.
func TestAuth_잘못된_쿠키값은_401(t *testing.T) {
	db := newTestDB(t)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	forged := &http.Cookie{Name: auth.CookieName, Value: "forged-token-not-in-session-store"}
	resp := getWithCookie(t, srv.URL+"/api/v1/query?query=m", forged)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("위조 쿠키 status = %d, want 401", resp.StatusCode)
	}
}
