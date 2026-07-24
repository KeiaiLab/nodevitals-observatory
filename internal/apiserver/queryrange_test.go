// queryrange_test.go 는 M5 /api/v1/query_range(m5-design.md §3, M3-lite)를
// 검증한다. 헬퍼(newTestDB/secParam/newTestAuthenticator/loginForTest/
// getWithCookie)는 server_test.go 에 정의돼 있다 — 같은 패키지라 재사용한다.
package apiserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// TestQueryRange_버킷값과_스키마 는 정상 경로를 검증한다: carry-in 샘플(start
// 이전, lookback 안) → bucket0 값으로 이월 / 버킷 경계에 정확히 찍힌 샘플 →
// 그 버킷 값 / 두 버킷 사이 샘플 → 다음 버킷 값. 기대값은 산식이 아니라
// 리터럴로 하드코딩한다(동어반복 금지).
func TestQueryRange_버킷값과_스키마(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000) // 고정 ms epoch — 결정론
	const stepMS = int64(60_000)        // 60s

	lset := tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m", "k": "v"})

	// carry-in: start 이전 샘플(5m lookback 안) — bucket0(t0) 값으로 이월돼야 한다.
	if err := db.Append(lset, t0-30_000, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// bucket1(t0+60s) 경계에 정확히 찍힌 샘플.
	if err := db.Append(lset, t0+stepMS, 2); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// bucket1 과 bucket2 사이 — bucket2(t0+120s) 값으로 쓰여야 한다.
	if err := db.Append(lset, t0+stepMS+30_000, 3); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := db.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	reqURL := srv.URL + "/api/v1/query_range?query=" + url.QueryEscape(`m{k="v"}`) +
		"&start=" + secParam(t0) + "&end=" + secParam(t0+2*stepMS) + "&step=60"

	resp := getWithCookie(t, reqURL, cookie)
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
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Status != "success" {
		t.Fatalf("status 필드 = %q, want success", body.Status)
	}
	if body.Data.ResultType != "matrix" {
		t.Fatalf("resultType = %q, want matrix", body.Data.ResultType)
	}
	if len(body.Data.Result) != 1 {
		t.Fatalf("result 길이 = %d, want 1: %+v", len(body.Data.Result), body.Data.Result)
	}

	r := body.Data.Result[0]
	if r.Metric["k"] != "v" || r.Metric[tsdb.MetricName] != "m" {
		t.Fatalf("metric 라벨 불일치: %+v", r.Metric)
	}
	if len(r.Values) != 3 {
		t.Fatalf("values 길이 = %d, want 3: %+v", len(r.Values), r.Values)
	}

	wantTS := []float64{float64(t0) / 1000, float64(t0+stepMS) / 1000, float64(t0+2*stepMS) / 1000}
	wantVal := []string{"1", "2", "3"}
	for i, wantT := range wantTS {
		gotTS, ok := r.Values[i][0].(float64)
		if !ok || gotTS != wantT {
			t.Fatalf("values[%d][0] = %v, want %v", i, r.Values[i][0], wantT)
		}
		gotVal, ok := r.Values[i][1].(string)
		if !ok || gotVal != wantVal[i] {
			t.Fatalf("values[%d][1] = %v, want %q", i, r.Values[i][1], wantVal[i])
		}
	}
}

// TestQueryRange_lookback_경계 는 (t-lookback, t] 열린-시작 구간 계약을
// 검증한다: t0-5m 정확히 그 시각의 샘플은 배제되고, 1ms 라도 안쪽이면
// 포함돼야 한다. 두 케이스를 서로 다른 시리즈/DB 로 분리해 "더 최신 샘플이
// 가려서 통과"하는 오탐을 막는다.
func TestQueryRange_lookback_경계(t *testing.T) {
	const t0 = int64(1_700_000_000_000)
	const lookbackMS = int64(5 * 60_000) // queryWindow(5m) 재사용 계약

	t.Run("정확히_경계는_제외", func(t *testing.T) {
		db := newTestDB(t)
		lset := tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m_edge"})
		if err := db.Append(lset, t0-lookbackMS, 99); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := db.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}

		srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
		defer srv.Close()
		cookie := loginForTest(t, srv)

		reqURL := srv.URL + "/api/v1/query_range?query=m_edge&start=" + secParam(t0) +
			"&end=" + secParam(t0) + "&step=60"
		resp := getWithCookie(t, reqURL, cookie)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var out struct {
			Data struct {
				Result []any `json:"result"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Data.Result) != 0 {
			t.Fatalf("result 길이 = %d, want 0(t-5m 정확히 경계 샘플은 배제돼야 한다): %+v",
				len(out.Data.Result), out.Data.Result)
		}
	})

	t.Run("경계_바로_안쪽은_포함", func(t *testing.T) {
		db := newTestDB(t)
		lset := tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m_inside"})
		if err := db.Append(lset, t0-lookbackMS+1, 7); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := db.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}

		srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
		defer srv.Close()
		cookie := loginForTest(t, srv)

		reqURL := srv.URL + "/api/v1/query_range?query=m_inside&start=" + secParam(t0) +
			"&end=" + secParam(t0) + "&step=60"
		resp := getWithCookie(t, reqURL, cookie)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var out struct {
			Data struct {
				Result []struct {
					Values [][2]any `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Data.Result) != 1 || len(out.Data.Result[0].Values) != 1 {
			t.Fatalf("경계 바로 안쪽 샘플이 포함되지 않았다: %+v", out.Data.Result)
		}
		gotVal, ok := out.Data.Result[0].Values[0][1].(string)
		if !ok || gotVal != "7" {
			t.Fatalf("values[0][1] = %v, want \"7\"", out.Data.Result[0].Values[0][1])
		}
	})
}

// TestQueryRange_에러_경로 는 §3.5-3 의 에러 케이스 전부를 실제로 실행한다
// (선언만 하고 안 돌리는 함정 방지). step_서브밀리초 케이스는 적대검토 F2
// (int64(0.0004*1000)==0 절삭 → 0-증분 무한루프) 회귀를 직접 잡는다 — 가드가
// "초 값 > 0" 에 걸려 있으면 이 요청이 타임아웃될 때까지 걸린다.
// start_end_차이_int64_오버플로우_우회 케이스는 별도 적대검토(중요도 important)
// 회귀를 잡는다 — start/end 가 각각은 유효 범위(±9e15초)라 파싱은 통과하지만
// endMS-startMS 가 int64 를 오버플로우해 음수가 되면 maxRangePoints 가드를
// 우회하고 bucketSeries 가 그 폭을 순회하는 DoS 로 이어진다. 가드가 float64
// 로 계산돼 있지 않으면 이 케이스가 200(또는 타임아웃)으로 실패한다.
func TestQueryRange_에러_경로(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)
	if err := db.Append(tsdb.LabelsFromMap(map[string]string{tsdb.MetricName: "m"}), t0, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	cases := []struct {
		name  string
		query string
	}{
		{"start_누락", "query=m&end=" + secParam(t0) + "&step=60"},
		{"end_누락", "query=m&start=" + secParam(t0) + "&step=60"},
		{"step_누락", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0)},
		{"step_0", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=0"},
		{"step_음수", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=-1"},
		{"step_NaN", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=NaN"},
		{"step_서브밀리초_ms절삭0", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=0.0004"},
		{"end가_start보다_이전", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0-60_000) + "&step=60"},
		{"포인트수_초과", "query=m&start=" + secParam(t0) + "&end=" + secParam(t0+1_500_000) + "&step=1"},
		{"start_end_차이_int64_오버플로우_우회", "query=m&start=-9e15&end=9e15&step=1"},
		{"잘못된_셀렉터", "query=" + url.QueryEscape("m{k=") + "&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=60"},
		{"정규식연산자_거절", "query=" + url.QueryEscape(`m{k=~"x"}`) + "&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=60"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := getWithCookie(t, srv.URL+"/api/v1/query_range?"+tc.query, cookie)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			var body struct {
				Status    string `json:"status"`
				ErrorType string `json:"errorType"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Status != "error" || body.ErrorType != "bad_data" {
				t.Fatalf("status/errorType = %+v, want status=error errorType=bad_data", body)
			}
		})
	}
}

// TestQueryRange_인증_보호 는 auth.Middleware 배선을 확인한다 — 세션 쿠키
// 없이는 401(M2/M4 에러 스키마 그대로)이어야 한다.
func TestQueryRange_인증_보호(t *testing.T) {
	db := newTestDB(t)
	const t0 = int64(1_700_000_000_000)
	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()

	reqURL := srv.URL + "/api/v1/query_range?query=m&start=" + secParam(t0) + "&end=" + secParam(t0) + "&step=60"
	resp := getWithCookie(t, reqURL, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("무인증 status = %d, want 401", resp.StatusCode)
	}
	var body struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "error" || body.ErrorType != "unauthorized" {
		t.Fatalf("무인증 에러 바디 = %+v, want status=error errorType=unauthorized", body)
	}
}

// TestQueryRange_빈결과는_null아닌_빈배열 은 매치되는 시리즈가 0건일 때
// `"result":[]` 로 직렬화되는지(= `make([]rangeResult, 0, ...)` 패턴) raw
// 바이트를 직접 검사한다. `var result []rangeResult` 로 잘못 구현하면
// "result":null 로 나가 Prometheus matrix 클라이언트가 오작동한다 — 구조체
// 디코드만으로는 len(nil)==len([])==0 이라 이 함정을 못 잡는다.
func TestQueryRange_빈결과는_null아닌_빈배열(t *testing.T) {
	db := newTestDB(t) // 샘플 0건 — 매치되는 시리즈가 없어야 한다.
	const t0 = int64(1_700_000_000_000)

	srv := httptest.NewServer(NewServer(db, nil, newTestAuthenticator(t)))
	defer srv.Close()
	cookie := loginForTest(t, srv)

	reqURL := srv.URL + "/api/v1/query_range?query=nonexistent_metric&start=" + secParam(t0) +
		"&end=" + secParam(t0) + "&step=60"
	resp := getWithCookie(t, reqURL, cookie)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), `"result":[]`) {
		t.Fatalf("body 에 result:[] 리터럴이 없다(null 로 직렬화됐을 가능성): %s", raw)
	}
}
