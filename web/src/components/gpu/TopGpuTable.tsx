// TopGpuTable — demo1 "GPU 사용률 Top 10" 테이블. useFleet() 셀을 사용률
// 내림차순으로 잘라 GPU / 노드 / 워크로드 3개 탭으로 보여준다.
// 메모리·온도는 벽면 셀에 없으므로 mem_used/mem_total/temperature instant 를
// 각 1회 조회해 gpu_uuid 로 조인한다(30s 폴링, 응답 없으면 해당 칸은 '—').
import { useMemo } from 'react';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
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
import type { CellStatus, FleetCell } from '@/hooks/useFleet';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api } from '@/lib/api';
import {
  bandForUtil,
  formatCelsius,
  formatCount,
  formatPct,
  METRIC_GPU_MEM_TOTAL,
  METRIC_GPU_MEM_USED,
  METRIC_GPU_TEMP,
} from '@/lib/gpuMetrics';
import type { AlertEvent } from '@/lib/demoApi';

export interface TopGpuTableProps {
  /** 대상 셀(null = 로딩). 표는 스스로 벽면을 받지 않는다 — 호출부가 이미
   *  필터를 건 셀을 주므로, 표는 필터를 모른 채 필터에 반응한다. */
  cells: FleetCell[] | null;
  /** 표시할 상위 행 수 (기본 10) */
  limit?: number;
  /** 건강 상태 분류자 — 상태는 라벨이 아니라 장애·온도·격리 집합에서 나오므로
   *  그것을 아는 호출부가 주입한다. 미주입이면 기존 사용률 대역 라벨 유지. */
  statusOf?: (cell: FleetCell) => CellStatus;
  /** 이벤트 열 소재 — instance(+device) 로 매칭해 최근 알림 code 를 표기. */
  alerts?: AlertEvent[];
}

const DETAIL_POLL_MS = 30_000;

const STATUS_LABEL: Record<CellStatus, string> = {
  normal: '정상',
  degraded: '성능저하',
  fault: '장애',
  isolated: '격리',
  recovering: '복구중',
};

const STATUS_COLOR: Record<CellStatus, string> = {
  normal: 'var(--metric-pod)',
  degraded: 'var(--metric-thermal)',
  fault: 'var(--metric-fault)',
  isolated: 'var(--metric-mem)',
  recovering: 'var(--metric-cpu)',
};

interface GpuDetail {
  memPct?: number;
  tempC?: number;
}

/** useFleet 과 같은 uuid 파생 규칙 — 조인 키가 어긋나면 칸이 통째로 빈다. */
function detailKey(m: Record<string, string>): string {
  return m.gpu_uuid ?? `${m.instance}/${m.device}`;
}

/** 메모리·온도 조인 소스. 실패하면 null 로 남아 각 칸이 '—' 가 된다. */
function useGpuDetails(): Map<string, GpuDetail> | null {
  const query = usePolledQuery(async () => {
    const [used, total, temp] = await Promise.all([
      api.instantQuery(METRIC_GPU_MEM_USED),
      api.instantQuery(METRIC_GPU_MEM_TOTAL),
      api.instantQuery(METRIC_GPU_TEMP),
    ]);
    const usedBy = new Map(
      used.data.result.map((s) => [detailKey(s.metric), Number(s.value[1])] as const),
    );
    const map = new Map<string, GpuDetail>();
    for (const s of total.data.result) {
      const key = detailKey(s.metric);
      const cap = Number(s.value[1]);
      const u = usedBy.get(key);
      if (cap > 0 && u != null && Number.isFinite(u)) map.set(key, { memPct: (u / cap) * 100 });
    }
    for (const s of temp.data.result) {
      const key = detailKey(s.metric);
      map.set(key, { ...map.get(key), tempC: Number(s.value[1]) });
    }
    return map;
  }, DETAIL_POLL_MS);
  return query.data;
}

/** instance(+device) → 최근 알림 code. 오래된 것부터 넣어 최신이 덮어쓴다. */
function buildAlertIndex(alerts: AlertEvent[]): Map<string, string> {
  const idx = new Map<string, string>();
  for (const a of [...alerts].sort((x, y) => x.at - y.at)) {
    if (!a.instance) continue;
    if (a.device) idx.set(`${a.instance}/${a.device}`, a.code);
    idx.set(a.instance, a.code);
  }
  return idx;
}

function PctBar({ value, color }: { value: number | undefined; color: string }) {
  if (value == null || Number.isNaN(value)) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex items-center gap-2">
      <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
        <div
          className="h-full rounded-full"
          style={{ width: `${Math.min(100, Math.max(0, value))}%`, backgroundColor: color }}
        />
      </div>
      <span className="w-12 text-right tabular-nums">{formatPct(value, 0)}</span>
    </div>
  );
}

function StatusCell({ cell, statusOf }: { cell: FleetCell; statusOf?: TopGpuTableProps['statusOf'] }) {
  if (!statusOf) return <span className="text-muted-foreground">{bandForUtil(cell.util).label}</span>;
  const s = statusOf(cell);
  return (
    <Badge
      variant="outline"
      className="text-[10px]"
      style={{
        color: STATUS_COLOR[s],
        borderColor: STATUS_COLOR[s],
        backgroundColor: `color-mix(in srgb, ${STATUS_COLOR[s]} 12%, transparent)`,
      }}
    >
      {STATUS_LABEL[s]}
    </Badge>
  );
}

/** 노드·워크로드 탭의 집계 행 — 어느 축이든 (GPU 수, 평균 사용률) 이 본체다. */
interface GroupRow {
  key: string;
  label: string;
  detail: string;
  gpus: number;
  utilAvg: number;
}

/** 수집 중단 노드의 '데이터 없음' 셀은 util 이 NaN 이다 — 사용률 순위·평균에서
 *  빼야 한다. NaN 을 비교자에 넣으면 정렬이 통째로 무너지고(NaN 비교는 항상
 *  false) 평균은 NaN 으로 오염된다. 대수(gpus)에는 그대로 남는다 — 데이터가
 *  안 온다고 장비가 없어지는 건 아니다. */
function measured(c: FleetCell): boolean {
  return Number.isFinite(c.util);
}

function groupBy(cells: FleetCell[], pick: (c: FleetCell) => { key: string; detail: string }): GroupRow[] {
  const acc = new Map<string, { detail: string; gpus: number; sum: number; n: number }>();
  for (const c of cells) {
    const { key, detail } = pick(c);
    const cur = acc.get(key) ?? { detail, gpus: 0, sum: 0, n: 0 };
    cur.gpus += 1;
    if (measured(c)) {
      cur.sum += c.util;
      cur.n += 1;
    }
    acc.set(key, cur);
  }
  return [...acc.entries()]
    .map(([key, v]) => ({
      key,
      label: key,
      detail: v.detail,
      gpus: v.gpus,
      utilAvg: v.n > 0 ? v.sum / v.n : Number.NaN,
    }))
    .sort((a, b) => (b.utilAvg || 0) - (a.utilAvg || 0));
}

const WORKLOAD_LABEL: Record<string, string> = { training: '학습', inference: '추론' };

function GroupTable({ rows, headLabel, headDetail }: { rows: GroupRow[]; headLabel: string; headDetail: string }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10">순위</TableHead>
          <TableHead>{headLabel}</TableHead>
          <TableHead>{headDetail}</TableHead>
          <TableHead className="w-20 text-right">GPU 수</TableHead>
          <TableHead className="w-48">평균 사용률</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r, i) => (
          <TableRow key={r.key}>
            <TableCell className="text-muted-foreground tabular-nums">{i + 1}</TableCell>
            <TableCell className="font-medium">{r.label}</TableCell>
            <TableCell className="text-muted-foreground">{r.detail || '—'}</TableCell>
            <TableCell className="text-right tabular-nums">{formatCount(r.gpus)}</TableCell>
            <TableCell>
              <PctBar value={r.utilAvg} color="var(--metric-gpu)" />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export default function TopGpuTable({ cells, limit = 10, statusOf, alerts }: TopGpuTableProps) {
  const details = useGpuDetails();

  // 사용률 순위이므로 사용률이 없는 셀(수집 중단)은 애초에 후보가 아니다.
  const top = useMemo(
    () => (cells ?? []).filter(measured).sort((a, b) => b.util - a.util).slice(0, limit),
    [cells, limit],
  );
  const nodeRows = useMemo(
    () =>
      groupBy(cells ?? [], (c) => ({ key: c.instance, detail: c.cluster })).slice(0, limit),
    [cells, limit],
  );
  const poolRows = useMemo(
    () =>
      groupBy(cells ?? [], (c) => ({
        key: c.pool || '(미할당)',
        detail: [c.tenant, WORKLOAD_LABEL[c.workload] ?? ''].filter(Boolean).join(' · '),
      })).slice(0, limit),
    [cells, limit],
  );
  const alertIndex = useMemo(() => buildAlertIndex(alerts ?? []), [alerts]);

  return (
    <Card>
      <Tabs defaultValue="gpu">
        <CardHeader className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle>GPU 사용률 Top {limit}</CardTitle>
          <TabsList>
            <TabsTrigger value="gpu">GPU</TabsTrigger>
            <TabsTrigger value="node">노드</TabsTrigger>
            <TabsTrigger value="workload">워크로드</TabsTrigger>
          </TabsList>
        </CardHeader>
        <CardContent>
          {cells == null ? (
            <Skeleton className="h-64 w-full" />
          ) : (
            <>
              <TabsContent value="gpu">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-10">순위</TableHead>
                      <TableHead>GPU·노드</TableHead>
                      <TableHead>모델</TableHead>
                      <TableHead>클러스터</TableHead>
                      <TableHead>테넌트</TableHead>
                      <TableHead className="w-40">GPU 사용률</TableHead>
                      <TableHead className="w-36">메모리</TableHead>
                      <TableHead className="w-20">온도</TableHead>
                      <TableHead>상태</TableHead>
                      <TableHead>이벤트</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {top.map((c, i) => {
                      const d = details?.get(c.uuid);
                      return (
                        <TableRow key={c.uuid}>
                          <TableCell className="text-muted-foreground tabular-nums">{i + 1}</TableCell>
                          <TableCell className="font-medium">
                            {c.device}
                            <span className="text-muted-foreground"> · {c.instance}</span>
                          </TableCell>
                          <TableCell>{c.model.replace(/^NVIDIA\s+/, '')}</TableCell>
                          <TableCell>{c.cluster}</TableCell>
                          <TableCell>{c.tenant}</TableCell>
                          <TableCell>
                            <PctBar value={c.util} color="var(--metric-gpu)" />
                          </TableCell>
                          <TableCell>
                            <PctBar value={d?.memPct} color="var(--metric-mem)" />
                          </TableCell>
                          <TableCell className="tabular-nums">
                            {d?.tempC == null ? (
                              <span className="text-muted-foreground">—</span>
                            ) : (
                              formatCelsius(d.tempC)
                            )}
                          </TableCell>
                          <TableCell>
                            <StatusCell cell={c} statusOf={statusOf} />
                          </TableCell>
                          <TableCell className="font-mono text-muted-foreground text-xs">
                            {alertIndex.get(`${c.instance}/${c.device}`) ??
                              alertIndex.get(c.instance) ??
                              '-'}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </TabsContent>

              <TabsContent value="node">
                <GroupTable rows={nodeRows} headLabel="노드" headDetail="클러스터" />
              </TabsContent>

              <TabsContent value="workload">
                <GroupTable rows={poolRows} headLabel="워크로드" headDetail="테넌트 · 종류" />
              </TabsContent>
            </>
          )}
        </CardContent>
      </Tabs>
    </Card>
  );
}
