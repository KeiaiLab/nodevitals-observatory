// Package apiserver 는 internal/tsdb 위에 Prometheus HTTP API 와 호환되는 최소
// 조회 엔드포인트를 제공한다(grafana/promtool 이 읽을 수 있는 최소 스키마).
// M2 범위: instant query, series 검색, label 이름 나열, 헬스체크. 정규식·함수·
// 연산자를 포함한 완전한 PromQL 은 M3 로 유보한다.
//
// M4: internal/auth 미들웨어로 /api/v1/{query,series,labels} 를 보호하고
// internal/webui 정적 콘솔을 "/" 에 서빙한다(m4-design.md §3). 공개 예외 =
// /healthz·/readyz·POST /api/v1/auth/{login,logout}·콘솔 정적 자산(D1). M2
// 핸들러 본문(handleQuery/handleSeries/handleLabels)은 무수정 — 배선만 바뀐다(D6).
//
// M5: /api/v1/query_range 를 추가한다(m5-design.md §3, M3-lite). parseSelector
// + db.Querier + Select 패턴을 그대로 미러하되 [start,end] 를 step 간격 버킷
// 시계열(matrix)로 변환한다 — 완전한 PromQL(함수·연산자·정규식 매처)은
// 여전히 M3 로 유보한다. handleQuery 등 기존 M2/M4 핸들러는 무수정이다.
package apiserver

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KeiaiLab/nodevitals-observatory/internal/auth"
	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
	"github.com/KeiaiLab/nodevitals-observatory/internal/webui"
)

// queryWindow 는 /api/v1/query 의 평가창 폭이다 — [eval-queryWindow, eval].
const queryWindow = 5 * time.Minute

// defaultSeriesWindow 은 /api/v1/series, /api/v1/labels 의 start/end 생략 시
// 기본 조회창(최근) 이다.
const defaultSeriesWindow = time.Hour

// NewServer 는 조회 핸들러 묶음을 만든다. ready 는 /readyz 판정 함수이며 nil 이면
// 항상 준비된 것으로 취급한다. 패턴은 nodevitals internal/httpapi.NewServer 미러
// (Go 1.22+ 메서드 패턴 mux).
//
// a 는 M4 인증 배선이다 — nil 금지(계약, m4-design.md §3): 호출자(main)가 항상
// 구성해 넘긴다. 공개(인증 예외) = /healthz·/readyz·POST /api/v1/auth/{login,
// logout}·"/"(콘솔 정적 자산, webui.Handler — 데이터 0 이라 인증 불요, D1).
// 보호 = /api/v1/{query,query_range,series,labels} — a.Middleware 로 감싸며 M2
// 핸들러 본문은 무수정이다(배선만 변경, D6). query_range 는 M5 신규(동일
// Middleware 로 보호, m5-design.md §3.1).
func NewServer(db *tsdb.DB, ready func() bool, a *auth.Authenticator, opts ...ServerOption) http.Handler {
	var cfg serverConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	// public demo 는 데모 엔진과 함께일 때만 성립한다 — 실서비스(합성 아닌
	// 실 데이터)에서 인증이 실수로 풀리는 경로를 구조적으로 막는다.
	if cfg.demo == nil {
		cfg.openAccess = false
	}

	// protect 는 보호 라우트 래퍼다. public demo 모드에서는 통과시킨다 —
	// 합성 데이터만 서빙하므로 로그인 장벽이 시연 접근성만 해친다. 프론트는
	// 세션 프로브(GET /api/v1/labels)가 200 이 되므로 무수정으로 로그인 화면을
	// 건너뛴다.
	protect := func(h http.Handler) http.Handler { return a.Middleware(h) }
	actor := a.Username
	if cfg.openAccess {
		protect = func(h http.Handler) http.Handler { return h }
		actor = func() string { return "guest" }
	}

	mux := http.NewServeMux()

	// 공개 (인증 예외)
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(ready))
	mux.Handle("POST /api/v1/auth/login", a.LoginHandler())
	mux.Handle("POST /api/v1/auth/logout", a.LogoutHandler())
	mux.HandleFunc("GET /api/", handleUnknownAPI) // 미등록 /api/* GET → JSON 404(m5-design.md 적대검토 F3 — 아래 webui SPA 서브트리보다 구체적이라 우선)
	mux.Handle("GET /", webui.Handler())          // 콘솔 정적 자산(D1) — catch-all 이나 위 구체 패턴이 우선한다

	// 데모 status 는 항상 등록(비인증, enabled 불리언만) — 프론트의 모드 감지
	// 계약. 나머지 데모 라우트는 엔진이 있을 때만 등록하고, 없으면 위
	// handleUnknownAPI 가 JSON 404 를 담당한다(off 분기 코드 0).
	mux.HandleFunc("GET /api/v1/demo/status", handleDemoStatus(cfg.demo, cfg.openAccess))
	if cfg.demo != nil {
		mux.Handle("GET /api/v1/demo/state", protect(handleDemoState(cfg.demo)))
		mux.Handle("POST /api/v1/demo/actions/{action}", protect(handleDemoAction(cfg.demo, actor)))
	}

	// 보호 (M2 핸들러 본문 무수정 — 배선만 Middleware 로 감싼다)
	mux.Handle("GET /api/v1/query", protect(handleQuery(db)))
	mux.Handle("GET /api/v1/query_range", protect(handleQueryRange(db)))
	mux.Handle("GET /api/v1/series", protect(handleSeries(db)))
	mux.Handle("GET /api/v1/labels", protect(handleLabels(db)))

	return mux
}

// handleHealthz/handleReadyz 는 nodevitals 관례대로 순수 텍스트 바디("ok" /
// "not ready")를 낸다 — Content-Type 을 강제하지 않아 Go 가 본문으로 sniff 한다
// (JSON 오브젝트가 아닌 리터럴 문자열을 application/json 으로 잘못 표시하지
// 않기 위함).
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleReadyz(ready func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if ready != nil && !ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// handleUnknownAPI 는 등록되지 않은 /api/* GET 경로에 JSON 404 를 낸다.
// webui 의 "GET /" SPA 서브트리 catch-all 이 미등록 API 경로(오타·미래 버전 등)
// 까지 흡수해 index.html 200(HTML)을 내는 M4 대비 회귀(m5-design.md 적대검토
// F3)를 막는다 — /api/v1/{query,query_range,series,labels} 등 구체 패턴은
// ServeMux 우선순위(구체성)로 이 서브트리보다 먼저 매치되므로 영향받지 않는다.
func handleUnknownAPI(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "unknown endpoint: "+r.URL.Path)
}

// ---- /api/v1/query ----

type queryData struct {
	ResultType string        `json:"resultType"`
	Result     []queryResult `json:"result"`
}

type queryResult struct {
	Metric map[string]string `json:"metric"`
	Value  [2]any            `json:"value"`
}

// handleQuery 는 instant 쿼리를 처리한다. 평가창 [eval-5m, eval] 안에서 매처를
// 만족하는 시리즈별 최신 샘플 1개만 낸다(Prometheus instant query 의미론).
func handleQuery(db *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		matchers, err := parseSelector(q.Get("query"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		evalMS, err := evalTimeMS(q.Get("time"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		mint := evalMS - queryWindow.Milliseconds()
		querier, closeFn, err := db.Querier(mint, evalMS)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		defer func() { _ = closeFn() }()

		evalSec := float64(evalMS) / 1000
		series := querier.Select(matchers...)
		result := make([]queryResult, 0, len(series))
		for _, s := range series {
			it := s.Iterator()
			var (
				lastV float64
				found bool
			)
			for it.Next() {
				_, lastV = it.At()
				found = true
			}
			if err := it.Err(); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			if !found {
				continue
			}
			result = append(result, queryResult{
				Metric: labelsToMap(s.Labels()),
				Value:  [2]any{evalSec, strconv.FormatFloat(lastV, 'g', -1, 64)},
			})
		}

		writeSuccess(w, queryData{ResultType: "vector", Result: result})
	}
}

// ---- /api/v1/query_range (M5, M3-lite) ----

// maxRangePoints 는 시리즈당 응답 포인트 상한이다 — 화면 렌더링 픽셀 수
// (~1000) 유계 + 여유(m5-design.md §3.1 D4). 요청한 [start,end]/step 해상도가
// 이를 넘으면 400 으로 거절한다. 전체 PromQL(함수·연산자·정규식 매처)은
// 여전히 M3 로 유보 — 여기선 selector+range 만 다룬다.
const maxRangePoints = 1500

type rangeData struct {
	ResultType string        `json:"resultType"`
	Result     []rangeResult `json:"result"`
}

type rangeResult struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"` // [unix초 float64, 값 문자열] — queryResult.Value 미러
}

// handleQueryRange 는 range 쿼리를 처리한다(m5-design.md §3). matchers 를
// 만족하는 시리즈별로 [start,end] 를 step 간격 버킷 시계열(matrix)로 만든다 —
// 각 버킷 t 의 값은 (t-queryWindow, t] 안의 최신 샘플이다(Prometheus
// staleness-lite, handleQuery 의 5m 평가창을 재사용). 응답은 Prometheus HTTP
// API matrix 스키마와 호환된다.
func handleQueryRange(db *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		matchers, err := parseSelector(q.Get("query"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		// start/end 는 둘 다 필수다 — instant 쿼리의 `time` 과 달리 range 는
		// wall-clock 기본값을 두지 않는다(m5-design.md §3.2).
		startRaw := q.Get("start")
		if startRaw == "" {
			writeError(w, http.StatusBadRequest, "bad_data", "start 파라미터가 필요하다")
			return
		}
		startMS, err := parseUnixSecondsMS(startRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		endRaw := q.Get("end")
		if endRaw == "" {
			writeError(w, http.StatusBadRequest, "bad_data", "end 파라미터가 필요하다")
			return
		}
		endMS, err := parseUnixSecondsMS(endRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		if endMS < startMS {
			writeError(w, http.StatusBadRequest, "bad_data", "end 이 start 보다 이전일 수 없다")
			return
		}

		stepMS, err := parseStepMS(q.Get("step"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		// endMS-startMS 를 int64 그대로 빼면 start/end 각각은 유효 범위(예:
		// ±9e15초)여도 차가 int64 상한을 넘어 음수로 오버플로우해 이 유계
		// 검사를 우회하고, 그 뒤 bucketSeries 가 그 폭을 stepMS 간격으로
		// 순회하는 DoS 로 이어진다(적대검토). float64 로 계산해 오버플로우를
		// 없앤다.
		if numPoints := (float64(endMS)-float64(startMS))/float64(stepMS) + 1; numPoints > maxRangePoints {
			writeError(w, http.StatusBadRequest, "bad_data",
				fmt.Sprintf("요청 해상도가 최대 포인트 수를 초과한다(%.0f > %d)", numPoints, maxRangePoints))
			return
		}

		lookbackMS := queryWindow.Milliseconds()
		querier, closeFn, err := db.Querier(startMS-lookbackMS, endMS)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		defer func() { _ = closeFn() }()

		series := querier.Select(matchers...)
		result := make([]rangeResult, 0, len(series))
		for _, s := range series {
			values, err := bucketSeries(s, startMS, endMS, stepMS, lookbackMS)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			if len(values) == 0 {
				continue
			}
			result = append(result, rangeResult{
				Metric: labelsToMap(s.Labels()),
				Values: values,
			})
		}

		writeSuccess(w, rangeData{ResultType: "matrix", Result: result})
	}
}

// bucketSeries 는 시리즈의 샘플 스트림(chainIterator, 시간 오름차순)과
// [start,end]/step 버킷 커서를 투 포인터로 1-pass 병합한다(O(samples+points),
// m5-design.md §3.3). 버킷 t 의 값 = (t-lookbackMS, t] 구간의 최신 샘플 — 그
// 구간에 샘플이 없으면 버킷을 생략한다(matrix 에 구멍, 프론트가
// connectNulls=false 로 끊어 그린다).
func bucketSeries(s tsdb.Series, startMS, endMS, stepMS, lookbackMS int64) ([][2]any, error) {
	it := s.Iterator()
	hasSample := it.Next()
	var curT int64
	var curV float64
	if hasSample {
		curT, curV = it.At()
	}

	var (
		lastT    int64
		lastV    float64
		haveLast bool
	)

	var values [][2]any
	for t := startMS; t <= endMS; t += stepMS {
		// t 이하의 모든 신규 샘플을 커서에 흡수 — 다음 버킷을 위해 남겨두지
		// 않는다(각 샘플은 그 시각 이후 최초로 도달하는 버킷부터 stale 해질
		// 때까지 여러 버킷에 이월될 수 있다).
		for hasSample && curT <= t {
			lastT, lastV = curT, curV
			haveLast = true
			hasSample = it.Next()
			if hasSample {
				curT, curV = it.At()
			}
		}
		if haveLast && lastT > t-lookbackMS {
			values = append(values, [2]any{float64(t) / 1000, strconv.FormatFloat(lastV, 'g', -1, 64)})
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// parseStepMS 는 step 파라미터(초, 정수/실수)를 밀리초로 바꾼다. 빈 문자열·
// 비유한값은 거절한다.
//
// 검사는 반드시 "초 값 > 0" 이 아니라 "변환된 ms 값 > 0" 에 걸어야 한다 —
// 서브밀리초 step(예: 0.0004초)은 초 값으로는 양수이지만 int64(step*1000)
// 절삭으로 stepMS=0 이 되어, 이후 버킷 커서(t += stepMS)가 0-증분 무한루프에
// 빠진다(m5-design.md 적대검토 F2). 그래서 ms 변환 후 값을 검사한다.
func parseStepMS(raw string) (int64, error) {
	if raw == "" {
		return 0, fmt.Errorf("apiserver: step 파라미터가 필요하다")
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("apiserver: step 파싱 실패 %q: %w", raw, err)
	}
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, fmt.Errorf("apiserver: step 이 유한값이 아니다: %q", raw)
	}
	stepMS := int64(sec * 1000)
	if stepMS <= 0 {
		return 0, fmt.Errorf("apiserver: step 이 너무 작다(밀리초 절삭 후 %d, 1ms 이상이어야 한다): %q", stepMS, raw)
	}
	return stepMS, nil
}

// ---- /api/v1/series ----

// handleSeries 는 match[] 셀렉터 하나 이상을 받아 라벨셋 union 을 낸다
// (MapKey 기준 dedup — 겹치는 셀렉터가 같은 시리즈를 중복 내지 않는다).
func handleSeries(db *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		exprs := r.URL.Query()["match[]"]
		if len(exprs) == 0 {
			writeError(w, http.StatusBadRequest, "bad_data", "match[] 파라미터가 최소 1개 필요하다")
			return
		}

		matcherSets := make([][]*tsdb.Matcher, 0, len(exprs))
		for _, expr := range exprs {
			ms, err := parseSelector(expr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_data", err.Error())
				return
			}
			matcherSets = append(matcherSets, ms)
		}

		startMS, endMS, err := timeRangeMS(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		querier, closeFn, err := db.Querier(startMS, endMS)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		defer func() { _ = closeFn() }()

		seen := map[string]map[string]string{}
		order := make([]string, 0)
		for _, ms := range matcherSets {
			for _, s := range querier.Select(ms...) {
				lset := s.Labels()
				key := lset.MapKey()
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = labelsToMap(lset)
				order = append(order, key)
			}
		}

		sort.Strings(order)
		data := make([]map[string]string, 0, len(order))
		for _, k := range order {
			data = append(data, seen[k])
		}
		writeSuccess(w, data)
	}
}

// ---- /api/v1/labels ----

// handleLabels 는 [start,end] 창(기본 최근 1h)에 걸치는 블록 + head 전체에서
// 라벨 이름을 모은다. 창은 db.Querier 의 블록 선별에 반영된다 — head 쪽은
// tsdb.Head.Select 자체가 시간 필터를 두지 않아(라벨 나열은 시리즈 존재 여부만
// 본다) 상시 포함되지만, 오래된 블록을 창 밖으로 배제하는 효과는 유지된다.
func handleLabels(db *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startMS, endMS, err := timeRangeMS(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_data", err.Error())
			return
		}

		querier, closeFn, err := db.Querier(startMS, endMS)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		defer func() { _ = closeFn() }()

		writeSuccess(w, querier.LabelNames())
	}
}

// ---- 공통 응답 ----

type errorResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

type successResponse struct {
	Status string `json:"status"`
	Data   any    `json:"data"`
}

func writeError(w http.ResponseWriter, status int, errorType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Status: "error", ErrorType: errorType, Error: msg})
}

func writeSuccess(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(successResponse{Status: "success", Data: data})
}

func labelsToMap(ls tsdb.Labels) map[string]string {
	out := make(map[string]string, len(ls))
	for _, l := range ls {
		out[l.Name] = l.Value
	}
	return out
}

// ---- 시각 파라미터 ----

// evalTimeMS 는 /api/v1/query 의 `time` 파라미터(unix 초, 정수/실수)를 밀리초
// epoch 로 변환한다. 빈 문자열이면 wall clock(현재 시각)을 쓴다 — 이 엔드포인트는
// 사용자 요청 시점의 "지금"을 의미하는 값이라 순수 로직이 아니라 요청 처리
// 그 자체의 일부다(m2-design.md §5 계약). RFC3339 같은 비-숫자 형식은
// strconv.ParseFloat 이 그대로 거부해 400 으로 이어진다(M3 로 유보, 여기서
// 별도 분기를 두지 않는다).
func evalTimeMS(raw string) (int64, error) {
	if raw == "" {
		return time.Now().UnixMilli(), nil
	}
	return parseUnixSecondsMS(raw)
}

// timeRangeMS 는 /api/v1/series, /api/v1/labels 의 start/end 파라미터(unix 초)를
// 밀리초 창으로 변환한다. end 생략 시 wall clock, start 생략 시 end-1h.
func timeRangeMS(r *http.Request) (startMS, endMS int64, err error) {
	q := r.URL.Query()

	endMS = time.Now().UnixMilli()
	if raw := q.Get("end"); raw != "" {
		if endMS, err = parseUnixSecondsMS(raw); err != nil {
			return 0, 0, err
		}
	}

	startMS = endMS - defaultSeriesWindow.Milliseconds()
	if raw := q.Get("start"); raw != "" {
		if startMS, err = parseUnixSecondsMS(raw); err != nil {
			return 0, 0, err
		}
	}

	return startMS, endMS, nil
}

// parseUnixSecondsMS 는 unix 초 문자열(정수/실수, "NaN"/"Inf" 포함)을 밀리초로
// 바꾼다. NaN/Inf 는 ParseFloat 자체는 성공시키지만 int64 변환이 플랫폼별로
// 정의되지 않은 값을 내므로(Go 스펙상 "implementation-specific") 여기서 명시
// 거절한다.
func parseUnixSecondsMS(s string) (int64, error) {
	sec, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("apiserver: 시각 파라미터 파싱 실패 %q: %w", s, err)
	}
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, fmt.Errorf("apiserver: 시각 파라미터가 유한값이 아니다: %q", s)
	}
	return int64(sec * 1000), nil
}

// ---- 최소 셀렉터 파서 ----
//
// 지원 문법: `metric_name` / `metric_name{}` / `metric_name{k="v",k2="v2"}` /
// `{k="v"}`(이름 생략). `=~`·`!=`·`!~`·함수·연산자 등장은 400 으로 명시
// 거절한다 — "full PromQL is M3", 여기서 정규식 매처를 열지 않는다(지시 고정).

// parseSelector 는 셀렉터 문자열을 tsdb 매처 목록으로 바꾼다.
func parseSelector(expr string) ([]*tsdb.Matcher, error) {
	name, rest := splitSelectorName(expr)

	labelMatchers, err := parseLabelBlock(rest)
	if err != nil {
		return nil, err
	}

	var matchers []*tsdb.Matcher
	if name != "" {
		m, err := tsdb.NewMatcher(tsdb.MatchEqual, tsdb.MetricName, name)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
	}
	matchers = append(matchers, labelMatchers...)

	if len(matchers) == 0 {
		return nil, fmt.Errorf("apiserver: 빈 셀렉터는 허용하지 않는다: %q", expr)
	}
	return matchers, nil
}

// splitSelectorName 은 `{` 앞의 메트릭 이름과 그 뒤(중괄호 포함) 부분을 나눈다.
// `{` 가 없으면 전체가 이름이고 rest 는 빈 문자열이다.
func splitSelectorName(expr string) (name, rest string) {
	idx := strings.IndexByte(expr, '{')
	if idx < 0 {
		return strings.TrimSpace(expr), ""
	}
	return strings.TrimSpace(expr[:idx]), expr[idx:]
}

// parseLabelBlock 은 `{...}` 라벨 매처 블록을 파싱한다. rest 가 빈 문자열이면
// (중괄호 없음) 매처 없이 nil 을 반환한다.
func parseLabelBlock(rest string) ([]*tsdb.Matcher, error) {
	if rest == "" {
		return nil, nil
	}
	if rest[0] != '{' {
		return nil, fmt.Errorf("apiserver: 라벨 블록은 { 로 시작해야 한다: %q", rest)
	}

	var matchers []*tsdb.Matcher
	i := 1
loop:
	for {
		i = skipSpace(rest, i)
		if i >= len(rest) {
			return nil, fmt.Errorf("apiserver: 라벨 블록이 닫히지 않았다(누락된 }): %q", rest)
		}
		if rest[i] == '}' {
			i++
			break loop
		}

		nameStart := i
		for i < len(rest) && isSelectorNameChar(rest[i]) {
			i++
		}
		labelName := rest[nameStart:i]
		if labelName == "" {
			return nil, fmt.Errorf("apiserver: 라벨 이름이 비어 있다: %q", rest)
		}

		i = skipSpace(rest, i)
		op, next, err := scanOperator(rest, i)
		if err != nil {
			return nil, err
		}
		i = next
		if op != "=" {
			return nil, fmt.Errorf("apiserver: 지원하지 않는 매처 연산자 %q — 정규식/부정 매처는 M3", op)
		}

		i = skipSpace(rest, i)
		if i >= len(rest) || rest[i] != '"' {
			return nil, fmt.Errorf("apiserver: 라벨 값은 따옴표 문자열이어야 한다: %q", rest)
		}
		value, next2, err := scanQuotedValue(rest, i+1)
		if err != nil {
			return nil, err
		}
		i = next2

		m, err := tsdb.NewMatcher(tsdb.MatchEqual, labelName, value)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)

		i = skipSpace(rest, i)
		if i >= len(rest) {
			return nil, fmt.Errorf("apiserver: 라벨 블록이 닫히지 않았다(누락된 }): %q", rest)
		}
		switch rest[i] {
		case ',':
			i++
		case '}':
			i++
			break loop
		default:
			return nil, fmt.Errorf("apiserver: , 또는 } 가 필요하다: %q", rest[i:])
		}
	}

	if i != len(rest) {
		return nil, fmt.Errorf("apiserver: 라벨 블록 뒤에 불필요한 문자가 있다: %q", rest[i:])
	}
	return matchers, nil
}

// scanOperator 는 s[i:] 에서 매처 연산자(=, !=, =~, !~)를 읽는다. `=` 만
// 지원 대상이고 나머지 3종은 파서가 인식은 하되(정확한 에러 메시지를 위해)
// 호출자가 즉시 거절한다.
func scanOperator(s string, i int) (op string, next int, err error) {
	if i >= len(s) {
		return "", 0, fmt.Errorf("apiserver: 매처 연산자가 없다: %q", s)
	}
	switch s[i] {
	case '=':
		if i+1 < len(s) && s[i+1] == '~' {
			return "=~", i + 2, nil
		}
		return "=", i + 1, nil
	case '!':
		if i+1 >= len(s) {
			return "", 0, fmt.Errorf("apiserver: ! 뒤에 = 또는 ~ 가 필요하다: %q", s)
		}
		switch s[i+1] {
		case '=':
			return "!=", i + 2, nil
		case '~':
			return "!~", i + 2, nil
		default:
			return "", 0, fmt.Errorf("apiserver: 알 수 없는 연산자 !%c", s[i+1])
		}
	default:
		return "", 0, fmt.Errorf("apiserver: 매처 연산자가 필요하다(= != =~ !~): %q", s[i:])
	}
}

// scanQuotedValue 는 s[i:] (여는 " 바로 다음 위치)에서 닫는 "까지 라벨 값을
// 읽으며 3종 이스케이프(\\, \", \n)를 복원한다. promtext 파서와 동일 규칙 —
// 콤마 분리 없이 문자 단위로 스캔해 값 안의 `,`·`}` 를 안전하게 다룬다.
func scanQuotedValue(s string, i int) (value string, next int, err error) {
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		switch c {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			if i+1 >= len(s) {
				return "", 0, fmt.Errorf("apiserver: 이스케이프가 문자열 끝에서 잘렸다: %q", s)
			}
			switch s[i+1] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			default:
				return "", 0, fmt.Errorf("apiserver: 알 수 없는 이스케이프 시퀀스 \\%c", s[i+1])
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", 0, fmt.Errorf("apiserver: 라벨 값 문자열이 닫히지 않았다: %q", s)
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func isSelectorNameChar(c byte) bool {
	return c == '_' || c == ':' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
