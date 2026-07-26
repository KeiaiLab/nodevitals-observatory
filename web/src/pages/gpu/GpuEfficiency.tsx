// GpuEfficiency — Step 6 "효율·활용" (경영 설득 핵심). demo on/off 모두 동작:
// on = KPI 대비 + 갭 시계열 + 저활용(pool 배정분) + 30일+ 미사용, off = 실데이터
// 저활용 탐지만(강등). 차트는 Explorer 의 CSP-안전 Recharts 셋업 미러.
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
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useDemo } from '@/hooks/demoContext';
import { useFleet } from '@/hooks/useFleet';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type ApiEnvelope, type MatrixData, type VectorData } from '@/lib/api';
import {
  formatPct,
  METRIC_DEMO_FLEET_ALLOC_PCT,
  METRIC_DEMO_FLEET_UTIL_AVG,
} from '@/lib/gpuMetrics';

const KPI_POLL_MS = 15_000;
const GAP_POLL_MS = 60_000;
const GAP_RANGE_SECONDS = 24 * 3600;
const GAP_MIN_STEP_SECONDS = 60;
const GAP_TARGET_POINTS = 500;
const LOW_UTIL_THRESHOLD = 30;
const LOW_UTIL_ROWS = 50;

// 장기 미사용 사유 옵션 — register-idle-reason 액션 페이로드로 그대로 전송.
const IDLE_REASONS = ['서비스 출시 지연', '이용자 수 부족', '점검·정비', '향후 사용 예정'];

function firstValue(env: ApiEnvelope<VectorData>): number | null {
  const sample = env.data.result[0];
  return sample ? Number(sample.value[1]) : null;
}

function formatDayTick(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleString('ko-KR', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

interface GapRow {
  time: number;
  alloc: number | null;
  util: number | null;
}

// 두 matrix 를 공통 시간축 행으로 병합 — 빠진 버킷은 null 로 명시해
// connectNulls=false 가 그 지점만 끊어 그리게 한다(Explorer 관례).
function mergeGapRows(alloc: MatrixData, util: MatrixData): GapRow[] {
  const byTime = new Map<number, GapRow>();
  const put = (matrix: MatrixData, key: 'alloc' | 'util') => {
    for (const s of matrix.result) {
      for (const [t, v] of s.values) {
        const row = byTime.get(t) ?? { time: t, alloc: null, util: null };
        row[key] = Number(v);
        byTime.set(t, row);
      }
    }
  };
  put(alloc, 'alloc');
  put(util, 'util');
  return [...byTime.values()].sort((a, b) => a.time - b.time);
}

export default function GpuEfficiency() {
  const { demoMode, state, act } = useDemo();
  const demoOn = demoMode === true;
  const { cells } = useFleet();

  // 오탐 피드백 접수분(로컬) — 폴링과 무관하게 즉시 배지 반영.
  const [feedbackDone, setFeedbackDone] = useState<ReadonlySet<string>>(new Set());
  // 미사용 사유 등록분(로컬 낙관 반영) — 서버 상태는 다음 폴링이 재확인한다.
  const [reasonOverlay, setReasonOverlay] = useState<Record<string, string>>({});

  // KPI: 할당율 vs 실사용률 instant (demo 전용, 15s).
  const kpiQuery = usePolledQuery(
    async () => {
      if (!demoOn) return null;
      const [allocRes, utilRes] = await Promise.all([
        api.instantQuery(METRIC_DEMO_FLEET_ALLOC_PCT),
        api.instantQuery(METRIC_DEMO_FLEET_UTIL_AVG),
      ]);
      return { alloc: firstValue(allocRes), util: firstValue(utilRes) };
    },
    KPI_POLL_MS,
    [demoOn],
  );

  // 갭 시계열: 최근 24h 2시리즈 (demo 전용) — step=max(60, floor(86400/500)).
  const gapQuery = usePolledQuery(
    async () => {
      if (!demoOn) return null;
      const endSec = Math.floor(Date.now() / 1000);
      const startSec = endSec - GAP_RANGE_SECONDS;
      const step = Math.max(
        GAP_MIN_STEP_SECONDS,
        Math.floor(GAP_RANGE_SECONDS / GAP_TARGET_POINTS),
      );
      const [allocRes, utilRes] = await Promise.all([
        api.rangeQuery(METRIC_DEMO_FLEET_ALLOC_PCT, startSec, endSec, step),
        api.rangeQuery(METRIC_DEMO_FLEET_UTIL_AVG, startSec, endSec, step),
      ]);
      return mergeGapRows(allocRes.data, utilRes.data);
    },
    GAP_POLL_MS,
    [demoOn],
  );

  // 저활용 탐지 — demo on 은 pool 배정분만(회수 검토 의미), off 는 전체 강등.
  const lowUtil = useMemo(() => {
    if (!cells) return null;
    const filtered = cells.filter(
      (c) => c.util < LOW_UTIL_THRESHOLD && (!demoOn || c.pool !== ''),
    );
    filtered.sort((a, b) => a.util - b.util);
    return { total: filtered.length, rows: filtered.slice(0, LOW_UTIL_ROWS) };
  }, [cells, demoOn]);

  // 장기 미사용 — idleDays 내림차순 (demo 전용 데이터).
  const idleRows = useMemo(() => {
    const idle = state?.idle ?? [];
    return [...idle].sort((a, b) => b.idleDays - a.idleDays);
  }, [state?.idle]);

  async function onFalsePositive(uuid: string) {
    const outcome = await act('report-false-positive', { uuid });
    if (outcome.ok) {
      setFeedbackDone((prev) => {
        const next = new Set(prev);
        next.add(uuid);
        return next;
      });
    }
  }

  async function onRegisterReason(uuid: string, reason: string) {
    const outcome = await act('register-idle-reason', { uuid, reason });
    if (outcome.ok) {
      setReasonOverlay((prev) => ({ ...prev, [uuid]: reason }));
    }
  }

  if (demoMode === null) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const alloc = kpiQuery.data?.alloc ?? null;
  const utilAvg = kpiQuery.data?.util ?? null;
  const gapPp = alloc !== null && utilAvg !== null ? alloc - utilAvg : null;
  const gapRows = gapQuery.data ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="font-semibold text-xl">효율·활용</h1>
        <p className="text-muted-foreground text-sm">
          할당 대비 실사용 갭을 실측해 회수 가능 용량을 드러낸다.
        </p>
      </div>

      {/* 1. KPI 스트립 — 할당율 vs 실사용률 대형 대비 (demo 전용) */}
      {demoOn ? (
        <Card>
          <CardContent className="flex flex-col gap-3 py-5">
            {kpiQuery.data === null && kpiQuery.error === null ? (
              <Skeleton className="h-20 w-full" />
            ) : (
              <div className="flex flex-wrap items-center justify-center gap-x-10 gap-y-4">
                <div className="flex flex-col items-center gap-1">
                  <span className="text-muted-foreground text-xs">할당율</span>
                  <span
                    className="font-semibold text-4xl tabular-nums sm:text-5xl"
                    style={{ color: 'var(--metric-cpu)' }}
                  >
                    {formatPct(alloc)}
                  </span>
                </div>
                <div className="flex flex-col items-center gap-1">
                  <Badge variant="destructive">
                    갭 {gapPp === null ? '—' : `${gapPp.toFixed(1)}pp`}
                  </Badge>
                  <span className="text-muted-foreground text-xs">할당 − 실사용</span>
                </div>
                <div className="flex flex-col items-center gap-1">
                  <span className="text-muted-foreground text-xs">실사용률 (평균)</span>
                  <span
                    className="font-semibold text-4xl tabular-nums sm:text-5xl"
                    style={{ color: 'var(--metric-gpu)' }}
                  >
                    {formatPct(utilAvg)}
                  </span>
                </div>
              </div>
            )}
            <p className="text-center text-muted-foreground text-sm">
              할당은 가득, 실사용은 갭 — 회수 가능 용량이 잠들어 있다
            </p>
          </CardContent>
        </Card>
      ) : null}

      {/* 2. 갭 시계열 차트 (demo 전용) */}
      {demoOn ? (
        <Card>
          <CardHeader>
            <CardTitle>할당율 vs 실사용률 추이</CardTitle>
            <CardDescription>최근 24시간 — 두 선 사이가 잠든 용량이다</CardDescription>
          </CardHeader>
          <CardContent>
            {gapQuery.data === null && gapQuery.error === null ? (
              <Skeleton className="h-64 w-full" />
            ) : gapRows.length === 0 ? (
              <p className="py-8 text-center text-muted-foreground text-sm">
                표시할 데이터가 없다.
              </p>
            ) : (
              <ResponsiveContainer width="100%" height={260}>
                <LineChart data={gapRows} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                  <XAxis
                    dataKey="time"
                    type="number"
                    domain={['dataMin', 'dataMax']}
                    tickFormatter={formatDayTick}
                    tick={{ fontSize: 12 }}
                    className="fill-muted-foreground"
                  />
                  <YAxis
                    domain={[0, 100]}
                    width={40}
                    tick={{ fontSize: 12 }}
                    className="fill-muted-foreground"
                  />
                  <Tooltip
                    labelFormatter={(t) => formatDayTick(Number(t))}
                    formatter={(value) => `${Number(value).toFixed(1)}%`}
                    contentStyle={{
                      backgroundColor: 'var(--popover)',
                      borderColor: 'var(--border)',
                      color: 'var(--popover-foreground)',
                      fontSize: 12,
                    }}
                  />
                  <Legend wrapperStyle={{ fontSize: 12 }} />
                  <Line
                    type="monotone"
                    dataKey="alloc"
                    name="할당율"
                    stroke="var(--metric-cpu)"
                    strokeWidth={2}
                    dot={false}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                  <Line
                    type="monotone"
                    dataKey="util"
                    name="실사용률"
                    stroke="var(--metric-gpu)"
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
      ) : null}

      {/* 3·4. 저활용 탐지 / 30일+ 미사용 */}
      <Tabs defaultValue="low-util">
        <TabsList>
          <TabsTrigger value="low-util">저활용 탐지</TabsTrigger>
          {demoOn ? <TabsTrigger value="idle">30일+ 미사용</TabsTrigger> : null}
        </TabsList>

        <TabsContent value="low-util">
          <Card>
            <CardHeader>
              <CardTitle>저활용 GPU</CardTitle>
              <CardDescription>
                {lowUtil === null
                  ? '집계 중…'
                  : `저활용 ${lowUtil.total.toLocaleString('ko-KR')}장 — 회수 검토 대상 (사용률 ${LOW_UTIL_THRESHOLD}% 미만${demoOn ? ' · 풀 배정분' : ''}, 상위 ${LOW_UTIL_ROWS}행 표시)`}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {lowUtil === null ? (
                <div className="flex flex-col gap-2">
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>인스턴스</TableHead>
                      <TableHead>디바이스</TableHead>
                      <TableHead>모델</TableHead>
                      <TableHead>풀</TableHead>
                      <TableHead className="text-right">사용률</TableHead>
                      <TableHead>상태</TableHead>
                      {demoOn ? <TableHead /> : null}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {lowUtil.rows.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={demoOn ? 7 : 6}
                          className="text-center text-muted-foreground"
                        >
                          저활용 GPU 가 없다.
                        </TableCell>
                      </TableRow>
                    ) : (
                      lowUtil.rows.map((c) => (
                        <TableRow key={c.uuid}>
                          <TableCell className="font-mono text-xs">{c.instance}</TableCell>
                          <TableCell className="font-mono text-xs">{c.device}</TableCell>
                          <TableCell className="text-xs">{c.model || '—'}</TableCell>
                          <TableCell className="text-xs">{c.pool || '—'}</TableCell>
                          <TableCell className="text-right text-xs tabular-nums">
                            {formatPct(c.util)}
                          </TableCell>
                          <TableCell>
                            {feedbackDone.has(c.uuid) ? (
                              <Badge variant="secondary">피드백 접수</Badge>
                            ) : (
                              <Badge variant="destructive">회수 검토</Badge>
                            )}
                          </TableCell>
                          {demoOn ? (
                            <TableCell className="text-right">
                              <Button
                                variant="outline"
                                size="sm"
                                disabled={feedbackDone.has(c.uuid)}
                                onClick={() => onFalsePositive(c.uuid)}
                              >
                                오탐 피드백
                              </Button>
                            </TableCell>
                          ) : null}
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {demoOn ? (
          <TabsContent value="idle">
            <Card>
              <CardHeader>
                <CardTitle>30일 이상 미사용 GPU</CardTitle>
                <CardDescription>
                  자동 회수는 범위 외 — 경보와 검토 대상 제공까지. 사유를 등록해 관리한다.
                </CardDescription>
              </CardHeader>
              <CardContent>
                {state === null ? (
                  <div className="flex flex-col gap-2">
                    <Skeleton className="h-8 w-full" />
                    <Skeleton className="h-8 w-full" />
                  </div>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>인스턴스</TableHead>
                        <TableHead>디바이스</TableHead>
                        <TableHead>모델</TableHead>
                        <TableHead>CSP</TableHead>
                        <TableHead className="text-right">미사용 일수</TableHead>
                        <TableHead className="w-44">사유</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {idleRows.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={6} className="text-center text-muted-foreground">
                            30일 이상 미사용 GPU 가 없다.
                          </TableCell>
                        </TableRow>
                      ) : (
                        idleRows.map((g) => {
                          const reason = reasonOverlay[g.uuid] ?? g.reason ?? '';
                          return (
                            <TableRow key={g.uuid}>
                              <TableCell className="font-mono text-xs">{g.instance}</TableCell>
                              <TableCell className="font-mono text-xs">{g.device}</TableCell>
                              <TableCell className="text-xs">{g.model}</TableCell>
                              <TableCell className="text-xs uppercase">{g.csp}</TableCell>
                              <TableCell className="text-right text-xs tabular-nums">
                                {g.idleDays}일
                              </TableCell>
                              <TableCell>
                                <Select
                                  value={reason}
                                  onValueChange={(v) => onRegisterReason(g.uuid, v)}
                                >
                                  <SelectTrigger
                                    className="w-40"
                                    size="sm"
                                    aria-label={`${g.instance} ${g.device} 미사용 사유`}
                                  >
                                    <SelectValue placeholder="사유 선택" />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {IDLE_REASONS.map((r) => (
                                      <SelectItem key={r} value={r}>
                                        {r}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
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
          </TabsContent>
        ) : null}
      </Tabs>

      <p className="text-muted-foreground text-xs">
        월별 활용계획 연계·회수 워크플로는 로드맵에서 다룬다.
      </p>
    </div>
  );
}
