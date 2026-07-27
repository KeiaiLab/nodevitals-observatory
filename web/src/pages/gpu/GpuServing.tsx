// GpuServing — "서빙 · SLO". 이 플랫폼의 워크로드는 GPU **추론 서빙**이다:
// 하드웨어 지표(util/temp/power)만 보이면 장비 모니터지만, 요청·지연·토큰·큐와
// SLO 가 함께 보이면 운영 중인 추론 서비스 관제가 된다.
//
// 백엔드가 만든 인과를 화면이 그대로 서술한다:
//   GPU 사용률↑ → 배치 크기↑ → 큐 깊이↑ → p95/p99↑ → (부하 90% 초과) 에러율↑
// 그래서 히트맵이 붉어지는 시각과 지연이 오르는 시각이 일치한다.
//
// 차트는 Explorer 의 CSP-안전 Recharts 셋업 미러 — 원시 컴포넌트 + 색은
// var(--metric-*) 문자열 직접 참조(ChartStyle 의 style 주입 회피).
import { useMemo, useState } from 'react';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import EventStream from '@/components/gpu/EventStream';
import KpiTile from '@/components/gpu/KpiTile';
import SloPanel from '@/components/gpu/SloPanel';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useDemo } from '@/hooks/demoContext';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type ApiEnvelope, type MatrixData, type VectorData } from '@/lib/api';
import {
  formatCount,
  formatMs,
  formatPct,
  formatRps,
  formatTokensPerSec,
  METRIC_DEMO_FLEET_ERR_PCT,
  METRIC_DEMO_FLEET_P95,
  METRIC_DEMO_FLEET_RPS,
  METRIC_DEMO_FLEET_TOKENS,
  METRIC_DEMO_POOL_BATCH,
  METRIC_DEMO_POOL_P50,
  METRIC_DEMO_POOL_P95,
  METRIC_DEMO_POOL_P99,
  METRIC_DEMO_POOL_QUEUE,
  selector,
} from '@/lib/gpuMetrics';

const KPI_POLL_MS = 15_000;
const RANGE_POLL_MS = 30_000;
const FLEET_RANGE_SECONDS = 3 * 3600;
const POOL_RANGE_SECONDS = 3600;
const MIN_STEP_SECONDS = 15;
const TARGET_POINTS = 500;
/** 에러율 경고 임계 — 이 위는 사용자에게 실패가 보이기 시작하는 구간. */
const ERROR_RATE_WARN_PCT = 0.5;

function firstValue(env: ApiEnvelope<VectorData>): number | null {
  const sample = env.data.result[0];
  return sample ? Number(sample.value[1]) : null;
}

/** 조회 창 — step 은 화면 해상도 목표(~500 포인트) 기준(Explorer 관례). */
function rangeWindow(seconds: number): { startSec: number; endSec: number; step: number } {
  const endSec = Math.floor(Date.now() / 1000);
  return {
    startSec: endSec - seconds,
    endSec,
    step: Math.max(MIN_STEP_SECONDS, Math.floor(seconds / TARGET_POINTS)),
  };
}

interface ChartRow {
  time: number;
  [seriesKey: string]: number | null;
}

interface MatrixPart {
  key: string;
  matrix: MatrixData;
}

// 여러 matrix 를 공통 시간축 행으로 병합한다 — 빠진 버킷은 null 로 명시해
// connectNulls=false 가 그 지점만 끊어 그리게 한다(Explorer/Efficiency 관례).
function mergeMatrices(parts: readonly MatrixPart[]): ChartRow[] {
  const byTime = new Map<number, ChartRow>();
  for (const { key, matrix } of parts) {
    for (const s of matrix.result) {
      for (const [t, v] of s.values) {
        let row = byTime.get(t);
        if (!row) {
          row = { time: t };
          for (const p of parts) row[p.key] = null;
          byTime.set(t, row);
        }
        row[key] = Number(v);
      }
    }
  }
  return [...byTime.values()].sort((a, b) => a.time - b.time);
}

function formatClock(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

const CHART_MARGIN = { top: 8, right: 16, left: 0, bottom: 8 } as const;

const TOOLTIP_STYLE = {
  backgroundColor: 'var(--popover)',
  borderColor: 'var(--border)',
  color: 'var(--popover-foreground)',
  fontSize: 12,
} as const;

export default function GpuServing() {
  const { demoMode, state } = useDemo();
  const demoOn = demoMode === true;

  // 표에서 고른 풀. null = 아직 선택 없음 → RPS 1위가 기본.
  const [picked, setPicked] = useState<string | null>(null);

  // 표 정렬(RPS 내림차순) — 기본 선택(1위)의 근거이기도 하다.
  const serving = useMemo(() => {
    const rows = state?.serving ?? [];
    return [...rows].sort((a, b) => b.rps - a.rps);
  }, [state?.serving]);

  const activePool = picked ?? serving[0]?.pool ?? '';
  const active = serving.find((s) => s.pool === activePool) ?? null;

  // 플릿 롤업 KPI 4종 (instant, 15s).
  const kpi = usePolledQuery(
    async () => {
      if (!demoOn) return null;
      const [rps, p95, tokens, err] = await Promise.all([
        api.instantQuery(METRIC_DEMO_FLEET_RPS),
        api.instantQuery(METRIC_DEMO_FLEET_P95),
        api.instantQuery(METRIC_DEMO_FLEET_TOKENS),
        api.instantQuery(METRIC_DEMO_FLEET_ERR_PCT),
      ]);
      return {
        rps: firstValue(rps),
        p95: firstValue(p95),
        tokens: firstValue(tokens),
        err: firstValue(err),
      };
    },
    KPI_POLL_MS,
    [demoOn],
  );

  // 플릿 부하·지연 추이 (최근 3시간, 30s) — 두 선이 함께 움직이는 것이 목적.
  const fleetTrend = usePolledQuery(
    async () => {
      if (!demoOn) return null;
      const { startSec, endSec, step } = rangeWindow(FLEET_RANGE_SECONDS);
      const [rpsRes, p95Res] = await Promise.all([
        api.rangeQuery(METRIC_DEMO_FLEET_RPS, startSec, endSec, step),
        api.rangeQuery(METRIC_DEMO_FLEET_P95, startSec, endSec, step),
      ]);
      return mergeMatrices([
        { key: 'rps', matrix: rpsRes.data },
        { key: 'p95', matrix: p95Res.data },
      ]);
    },
    RANGE_POLL_MS,
    [demoOn],
  );

  // 선택 풀 상세 추이 (최근 1시간, 30s) — 지연 3선 + 큐·배치 2선을 한 폴링에서.
  const poolTrend = usePolledQuery(
    async () => {
      if (!demoOn || activePool === '') return null;
      const { startSec, endSec, step } = rangeWindow(POOL_RANGE_SECONDS);
      const labels = { pool: activePool };
      const [p50, p95, p99, queue, batch] = await Promise.all([
        api.rangeQuery(selector(METRIC_DEMO_POOL_P50, labels), startSec, endSec, step),
        api.rangeQuery(selector(METRIC_DEMO_POOL_P95, labels), startSec, endSec, step),
        api.rangeQuery(selector(METRIC_DEMO_POOL_P99, labels), startSec, endSec, step),
        api.rangeQuery(selector(METRIC_DEMO_POOL_QUEUE, labels), startSec, endSec, step),
        api.rangeQuery(selector(METRIC_DEMO_POOL_BATCH, labels), startSec, endSec, step),
      ]);
      return {
        latency: mergeMatrices([
          { key: 'p50', matrix: p50.data },
          { key: 'p95', matrix: p95.data },
          { key: 'p99', matrix: p99.data },
        ]),
        load: mergeMatrices([
          { key: 'queue', matrix: queue.data },
          { key: 'batch', matrix: batch.data },
        ]),
      };
    },
    RANGE_POLL_MS,
    [demoOn, activePool],
  );

  if (demoMode === null) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!demoOn) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>서빙 · SLO</CardTitle>
          <CardDescription>서빙 지표는 데모 인스턴스 전용</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-sm">
            요청·지연·토큰·큐와 SLO 는 데모 엔진이 합성하는 추론 서빙 지표다. 실서비스
            인스턴스에는 해당 시리즈가 존재하지 않으므로 이 화면은 비활성이다 — GPU 하드웨어
            지표는 GPU 플릿·헬스 화면에서 확인한다.
          </p>
        </CardContent>
      </Card>
    );
  }

  const fleetRows = fleetTrend.data ?? [];
  const errPct = kpi.data?.err ?? null;
  const latencyRows = poolTrend.data?.latency ?? [];
  const loadRows = poolTrend.data?.load ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="font-semibold text-xl">서빙 · SLO</h1>
        <p className="text-muted-foreground text-sm">
          플릿에서 실제로 돌고 있는 추론 서비스의 요청·지연·큐와 가용성 목표를 함께 본다.
        </p>
      </div>

      {/* 1. 플릿 서빙 KPI */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiTile
          label="초당 요청 (RPS)"
          value={kpi.data === null ? null : formatRps(kpi.data.rps)}
          hint="전체 서빙 풀 합계"
          tone="info"
        />
        <KpiTile
          label="p95 지연"
          value={kpi.data === null ? null : formatMs(kpi.data.p95)}
          hint="요청량 가중 평균"
        />
        <KpiTile
          label="토큰 생성"
          value={kpi.data === null ? null : formatTokensPerSec(kpi.data.tokens)}
          hint="LLM·RAG 계열 합계"
        />
        <KpiTile
          label="에러율"
          value={kpi.data === null ? null : formatPct(errPct, 2)}
          hint={`경고 임계 ${ERROR_RATE_WARN_PCT}%`}
          tone={errPct !== null && errPct > ERROR_RATE_WARN_PCT ? 'danger' : 'ok'}
        />
      </div>

      {/* 2. 플릿 지연·요청 추이 — 부하와 지연이 함께 움직이는 것을 보이는 차트 */}
      <Card>
        <CardHeader>
          <CardTitle>요청량 · p95 지연 추이</CardTitle>
          <CardDescription>
            최근 3시간 — 요청이 몰리면 배치와 큐가 커지고 그만큼 p95 지연이 따라 오른다(히트맵이
            붉어지는 시각과 같은 시각).
          </CardDescription>
        </CardHeader>
        <CardContent>
          {fleetTrend.data === null && fleetTrend.error === null ? (
            <Skeleton className="h-64 w-full" />
          ) : fleetRows.length === 0 ? (
            <p className="py-8 text-center text-muted-foreground text-sm">표시할 데이터가 없다.</p>
          ) : (
            <ResponsiveContainer width="100%" height={260}>
              <LineChart data={fleetRows} margin={CHART_MARGIN}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                <XAxis
                  dataKey="time"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={formatClock}
                  tick={{ fontSize: 12 }}
                  className="fill-muted-foreground"
                />
                <YAxis
                  yAxisId="rps"
                  domain={['auto', 'auto']}
                  width={56}
                  tick={{ fontSize: 12 }}
                  className="fill-muted-foreground"
                />
                <YAxis
                  yAxisId="latency"
                  orientation="right"
                  domain={['auto', 'auto']}
                  width={56}
                  tick={{ fontSize: 12 }}
                  className="fill-muted-foreground"
                />
                <Tooltip
                  labelFormatter={(t) => formatClock(Number(t))}
                  formatter={(value, name) =>
                    name === 'p95 지연' ? formatMs(Number(value)) : formatRps(Number(value))
                  }
                  contentStyle={TOOLTIP_STYLE}
                />
                <Legend wrapperStyle={{ fontSize: 12 }} />
                <Line
                  yAxisId="rps"
                  type="monotone"
                  dataKey="rps"
                  name="초당 요청"
                  stroke="var(--metric-cpu)"
                  strokeWidth={2}
                  dot={false}
                  connectNulls={false}
                  isAnimationActive={false}
                />
                <Line
                  yAxisId="latency"
                  type="monotone"
                  dataKey="p95"
                  name="p95 지연"
                  stroke="var(--metric-thermal)"
                  strokeWidth={2}
                  dot={false}
                  connectNulls={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      {/* 3. SLO */}
      {state === null ? <Skeleton className="h-52 w-full" /> : <SloPanel slo={state.slo} />}

      {/* 4. 서빙 풀 테이블 */}
      <Card>
        <CardHeader>
          <CardTitle>서빙 풀</CardTitle>
          <CardDescription>
            요청량 순 — 행을 고르면 아래에 해당 풀의 지연 분포와 큐·배치가 열린다.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {state === null ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>풀</TableHead>
                  <TableHead>테넌트</TableHead>
                  <TableHead className="text-right">GPU</TableHead>
                  <TableHead className="text-right">사용률</TableHead>
                  <TableHead className="text-right">RPS</TableHead>
                  <TableHead className="text-right">p50</TableHead>
                  <TableHead className="text-right">p95</TableHead>
                  <TableHead className="text-right">p99</TableHead>
                  <TableHead className="text-right">큐</TableHead>
                  <TableHead className="text-right">배치</TableHead>
                  <TableHead className="text-right">에러율</TableHead>
                  <TableHead className="text-right">레플리카</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {serving.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={12} className="text-center text-muted-foreground">
                      서빙 풀이 없다.
                    </TableCell>
                  </TableRow>
                ) : (
                  serving.map((s) => {
                    const degraded = s.replicasReady < s.replicasDesired;
                    return (
                      <TableRow
                        key={s.pool}
                        onClick={() => setPicked(s.pool)}
                        className={`cursor-pointer ${s.pool === activePool ? 'bg-muted' : ''} ${
                          s.saturated ? 'border-l-2' : ''
                        }`}
                        style={
                          s.saturated ? { borderLeftColor: 'var(--metric-thermal)' } : undefined
                        }
                      >
                        <TableCell className="text-xs">
                          <div className="flex items-center gap-1.5">
                            <span className="font-medium">{s.display}</span>
                            {s.saturated ? (
                              <Badge variant="outline" className="text-metric-thermal text-[10px]">
                                포화
                              </Badge>
                            ) : null}
                          </div>
                          <span className="font-mono text-[10px] text-muted-foreground">
                            {s.pool}
                          </span>
                        </TableCell>
                        <TableCell className="text-xs">{s.tenant}</TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatCount(s.gpus)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatPct(s.utilAvgPct)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatRps(s.rps)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatMs(s.p50Ms)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatMs(s.p95Ms)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatMs(s.p99Ms)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {s.queueDepth.toFixed(1)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {s.batchSize.toFixed(1)}
                        </TableCell>
                        <TableCell className="text-right text-xs tabular-nums">
                          {formatPct(s.errorRatePct, 2)}
                        </TableCell>
                        <TableCell
                          className={`text-right text-xs tabular-nums ${
                            degraded ? 'font-semibold text-metric-thermal' : ''
                          }`}
                        >
                          {s.replicasReady}/{s.replicasDesired}
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 5. 선택 풀 상세 */}
      <Card>
        <CardHeader>
          <CardTitle>
            {active ? `${active.display} 상세` : '풀 상세'}
            {active ? (
              <Badge variant="secondary" className="ml-2 align-middle">
                {active.tenant}
              </Badge>
            ) : null}
          </CardTitle>
          <CardDescription>
            최근 1시간 — 큐가 쌓이는 구간에서 p99 꼬리가 가장 먼저 벌어진다.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {active === null ? (
            <p className="py-8 text-center text-muted-foreground text-sm">
              표에서 풀을 선택한다.
            </p>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <KpiTile
                  label="토큰 생성"
                  value={
                    active.tokensPerSec > 0
                      ? formatTokensPerSec(active.tokensPerSec)
                      : '토큰 생성 없음'
                  }
                  hint={
                    active.tokensPerSec > 0 ? '생성형 응답 처리량' : '임베딩·분류 계열 (비생성)'
                  }
                />
                <KpiTile
                  label="레플리카"
                  value={`${active.replicasReady}/${active.replicasDesired}`}
                  hint={
                    active.replicasReady < active.replicasDesired
                      ? '준비 중인 파드 존재'
                      : '전량 Ready'
                  }
                  tone={active.replicasReady < active.replicasDesired ? 'warn' : 'ok'}
                />
                <KpiTile
                  label="에러율"
                  value={formatPct(active.errorRatePct, 2)}
                  hint={active.saturated ? '포화 — 타임아웃·거절 발생 구간' : '정상 범위'}
                  tone={active.errorRatePct > ERROR_RATE_WARN_PCT ? 'danger' : 'ok'}
                />
              </div>

              <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <span className="text-muted-foreground text-xs">백분위 지연 (ms)</span>
                  {poolTrend.data === null && poolTrend.error === null ? (
                    <Skeleton className="h-56 w-full" />
                  ) : latencyRows.length === 0 ? (
                    <p className="py-8 text-center text-muted-foreground text-sm">
                      표시할 데이터가 없다.
                    </p>
                  ) : (
                    <ResponsiveContainer width="100%" height={220}>
                      <LineChart data={latencyRows} margin={CHART_MARGIN}>
                        <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                        <XAxis
                          dataKey="time"
                          type="number"
                          domain={['dataMin', 'dataMax']}
                          tickFormatter={formatClock}
                          tick={{ fontSize: 12 }}
                          className="fill-muted-foreground"
                        />
                        <YAxis
                          domain={['auto', 'auto']}
                          width={56}
                          tick={{ fontSize: 12 }}
                          className="fill-muted-foreground"
                        />
                        <Tooltip
                          labelFormatter={(t) => formatClock(Number(t))}
                          formatter={(value) => formatMs(Number(value))}
                          contentStyle={TOOLTIP_STYLE}
                        />
                        <Legend wrapperStyle={{ fontSize: 12 }} />
                        <Line
                          type="monotone"
                          dataKey="p50"
                          name="p50"
                          stroke="var(--metric-cpu)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls={false}
                          isAnimationActive={false}
                        />
                        <Line
                          type="monotone"
                          dataKey="p95"
                          name="p95"
                          stroke="var(--metric-thermal)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls={false}
                          isAnimationActive={false}
                        />
                        <Line
                          type="monotone"
                          dataKey="p99"
                          name="p99"
                          stroke="var(--metric-fault)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls={false}
                          isAnimationActive={false}
                        />
                      </LineChart>
                    </ResponsiveContainer>
                  )}
                </div>

                <div className="flex flex-col gap-1">
                  <span className="text-muted-foreground text-xs">큐 깊이 · 배치 크기</span>
                  {poolTrend.data === null && poolTrend.error === null ? (
                    <Skeleton className="h-56 w-full" />
                  ) : loadRows.length === 0 ? (
                    <p className="py-8 text-center text-muted-foreground text-sm">
                      표시할 데이터가 없다.
                    </p>
                  ) : (
                    <ResponsiveContainer width="100%" height={220}>
                      <LineChart data={loadRows} margin={CHART_MARGIN}>
                        <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                        <XAxis
                          dataKey="time"
                          type="number"
                          domain={['dataMin', 'dataMax']}
                          tickFormatter={formatClock}
                          tick={{ fontSize: 12 }}
                          className="fill-muted-foreground"
                        />
                        <YAxis
                          domain={['auto', 'auto']}
                          width={56}
                          tick={{ fontSize: 12 }}
                          className="fill-muted-foreground"
                        />
                        <Tooltip
                          labelFormatter={(t) => formatClock(Number(t))}
                          formatter={(value) => Number(value).toFixed(1)}
                          contentStyle={TOOLTIP_STYLE}
                        />
                        <Legend wrapperStyle={{ fontSize: 12 }} />
                        <Line
                          type="monotone"
                          dataKey="queue"
                          name="큐 깊이"
                          stroke="var(--metric-mem)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls={false}
                          isAnimationActive={false}
                        />
                        <Line
                          type="monotone"
                          dataKey="batch"
                          name="배치 크기"
                          stroke="var(--metric-pod)"
                          strokeWidth={2}
                          dot={false}
                          connectNulls={false}
                          isAnimationActive={false}
                        />
                      </LineChart>
                    </ResponsiveContainer>
                  )}
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* 6. 클러스터 이벤트 */}
      {state === null ? <Skeleton className="h-64 w-full" /> : <EventStream events={state.events} />}

      <p className="text-muted-foreground text-xs">
        서빙 지표는 하드웨어 신호에서 파생된다 — 사용률이 오르면 배치·큐가 커지고 지연이 따라
        오르며, 부하 90% 를 넘긴 풀에서만 에러율이 유의미해진다.
      </p>
    </div>
  );
}
