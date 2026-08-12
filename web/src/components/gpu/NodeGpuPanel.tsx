// NodeGpuPanel — 토폴로지 그래프 옆에 상주하는 워커 노드 실시간 GPU 상세 패널.
//
// NodeDrillSheet 와 역할이 다르다. 그쪽은 클릭 시 밀려나오는 *자산 대장*
// (시리얼·펌웨어·랙·K8s 라벨)이고, 여기는 "선택된 노드가 지금 어떤가"만 4탭으로
// 항상 띄워 둔다. 자산까지 필요해지면 [상세 보기 →] 로 그쪽에 넘긴다.
//
// 데이터 출처는 둘이다. 구성(GPU 수·상태·풀/테넌트/워크로드)은 props 로 받은
// 벽면 셀에서 집계하고(새 API 없음), 실시간 수치(사용률·온도·전력·메모리)와
// 30분 추이는 instance 등호 매처 질의로 15초 폴링한다. instance=null 이면 질의
// 자체를 skip 한다 — usePolledQuery 의 enabled 대체 패턴(재구독 신호 deps=[instance]).
import { useMemo } from 'react';
import Sparkline from '@/components/gpu/Sparkline';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import type { CellStatus, FleetCell } from '@/hooks/useFleet';
import { api, type InstantSample } from '@/lib/api';
import type { AlertEvent, ClusterEvent } from '@/lib/demoApi';
import {
  bandForUtil,
  formatCelsius,
  formatCount,
  formatPct,
  formatWatts,
  METRIC_COLORS,
  METRIC_GPU_MEM_TOTAL,
  METRIC_GPU_MEM_USED,
  METRIC_GPU_POWER,
  METRIC_GPU_TEMP,
  METRIC_GPU_UTIL,
  selector,
} from '@/lib/gpuMetrics';

export interface NodeGpuPanelProps {
  /** 선택된 노드. null 이면 안내 문구만 띄운다. */
  instance: string | null;
  /** 전체 벽면 셀 — 이 컴포넌트가 instance 로 걸러 쓴다. */
  cells: FleetCell[];
  /** 셀 상태 분류자(장애·격리는 라벨이 아니라 시나리오 집합에서 나온다). */
  statusOf?: (c: FleetCell) => CellStatus;
  alerts?: AlertEvent[];
  events?: ClusterEvent[];
  /** [상세 보기 →] — 부모가 NodeDrillSheet(자산 대장)를 연다. */
  onOpenDetail?: (instance: string) => void;
}

const POLL_MS = 15_000;
const TREND_SEC = 1800; // 최근 30분
const TREND_STEP = 60;
const MAX_FEED = 10; // 이벤트 탭 표시 상한

const STATUS_META: Record<CellStatus, { label: string; color: string }> = {
  normal: { label: '정상', color: 'var(--metric-pod)' },
  degraded: { label: '성능 저하', color: 'var(--metric-thermal)' },
  fault: { label: '장애', color: 'var(--metric-fault)' },
  isolated: { label: '격리', color: 'var(--metric-fault)' },
  recovering: { label: '복구 중', color: 'var(--metric-cpu)' },
};

/** 심각한 것부터 — 노드 대표 상태는 이 순서의 첫 히트다. */
const STATUS_ORDER: CellStatus[] = ['fault', 'isolated', 'degraded', 'recovering', 'normal'];

const SEVERITY_COLOR: Record<AlertEvent['severity'], string> = {
  critical: METRIC_COLORS.fault,
  major: METRIC_COLORS.thermal,
  warning: METRIC_COLORS.mem,
  info: METRIC_COLORS.cpu,
};

const WORKLOAD_LABEL: Record<string, string> = {
  inference: '추론',
  training: '학습',
};

/** "NVIDIA H200" → "H200" — GpuModelBar·IdleDonut 과 동일 규칙. */
function shortModel(model: string): string {
  return model.replace(/^NVIDIA\s+/, '');
}

function relTime(at: number): string {
  const min = Math.round((Date.now() - at) / 60_000);
  if (min < 1) return '방금';
  if (min < 60) return `${min}분 전`;
  const hour = Math.floor(min / 60);
  return hour < 24 ? `${hour}시간 전` : `${Math.floor(hour / 24)}일 전`;
}

function sampleValue(s: InstantSample): number | null {
  const v = Number(s.value[1]);
  return Number.isFinite(v) ? v : null;
}

interface DeviceLive {
  device: string;
  util: number | null;
  temp: number | null;
  power: number | null;
  memUsed: number | null;
  memTotal: number | null;
}

interface NodeLive {
  /** 어느 노드의 결과인지 — 노드 전환 직후 이전 노드 값 오표시 방지. */
  instance: string;
  devices: Map<string, DeviceLive>;
  /** 최근 30분 GPU 평균 사용률(시점별 device 평균). */
  trend: number[];
}

/** 지표 한 줄 — 목업의 좌 라벨 / 우 값 정의행. */
function MetricRow({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5">
      <span className="shrink-0 text-muted-foreground text-sm">{label}</span>
      <span className="min-w-0 truncate text-right font-medium text-sm tabular-nums">
        {value}
        {hint ? <span className="ml-1.5 font-normal text-muted-foreground text-xs">{hint}</span> : null}
      </span>
    </div>
  );
}

function StatusBadge({ status }: { status: CellStatus }) {
  const meta = STATUS_META[status];
  return (
    <Badge
      variant="outline"
      className="text-[10px]"
      style={{ color: meta.color, borderColor: meta.color }}
    >
      {meta.label}
    </Badge>
  );
}

export default function NodeGpuPanel({
  instance,
  cells,
  statusOf,
  alerts,
  events,
  onOpenDetail,
}: NodeGpuPanelProps) {
  // 7,000셀 전량 스캔은 instance/cells 가 바뀔 때만.
  const nodeCells = useMemo(
    () => (instance ? cells.filter((c) => c.instance === instance) : []),
    [cells, instance],
  );

  const counts = useMemo(() => {
    const out: Record<CellStatus, number> = {
      normal: 0,
      degraded: 0,
      fault: 0,
      isolated: 0,
      recovering: 0,
    };
    for (const c of nodeCells) out[statusOf?.(c) ?? 'normal'] += 1;
    return out;
  }, [nodeCells, statusOf]);

  const nodeStatus = STATUS_ORDER.find((s) => counts[s] > 0) ?? 'normal';
  const head = nodeCells[0];

  const query = usePolledQuery<NodeLive | null>(
    async () => {
      if (!instance) return null;
      const end = Math.floor(Date.now() / 1000);
      const [utilRes, tempRes, powerRes, memUsedRes, memTotalRes, trendRes] = await Promise.all([
        api.instantQuery(selector(METRIC_GPU_UTIL, { instance })),
        api.instantQuery(selector(METRIC_GPU_TEMP, { instance })),
        api.instantQuery(selector(METRIC_GPU_POWER, { instance })),
        api.instantQuery(selector(METRIC_GPU_MEM_USED, { instance })),
        api.instantQuery(selector(METRIC_GPU_MEM_TOTAL, { instance })),
        api.rangeQuery(
          selector(METRIC_GPU_UTIL, { instance }),
          end - TREND_SEC,
          end,
          TREND_STEP,
        ),
      ]);

      // device 라벨 기준 5메트릭 join — 결측 시리즈는 null 로 남긴다.
      const devices = new Map<string, DeviceLive>();
      const rowOf = (device: string): DeviceLive => {
        let row = devices.get(device);
        if (!row) {
          row = { device, util: null, temp: null, power: null, memUsed: null, memTotal: null };
          devices.set(device, row);
        }
        return row;
      };
      const fill = (samples: InstantSample[], assign: (r: DeviceLive, v: number | null) => void) => {
        for (const s of samples) assign(rowOf(s.metric.device ?? 'default'), sampleValue(s));
      };
      fill(utilRes.data.result, (r, v) => {
        r.util = v;
      });
      fill(tempRes.data.result, (r, v) => {
        r.temp = v;
      });
      fill(powerRes.data.result, (r, v) => {
        r.power = v;
      });
      fill(memUsedRes.data.result, (r, v) => {
        r.memUsed = v;
      });
      fill(memTotalRes.data.result, (r, v) => {
        r.memTotal = v;
      });

      // 추이는 device 시리즈 8개를 시점 단위로 평균낸다 — 시리즈마다 표본
      // 개수가 다를 수 있으므로 인덱스가 아니라 타임스탬프로 묶는다.
      const buckets = new Map<number, { sum: number; n: number }>();
      for (const series of trendRes.data.result) {
        for (const [ts, raw] of series.values) {
          const v = Number(raw);
          if (!Number.isFinite(v)) continue;
          const b = buckets.get(ts);
          if (b) {
            b.sum += v;
            b.n += 1;
          } else {
            buckets.set(ts, { sum: v, n: 1 });
          }
        }
      }
      const trend = [...buckets.entries()]
        .sort((a, b) => a[0] - b[0])
        .map(([, b]) => b.sum / b.n);

      return { instance, devices, trend };
    },
    POLL_MS,
    [instance],
  );

  // 노드 전환 직후 이전 노드 결과를 새 헤더 아래 보이지 않게 — 불일치는 로딩 취급.
  const live = query.data && query.data.instance === instance ? query.data : null;

  // 노드 롤업 — 실시간 표본이 오기 전에는 셀의 사용률로 버틴다.
  const rollup = useMemo(() => {
    const rows = live ? [...live.devices.values()] : [];
    const avg = (vs: (number | null)[]) => {
      const ok = vs.filter((v): v is number => v != null);
      return ok.length > 0 ? ok.reduce((s, v) => s + v, 0) / ok.length : null;
    };
    const sum = (vs: (number | null)[]) => {
      const ok = vs.filter((v): v is number => v != null);
      return ok.length > 0 ? ok.reduce((s, v) => s + v, 0) : null;
    };
    const memUsed = sum(rows.map((r) => r.memUsed));
    const memTotal = sum(rows.map((r) => r.memTotal));
    const cellAvg =
      nodeCells.length > 0 ? nodeCells.reduce((s, c) => s + c.util, 0) / nodeCells.length : null;
    return {
      util: avg(rows.map((r) => r.util)) ?? cellAvg,
      temp: avg(rows.map((r) => r.temp)),
      power: sum(rows.map((r) => r.power)),
      memPct: memUsed != null && memTotal ? (memUsed / memTotal) * 100 : null,
    };
  }, [live, nodeCells]);

  const workloads = useMemo(() => {
    const out = new Map<string, { pool: string; tenant: string; workload: string; count: number }>();
    for (const c of nodeCells) {
      const key = `${c.pool}|${c.tenant}|${c.workload}`;
      const row = out.get(key);
      if (row) row.count += 1;
      else out.set(key, { pool: c.pool, tenant: c.tenant, workload: c.workload, count: 1 });
    }
    return [...out.values()].sort((a, b) => b.count - a.count);
  }, [nodeCells]);

  const feed = useMemo(() => {
    if (!instance) return [];
    const fromAlerts = (alerts ?? [])
      .filter((a) => a.instance === instance)
      .map((a) => ({
        key: `a${a.id}-${a.at}`,
        at: a.at,
        color: SEVERITY_COLOR[a.severity],
        title: a.title,
        sub: [a.code, a.device, a.detail].filter(Boolean).join(' · '),
      }));
    const fromEvents = (events ?? [])
      .filter((e) => e.node === instance)
      .map((e, i) => ({
        key: `e${e.at}-${i}`,
        at: e.at,
        color: e.type === 'Warning' ? METRIC_COLORS.thermal : METRIC_COLORS.cpu,
        title: e.reason,
        sub: [e.object, e.message].filter(Boolean).join(' · '),
      }));
    return [...fromAlerts, ...fromEvents].sort((a, b) => b.at - a.at).slice(0, MAX_FEED);
  }, [alerts, events, instance]);

  if (!instance) {
    return (
      <Card className="h-full">
        <CardHeader>
          <CardTitle className="text-base">노드 상세</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="py-8 text-center text-muted-foreground text-sm">
            토폴로지에서 노드를 선택하면 실시간 상태가 여기 표시된다.
          </p>
        </CardContent>
      </Card>
    );
  }

  const loading = live === null && nodeCells.length === 0;
  const statusHint = feed[0] ? relTime(feed[0].at) : undefined;

  // 상태 분해 문구 — 0인 분류는 지운다("정상 6 / 장애 1 / 격리 1").
  const breakdown = STATUS_ORDER.filter((s) => counts[s] > 0)
    .map((s) => `${STATUS_META[s].label} ${counts[s]}`)
    .join(' / ');

  return (
    <Card className="flex h-full min-h-0 flex-col">
      <CardHeader className="gap-1 pb-3">
        <div className="flex items-center gap-2">
          <CardTitle className="min-w-0 flex-1 truncate font-mono text-base" title={instance}>
            {instance}
          </CardTitle>
          <StatusBadge status={nodeStatus} />
        </div>
        <CardDescription className="truncate">
          {[head?.cluster, head?.nodePool].filter(Boolean).join(' / ') || '구성 정보 조회 중…'}
        </CardDescription>
      </CardHeader>

      <CardContent className="flex min-h-0 flex-1 flex-col">
        <Tabs defaultValue="overview" className="flex min-h-0 flex-1 flex-col">
          <TabsList className="w-full">
            <TabsTrigger value="overview">개요</TabsTrigger>
            <TabsTrigger value="gpu">GPU</TabsTrigger>
            <TabsTrigger value="workload">워크로드</TabsTrigger>
            <TabsTrigger value="events">이벤트</TabsTrigger>
          </TabsList>

          {/* ---- 개요 ---- */}
          <TabsContent value="overview" className="min-h-0 overflow-y-auto pt-2">
            {loading ? (
              <div className="flex flex-col gap-2">
                <Skeleton className="h-6 w-full" />
                <Skeleton className="h-6 w-full" />
                <Skeleton className="h-6 w-full" />
                <Skeleton className="h-14 w-full" />
              </div>
            ) : (
              <>
                <dl className="divide-y divide-border">
                  <MetricRow
                    label="GPU"
                    value={`${formatCount(nodeCells.length)}장`}
                    hint={breakdown ? `(${breakdown})` : undefined}
                  />
                  <MetricRow
                    label="상태"
                    value={STATUS_META[nodeStatus].label}
                    hint={statusHint}
                  />
                  <MetricRow label="GPU 평균 사용률" value={formatPct(rollup.util, 0)} />
                  <MetricRow label="GPU 메모리" value={formatPct(rollup.memPct, 0)} />
                  <MetricRow label="온도 평균" value={formatCelsius(rollup.temp)} />
                  <MetricRow label="전력 합계" value={formatWatts(rollup.power)} />
                </dl>

                <div className="mt-3">
                  <p className="mb-1 text-[11px] text-muted-foreground">
                    최근 30분 GPU 평균 사용률
                  </p>
                  <Sparkline values={live?.trend ?? []} color={METRIC_COLORS.gpu} height={40} />
                </div>

                {onOpenDetail ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="mt-3 w-full"
                    onClick={() => onOpenDetail(instance)}
                  >
                    상세 보기 →
                  </Button>
                ) : null}
              </>
            )}
          </TabsContent>

          {/* ---- GPU ---- */}
          <TabsContent value="gpu" className="min-h-0 overflow-y-auto pt-2">
            {nodeCells.length === 0 ? (
              live === null ? (
                <Skeleton className="h-40 w-full" />
              ) : (
                <p className="py-6 text-center text-muted-foreground text-sm">
                  이 노드의 GPU 시리즈가 없다.
                </p>
              )
            ) : (
              <ul className="flex flex-col gap-1.5">
                {nodeCells.map((c) => {
                  const l = live?.devices.get(c.device);
                  const util = l?.util ?? c.util;
                  const band = bandForUtil(util);
                  const status = statusOf?.(c) ?? 'normal';
                  return (
                    <li
                      key={c.uuid || c.device}
                      className="flex items-center gap-3 rounded-md border bg-card p-2"
                    >
                      <div className="min-w-0 flex-1">
                        <p className="truncate font-medium font-mono text-xs">{c.device}</p>
                        <p className="truncate text-[11px] text-muted-foreground">
                          {shortModel(c.model) || '—'}
                          {l?.temp != null ? ` · ${formatCelsius(l.temp)}` : ''}
                        </p>
                      </div>
                      <div className="w-24 shrink-0">
                        <div className="mb-1 text-right text-[11px] font-medium tabular-nums">
                          {formatPct(util, 0)}
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                          <div
                            className="h-full rounded-full"
                            style={{
                              width: `${Math.max(Math.min(util, 100), 1)}%`,
                              backgroundColor: `var(--util-band-${band.index})`,
                            }}
                          />
                        </div>
                      </div>
                      <StatusBadge status={status} />
                    </li>
                  );
                })}
              </ul>
            )}
          </TabsContent>

          {/* ---- 워크로드 ---- */}
          <TabsContent value="workload" className="min-h-0 overflow-y-auto pt-2">
            {workloads.length === 0 ? (
              <p className="py-6 text-center text-muted-foreground text-sm">
                이 노드의 워크로드 정보가 없다.
              </p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {workloads.map((w) => (
                  <li
                    key={`${w.pool}|${w.tenant}|${w.workload}`}
                    className="flex items-center gap-3 rounded-md border bg-card p-2"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-medium text-sm">{w.pool || '미할당'}</p>
                      <p className="truncate text-[11px] text-muted-foreground">
                        {w.tenant || '테넌트 없음'}
                      </p>
                    </div>
                    <Badge
                      variant="outline"
                      className="shrink-0 text-[10px]"
                      style={
                        w.workload
                          ? {
                              color: METRIC_COLORS.gpu,
                              borderColor: METRIC_COLORS.gpu,
                            }
                          : undefined
                      }
                    >
                      {WORKLOAD_LABEL[w.workload] ?? '미할당'}
                    </Badge>
                    <span className="shrink-0 font-medium text-sm tabular-nums">
                      {formatCount(w.count)}장
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </TabsContent>

          {/* ---- 이벤트 ---- */}
          <TabsContent value="events" className="min-h-0 overflow-y-auto pt-2">
            {feed.length === 0 ? (
              <p className="py-6 text-center text-muted-foreground text-sm">
                이 노드 관련 이벤트 없음
              </p>
            ) : (
              <ol className="pr-1">
                {feed.map((item) => (
                  <li key={item.key} className="relative ml-1.5 border-border border-l pb-3 pl-4 last:pb-0">
                    <span
                      className="absolute top-1 -left-[5px] size-2.5 rounded-full ring-2 ring-card"
                      style={{ backgroundColor: item.color }}
                    />
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-[11px] text-muted-foreground tabular-nums">
                        {new Date(item.at).toLocaleTimeString('ko-KR')}
                      </span>
                      <span className="text-[11px] text-muted-foreground">{relTime(item.at)}</span>
                    </div>
                    <p className="text-sm">{item.title}</p>
                    {item.sub ? (
                      <p className="break-all text-[11px] text-muted-foreground">{item.sub}</p>
                    ) : null}
                  </li>
                ))}
              </ol>
            )}
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  );
}
