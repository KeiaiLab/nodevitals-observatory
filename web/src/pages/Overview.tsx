// Overview — KPI 타일(노드 수·up/down·평균 load·GPU 온도·총 시리즈) + 노드 상태
// 테이블 (m5-design.md §4.4). 데이터 소스: GET /api/v1/query ×2(node_load1,
// gpuMetrics.METRIC_GPU_TEMP — 실 에이전트 nodevitals_hw_ 접두 계약) + GET /api/v1/series
// (두 메트릭 union — 총 시리즈 KPI + 1h 로스터). up/down 판정 = node_load1 instant
// vector 존재(5분 평가창) 여부 — roster(series, 1시간 창) 대비 부재는 DOWN(M4
// countUpDown 산식 계승, observatory_up 자체관측 메트릭 대신 실 vitals 메트릭으로
// 판정). 15s polling(M4 POLL_INTERVAL 계승). 서버 유래 문자열은 전부 JSX 텍스트로만
// 렌더 — dangerouslySetInnerHTML 사용 금지(XSS 방침).
import { useCallback, useEffect, useState } from 'react';
import { Activity, Database, Gauge, Server, Thermometer } from 'lucide-react';
import KpiTile from '@/components/gpu/KpiTile';
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
import { api, type InstantSample, UnauthorizedError } from '@/lib/api';
import { METRIC_GPU_TEMP } from '@/lib/gpuMetrics';

const METRIC_LOAD = 'node_load1';
const POLL_INTERVAL_MS = 15000;
const EM_DASH = '—';

/** 표에 그리는 노드 행 수 — 수집 상태 화면의 표는 "샘플을 보여주는 것"이지
 *  플릿 전수 목록이 아니다. 수백 행을 다 뿌리면 페이지가 수십 화면이 된다. */
const MAX_ROWS = 12;

interface NodeRow {
  instance: string;
  up: boolean;
  load: number | null;
  gpuTemp: number | null;
}

interface Kpis {
  nodeCount: number;
  up: number;
  down: number;
  avgLoad: number | null;
  maxGpuTemp: number | null;
  totalSeries: number;
}

interface RefreshResult {
  rows: NodeRow[];
  kpis: Kpis;
}

function labelInstance(labels: Record<string, string>): string {
  return labels.instance ?? '';
}

// instance → 대표값(최댓값) 맵 — 동일 instance 에 시리즈가 여럿이면(다중 GPU
// 노드 등) 최댓값을 채택한다. 핫스팟 관제 의미로 최댓값이 맞다(M4 byInstanceMax
// 산식 계승).
function byInstanceMax(samples: InstantSample[]): Map<string, number> {
  const m = new Map<string, number>();
  for (const s of samples) {
    const inst = labelInstance(s.metric);
    if (!inst) continue;
    const v = Number(s.value[1]);
    const prev = m.get(inst);
    if (prev === undefined || v > prev) m.set(inst, v);
  }
  return m;
}

function average(values: number[]): number | null {
  return values.length === 0 ? null : values.reduce((sum, v) => sum + v, 0) / values.length;
}

function maxOf(values: number[]): number | null {
  return values.length === 0 ? null : Math.max(...values);
}

export default function Overview() {
  const [rows, setRows] = useState<NodeRow[] | null>(null);
  const [kpis, setKpis] = useState<Kpis | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async (): Promise<RefreshResult> => {
    const [loadResp, gpuResp, seriesResp] = await Promise.all([
      api.instantQuery(METRIC_LOAD),
      api.instantQuery(METRIC_GPU_TEMP),
      api.series([METRIC_LOAD, METRIC_GPU_TEMP]),
    ]);

    const loadSamples = loadResp.data.result;
    const gpuSamples = gpuResp.data.result;
    const roster = seriesResp.data;

    const loadByInstance = byInstanceMax(loadSamples);
    const gpuByInstance = byInstanceMax(gpuSamples);

    // roster(1시간 창)의 distinct instance — node_load1 instant(5분 창) 부재는 down.
    const rosterInstances = new Set<string>();
    for (const labels of roster) {
      const inst = labelInstance(labels);
      if (inst) rosterInstances.add(inst);
    }

    const nextRows: NodeRow[] = Array.from(rosterInstances)
      .map((instance) => ({
        instance,
        up: loadByInstance.has(instance),
        load: loadByInstance.get(instance) ?? null,
        gpuTemp: gpuByInstance.get(instance) ?? null,
      }))
      .sort((a, b) => a.instance.localeCompare(b.instance));

    const upCount = nextRows.filter((row) => row.up).length;

    return {
      rows: nextRows,
      kpis: {
        nodeCount: nextRows.length,
        up: upCount,
        down: nextRows.length - upCount,
        avgLoad: average(loadSamples.map((s) => Number(s.value[1]))),
        maxGpuTemp: maxOf(gpuSamples.map((s) => Number(s.value[1]))),
        totalSeries: roster.length,
      },
    };
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function tick() {
      try {
        const result = await refresh();
        if (!cancelled) {
          setRows(result.rows);
          setKpis(result.kpis);
          setError(null);
        }
      } catch (err) {
        // 401 은 api.ts 의 핸들러가 이미 처리한다(anon 전환 → RequireAuth 가 /login
        // 으로 리다이렉트) — 여기서는 그 외 에러(네트워크·5xx)만 노출하고 폴링은
        // 계속한다(다음 tick 에 자연 복구 시도).
        if (!cancelled && !(err instanceof UnauthorizedError)) {
          setError(err instanceof Error ? err.message : String(err));
        }
      }
    }

    void tick();
    const timer = setInterval(() => void tick(), POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [refresh]);

  return (
    <div className="flex flex-col gap-4">
      {/* 제목은 셸이 메뉴 라벨로 이미 그린다 — 같은 문구를 h1 으로 또 쓰면 화면
          맨 위에 같은 말이 두 번 찍힌다. 여기서는 설명 한 줄만 얹는다. */}
      <p className="text-muted-foreground text-sm">
        플릿 전 노드의 메트릭 수집이 끊김 없이 이어지는지 확인한다.
      </p>

      {error && (
        <p role="alert" className="text-sm text-destructive">
          데이터 갱신 실패: {error}
        </p>
      )}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-5" aria-label="핵심 지표">
        <KpiTile
          label="수집 노드"
          value={kpis && kpis.nodeCount.toLocaleString('ko-KR')}
          hint="최근 1시간 관측된 노드"
          tone="info"
          icon={<Server />}
        />
        <KpiTile
          label="수집 정상"
          value={kpis && kpis.up.toLocaleString('ko-KR')}
          sub={kpis ? `/ ${kpis.nodeCount.toLocaleString('ko-KR')}` : undefined}
          hint={kpis ? `미수집 ${kpis.down}대` : undefined}
          tone={kpis && kpis.down > 0 ? 'danger' : 'ok'}
          icon={<Activity />}
          progress={kpis && kpis.nodeCount > 0 ? (kpis.up / kpis.nodeCount) * 100 : undefined}
        />
        <KpiTile
          label="평균 Load"
          value={kpis && (kpis.avgLoad === null ? EM_DASH : kpis.avgLoad.toFixed(2))}
          hint="노드 1분 평균 부하"
          tone="default"
          icon={<Gauge />}
        />
        <KpiTile
          label="최고 GPU 온도"
          value={kpis && (kpis.maxGpuTemp === null ? EM_DASH : `${kpis.maxGpuTemp.toFixed(1)}°C`)}
          hint="플릿 최댓값 (스로틀 88°C)"
          tone={kpis && kpis.maxGpuTemp !== null && kpis.maxGpuTemp >= 83 ? 'warn' : 'ok'}
          icon={<Thermometer />}
        />
        <KpiTile
          label="수집 시리즈"
          value={kpis && kpis.totalSeries.toLocaleString('ko-KR')}
          hint="노드 부하 + GPU 온도"
          tone="info"
          icon={<Database />}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>노드 수집 현황</CardTitle>
          <CardDescription>
            최근 1시간 관측된 노드 기준 — 최근 5분 안에 값이 들어오지 않으면 미수집으로 본다.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {rows === null ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Instance</TableHead>
                  <TableHead>상태</TableHead>
                  <TableHead>Load</TableHead>
                  <TableHead>GPU 온도</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-center text-muted-foreground">
                      등록된 노드가 없다
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.slice(0, MAX_ROWS).map((row) => (
                    <TableRow key={row.instance}>
                      <TableCell className="font-medium">{row.instance}</TableCell>
                      <TableCell>
                        <Badge
                          variant={row.up ? 'default' : 'destructive'}
                          className={row.up ? 'bg-emerald-600 dark:bg-emerald-600' : undefined}
                        >
                          {row.up ? 'UP' : 'DOWN'}
                        </Badge>
                      </TableCell>
                      <TableCell>{row.load === null ? EM_DASH : row.load.toFixed(2)}</TableCell>
                      <TableCell>
                        {row.gpuTemp === null ? EM_DASH : `${row.gpuTemp.toFixed(1)}°C`}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          )}
          {rows !== null && rows.length > MAX_ROWS && (
            <p className="pt-3 text-muted-foreground text-xs">
              총 {rows.length.toLocaleString('ko-KR')}대 중 상위 {MAX_ROWS}대
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
