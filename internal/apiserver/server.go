// Package apiserver 는 internal/tsdb 위에 Prometheus HTTP API 와 호환되는 최소
// 조회 엔드포인트를 제공한다(grafana/promtool 이 읽을 수 있는 최소 스키마).
// M2 범위: instant query, series 검색, label 이름 나열, 헬스체크. 정규식·함수·
// 연산자를 포함한 완전한 PromQL 은 M3 로 유보한다.
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

	"github.com/KeiaiLab/nodevitals-observatory/internal/tsdb"
)

// queryWindow 는 /api/v1/query 의 평가창 폭이다 — [eval-queryWindow, eval].
const queryWindow = 5 * time.Minute

// defaultSeriesWindow 은 /api/v1/series, /api/v1/labels 의 start/end 생략 시
// 기본 조회창(최근) 이다.
const defaultSeriesWindow = time.Hour

// NewServer 는 조회 핸들러 묶음을 만든다. ready 는 /readyz 판정 함수이며 nil 이면
// 항상 준비된 것으로 취급한다. 패턴은 nodevitals internal/httpapi.NewServer 미러
// (Go 1.22+ 메서드 패턴 mux).
func NewServer(db *tsdb.DB, ready func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(ready))
	mux.HandleFunc("GET /api/v1/query", handleQuery(db))
	mux.HandleFunc("GET /api/v1/series", handleSeries(db))
	mux.HandleFunc("GET /api/v1/labels", handleLabels(db))
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
