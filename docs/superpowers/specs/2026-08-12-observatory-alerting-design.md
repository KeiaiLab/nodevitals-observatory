# observatory 알림 — 룰 평가 + 발송 설계 (1a)

> 대상: `git.keiailab.com/keiailab-demo/gpumanager` (제품명 nodevitals-observatory)
> 작성: 2026-08-12 · 상태: 설계 확정, 구현 전

## 1. 배경

observatory 는 수집·저장·질의·시각화·인증까지 자체 구현한 관측 콘솔이다. 지금은
**보는 것까지만** 된다 — 임계를 넘었을 때 사람에게 알려주는 경로가 없다.

`/api/v1/rules` 와 `/api/v1/alerts` 는 404 다(2026-08-12 라이브 실측, demo 인스턴스).
알림 관련 코드는 `internal/demo/` 의 합성 인시던트 시뮬레이션뿐이고, 그것은 데모
시나리오를 재생하는 장치이지 실제 룰 엔진이 아니다.

알림이 없으면 "Prometheus·Grafana 없이 이 제품만으로 완결"이라는 주장이 성립하지
않는다. 운영자는 화면을 계속 보고 있을 수 없다.

## 2. 결정 사항

| 항목 | 결정 | 근거 |
|---|---|---|
| 감지 주체 | nodevitals(노드 로컬) + observatory(전역) **둘 다** | 하드웨어 치명상은 노드에서 즉시 잡고, 추세·집계는 중앙에서만 가능하다 |
| 발송 주체 | **observatory 단독** | 두 곳이 발송하면 같은 사건이 두 경로로 나가고 침묵·그룹화가 분할된다 |
| 이벤트 전달 | **메트릭 경유**(`nodevitals_events_total` 스크레이프) | 새 프로토콜 0. 라벨에 `node·tier·device·condition·severity·phase·xid` 가 이미 실려 있어 정보 손실이 없다 |
| PromQL | **자체 서브셋 구현** (외부 의존 없음) | `go.mod` 에 `require` 가 하나도 없다 — 22,461줄 전부 표준 라이브러리다. Prometheus `promql` 임베드는 전이 의존성 수백 개를 끌어와 "차트 하나 = 파드 하나 = 완결" 을 깬다 |
| 룰 포맷 | **Prometheus rule YAML 호환** | Phase 2 에서 기존 vmalert 룰 328개를 그대로 받기 위해. 자체 포맷은 이식 경로를 스스로 막는다 |
| 발송 채널 | **webhook 하나** | 최소 충분. 메일·채팅 분배는 수신 측이 이미 잘 하는 일이고, 그것까지 품으면 SMTP·OAuth 가 따라온다 |

### 왜 nodevitals 의 이벤트 엔진을 은퇴시키지 않는가

nodevitals 의 임계 엔진은 노드 로컬에서 15초 주기로 돈다. 중앙이 죽거나 네트워크가
끊겨도 그 노드는 계속 감지하고, 복구되면 카운터가 그대로 남아 있어 사건이 소실되지
않는다. 반대로 XID·ECC·SMART pending 같은 하드웨어 치명상은 중앙까지 왕복할 이유가
없다. 감지를 중앙으로 모으면 이 두 성질을 잃는다.

## 3. 아키텍처

```
nodevitals (노드당 1)                      observatory (중앙 1)
  ├ 임계 감지 → nodevitals_events_total ──┐
  └ 하드웨어 메트릭 ──────────────────────┤ scrape(15s) → tsdb
                                           │
                                           ├─ [신규] rules: 주기 평가 → 상태머신
                                           │        └ promql 평가 결과
                                           │
                                           ├─ [신규] notify: 그룹화 · 중복제거 · 재시도
                                           │        └ webhook POST
                                           │
                                           └─ apiserver: /api/v1/rules · /alerts (신규)
                                                        /api/v1/query (기존, promql 로 교체)
```

평가 결과는 `ALERTS` / `ALERTS_FOR_STATE` 시리즈로 tsdb 에 기록한다. Prometheus 가
같은 관례를 쓰며, 이렇게 하면 **알림 이력이 별도 저장소 없이 시계열 질의로 답해진다** —
"지난주에 이 알림이 몇 번 울렸나" 에 기존 질의 경로가 그대로 쓰인다.

## 4. 컴포넌트

### 4.1 `internal/promql` (신규)

현재 `apiserver/server.go` 의 `parseSelector` 는 메트릭 이름과 라벨 매처만 판다.
함수·연산자·집계는 파싱조차 되지 않는다(코드 주석: *"전체 PromQL(함수·연산자·정규식
매처)은 여전히 M3 로 유보"*). 룰 평가에는 최소한 `increase` 가 필요하므로 엔진이
선행되어야 한다.

구성: 렉서 → 파서 → AST → 평가기. `tsdb.Querier` 를 데이터 원천으로 받는다.
**평가는 promql 이, 저장·조회는 tsdb 가** 담당하도록 경계를 긋는다 — 지금 apiserver 에
인라인으로 있는 평가 로직은 이리로 옮긴다.

지원 범위는 §5 에 확정한다. **미지원 문법은 명시적 에러**를 낸다. 조용히 다른 답을
내는 것이 이 프로젝트가 가장 경계하는 실패 모드다.

### 4.2 `internal/rules` (신규)

- **로더**: Prometheus rule YAML(`groups[].rules[]` 의 `alert`/`expr`/`for`/`labels`/`annotations`)을 읽는다. `record` 룰은 이번 범위 밖이며 파싱 시 명시적으로 거부한다.
- **평가 루프**: 그룹 단위, 기본 30초 주기(설정 가능). 그룹 내 룰은 순차, 그룹 간은 독립.
- **상태머신**: `inactive → pending → firing`, 해소 시 `firing → inactive`. `for` 가 0 이면 pending 을 건너뛴다.
- 룰 하나의 평가 실패가 다른 룰을 막지 않는다. 실패는 그 룰의 `lastError` 로 남고 `/api/v1/rules` 에 노출된다.

### 4.3 `internal/notify` (신규)

- **그룹화**: `group_by` 라벨이 같은 알림을 한 번에 묶어 보낸다. 기본 `group_wait` 30초, `group_interval` 5분.
- **중복제거**: 같은 알림 지문(fingerprint)에 대해 `repeat_interval`(기본 4시간) 안에는 재발송하지 않는다.
- **재시도**: 지수 백오프 + 지터, 5회. **소진 시 드롭 카운터를 올린다**(§7).
- **해소 알림**: `firing → inactive` 전이 시 `status: resolved` 로 한 번 더 보낸다.

### 4.4 `internal/apiserver` (확장)

| 엔드포인트 | 내용 |
|---|---|
| `GET /api/v1/rules` | 그룹·룰·상태·`lastError`·`lastEvaluation`·`evaluationTime` |
| `GET /api/v1/alerts` | 현재 pending/firing 알림 목록 |
| `GET /api/v1/query` (기존) | 셀렉터 전용 → **promql 엔진으로 교체** |

Prometheus HTTP API 스키마를 따른다 — 기존 도구가 그대로 붙는다.

## 5. PromQL 서브셋 범위

범위는 추측이 아니라 **실측**으로 정했다. 이 클러스터의 vmalert 룰 328개가 실제로
쓰는 함수를 센 결과(2026-08-12):

| 함수 | 사용 | 함수 | 사용 |
|---|---:|---|---:|
| `increase` | 88 | `count_over_time` | 7 |
| `rate` | 60 | `changes` | 7 |
| `day_of_week` | 60 | `label_replace` | 5 |
| `hour` | 40 | `clamp_min` | 5 |
| `absent` | 23 | `abs` | 5 |
| `max_over_time` | 17 | `timestamp` | 2 |
| `avg_over_time` | 10 | `min_over_time` | 2 |
| `histogram_quantile` | 9 | `last_over_time` | 2 |
| | | `bottomk` | 1 |

**1a 구현 대상(13종)**: `increase` · `rate` · `absent` · `max_over_time` ·
`avg_over_time` · `min_over_time` · `count_over_time` · `last_over_time` ·
`changes` · `day_of_week` · `hour` · `abs` · `clamp_min`

**함께 필요한 문법**: range vector selector(`[5m]`) · 집계(`sum`/`avg`/`min`/`max`/`count`
+ `by`/`without`) · 이항 산술·비교 연산자(+ `bool` modifier) · 정규식 매처(`=~`/`!~`) ·
스칼라 리터럴.

**이번 범위 밖(명시적 에러)**: `histogram_quantile`(히스토그램 메트릭 자체가 아직
없다) · `topk`/`bottomk` · `label_replace`/`label_join`(주로 시리즈 조인용이라
`on`/`group_left` 와 함께 와야 쓸모가 있다) · `timestamp` ·
`predict_linear`/`deriv`/`holt_winters` · `offset` · `@` modifier ·
부분집합 매칭(`on`/`ignoring`/`group_left`/`group_right`) · subquery.

실측 24종 대비 커버 관계는 이렇다 — 위 13종 + 집계 5종(`sum`·`avg`·`min`·`max`·`count`)
+ `time`·`vector` = **20종을 덮고, 4종**(`histogram_quantile` 9회 · `label_replace` 5회 ·
`timestamp` 2회 · `bottomk` 1회, 합 17회)을 남긴다.

제외 항목은 Phase 2 에서 룰 이식 시 실제로 걸리는 것만 추가한다. 지금 만들면 쓰지
않는 코드가 된다.

## 6. 룰 정의와 배포

```yaml
groups:
  - name: hardware
    interval: 30s
    rules:
      - alert: GpuXidCritical
        expr: increase(nodevitals_events_total{condition="gpu_xid_error",phase="ENTER"}[5m]) > 0
        for: 0s
        labels:
          severity: critical
        annotations:
          summary: "{{ $labels.node }} {{ $labels.device }} XID {{ $labels.xid }}"
```

룰 파일은 ConfigMap 으로 마운트하고 경로를 플래그(`-rules-dir`)로 준다. 시작 시 1회
로드하고, 파일 변경은 **주기적으로 다시 읽는다**(mtime 비교, 기본 60초). 파싱 실패한
파일은 **직전 정상 룰셋을 유지**하고 에러를 로그와 `/api/v1/rules` 에 남긴다 —
오타 하나로 전체 알림이 멈추는 것이 가장 나쁜 결과다.

annotation 템플릿은 Go `text/template` 에 `$labels`·`$value` 를 노출한다.

## 7. 에러 처리 — 조용한 실패 금지

이 프로젝트군이 반복해서 다친 지점이라 명시한다.

| 상황 | 처리 |
|---|---|
| 미지원 PromQL 문법 | 파싱 단계에서 **에러**. 유사 함수로 대체하거나 무시하지 않는다 |
| 룰 평가 실패 | 그 룰만 실패, 나머지 계속. `lastError` 노출 + `rule_evaluation_failures_total` |
| 발송 실패(재시도 소진) | **`notify_dropped_total` 증가**. 로그만 남기고 끝내지 않는다 |
| 룰 파일 파싱 실패 | 직전 룰셋 유지 + `rule_group_load_failures_total` |
| 스크레이프 실패로 데이터 없음 | `absent()` 로 룰을 쓸 수 있게 하고, 엔진은 빈 결과를 에러로 취급하지 않는다 |

observatory 자신의 상태도 `/metrics` 로 낸다: `rule_evaluation_duration_seconds`,
`rule_evaluation_failures_total`, `notify_sent_total`, `notify_dropped_total`,
`alerts_active`.

## 8. 테스트

- **promql**: 표 기반 단위 테스트. 함수마다 정상·경계(빈 range·단일 샘플·counter reset)를 덮는다. `rate`/`increase` 의 counter reset 처리는 별도 케이스로 고정한다 — 파드 재시작마다 실제로 일어나는 일이다.
- **rules**: 시간을 주입해 `for` 경계에서 `pending → firing` 전이가 일어나는지, 경계 직전에는 일어나지 않는지 양쪽을 검증한다.
- **notify**: `httptest` 로 성공·5xx 재시도·소진 드롭·해소 발송 4경로.
- **통합**: 합성 시계열을 tsdb 에 넣고 룰 → 발송까지 한 번에 흐르는지 확인한다.

## 9. 비목표

- Alertmanager 의 침묵(silence) UI·억제(inhibition) 규칙 — Phase 2
- 메일·채팅 직접 발송 — webhook 수신 측이 담당
- recording rule — 저장 부담과 이득을 먼저 재고 결정
- 멀티테넌시·알림 권한 분리

## 10. 리스크

| 리스크 | 대응 |
|---|---|
| 자체 PromQL 이 Prometheus 와 미묘하게 다르게 계산 | 함수별로 Prometheus 문서의 정의를 주석에 인용하고, 경계 케이스를 테스트로 고정. 호환을 주장하는 표면은 남의 기준으로 잰다 |
| `rate`/`increase` 의 extrapolation 규칙 | Prometheus 는 range 경계에서 외삽한다. 이 규칙을 그대로 구현하고, 안 할 경우 값이 체계적으로 작아진다는 점을 테스트로 박는다 |
| 룰 수가 늘면 평가가 스크레이프와 경합 | 평가 시간을 메트릭으로 내고, 그룹 단위 병렬화는 실측 후 판단 |
| 알림 폭풍 | `group_wait`/`group_interval`/`repeat_interval` 기본값을 보수적으로 두고, 그룹화 없이는 발송하지 않는다 |
