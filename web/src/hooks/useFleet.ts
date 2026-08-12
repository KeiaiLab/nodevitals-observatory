// useFleet — 플릿 히트맵 벽면의 데이터 훅. per-GPU util instant 1회(~7,000
// 샘플)를 30s 폴링으로 받고, 셀 정렬·클러스터 청크·필터 옵션을 파생한다.
// 서버 집계·정규식 매처가 없으므로 필터는 전량 클라이언트 사이드다.
import { useMemo } from 'react';
import { api } from '@/lib/api';
import { bandForUtil, METRIC_GPU_UTIL } from '@/lib/gpuMetrics';
import { usePolledQuery } from '@/hooks/usePolledQuery';

export interface FleetCell {
  instance: string;
  device: string;
  uuid: string;
  model: string;
  csp: string;
  region: string;
  cluster: string;
  /** K8s 노드 풀 — 토폴로지의 클러스터↔노드 사이 계층. */
  nodePool: string;
  pool: string;
  tenant: string;
  /** 워크로드 종류 — 'inference' | 'training' | ''(미할당). 학습/추론 필터 축. */
  workload: string;
  util: number;
  band: number;
}

/** 공통 검색 필터 — 전 화면이 같은 7개 차원을 쓴다(사용자 지시 2026-07-27).
 *  csp/region/cluster/tenant/model 은 셀 라벨로, status 는 장애·온도·격리
 *  집합으로, severity 는 알림 목록에 적용된다(셀에는 심각도가 없다). */
export interface FleetFilter {
  csp?: string;
  region?: string;
  cluster?: string;
  pool?: string;
  tenant?: string;
  model?: string;
  status?: string;
  severity?: string;
  /** 학습/추론 — 미지정(기본)이면 둘 다 본다. 'inference' | 'training'. */
  workload?: string;
}

/** 셀 상태 5분류 — 서버 상황판(dashboard.gpus)의 key 와 같은 어휘를 쓴다. */
export type CellStatus = 'normal' | 'degraded' | 'fault' | 'isolated' | 'recovering';

export interface ClusterChunk {
  key: string;
  csp: string;
  cells: FleetCell[];
  /** 셀 대역 시그니처 — React.memo 비교자가 이 문자열로 재렌더를 스킵한다. */
  signature: string;
}

export interface FleetOptions {
  csps: string[];
  regions: string[];
  clusters: string[];
  pools: string[];
  tenants: string[];
  models: string[];
}

const FLEET_POLL_MS = 30_000;

function deviceOrder(device: string): number {
  const n = Number.parseInt(device.replace(/^\D+/, ''), 10);
  return Number.isNaN(n) ? 0 : n;
}

export function useFleet() {
  const query = usePolledQuery(async () => {
    const resp = await api.instantQuery(METRIC_GPU_UTIL);
    const cells: FleetCell[] = resp.data.result.map((s) => {
      const util = Number(s.value[1]);
      return {
        instance: s.metric.instance ?? '',
        device: s.metric.device ?? '',
        uuid: s.metric.gpu_uuid ?? `${s.metric.instance}/${s.metric.device}`,
        model: s.metric.gpu_model ?? '',
        csp: s.metric.csp ?? '',
        region: s.metric.region ?? '',
        cluster: s.metric.cluster ?? s.metric.instance ?? '',
        nodePool: s.metric.node_pool ?? '',
        pool: s.metric.pool ?? '',
        tenant: s.metric.tenant ?? '',
        workload: s.metric.workload ?? '',
        util,
        band: bandForUtil(util).index,
      };
    });
    cells.sort(
      (a, b) =>
        a.csp.localeCompare(b.csp) ||
        a.cluster.localeCompare(b.cluster) ||
        a.instance.localeCompare(b.instance) ||
        deviceOrder(a.device) - deviceOrder(b.device),
    );
    return cells;
  }, FLEET_POLL_MS);

  const cells = query.data;

  // 필터 옵션은 벽면 응답 라벨에서 도출한다 — api.series 로 7,000 라벨셋을
  // 중복 수신하지 않는다.
  const options: FleetOptions = useMemo(() => {
    const uniq = (values: string[]) => [...new Set(values.filter(Boolean))].sort();
    return {
      csps: uniq((cells ?? []).map((c) => c.csp)),
      regions: uniq((cells ?? []).map((c) => c.region)),
      clusters: uniq((cells ?? []).map((c) => c.cluster)),
      pools: uniq((cells ?? []).map((c) => c.pool)),
      tenants: uniq((cells ?? []).map((c) => c.tenant)),
      models: uniq((cells ?? []).map((c) => c.model)),
    };
  }, [cells]);

  return { cells, options, error: query.error, updatedAt: query.updatedAt, refresh: query.refresh };
}

/** statusOf 는 셀의 상태 분류자다 — 상태는 라벨이 아니라 장애·온도·격리
 *  집합에서 나오므로 호출부(상태를 아는 화면)가 주입한다. 미주입이면 상태
 *  필터는 무시된다(필터가 조용히 전부를 지우는 것보다 낫다). */
export function applyFleetFilter(
  cells: FleetCell[],
  filter: FleetFilter,
  statusOf?: (cell: FleetCell) => CellStatus,
): FleetCell[] {
  return cells.filter(
    (c) =>
      (!filter.csp || c.csp === filter.csp) &&
      (!filter.region || c.region === filter.region) &&
      (!filter.cluster || c.cluster === filter.cluster) &&
      (!filter.pool || c.pool === filter.pool) &&
      (!filter.tenant || c.tenant === filter.tenant) &&
      (!filter.model || c.model === filter.model) &&
      (!filter.workload || c.workload === filter.workload) &&
      (!filter.status || !statusOf || statusOf(c) === filter.status),
  );
}

/** 클러스터 단위 청크(≈250셀)로 묶는다 — 히트맵 memo 재렌더 단위. */
export function groupClusterChunks(cells: FleetCell[]): ClusterChunk[] {
  const byCluster = new Map<string, FleetCell[]>();
  for (const c of cells) {
    const list = byCluster.get(c.cluster);
    if (list) list.push(c);
    else byCluster.set(c.cluster, [c]);
  }
  return [...byCluster.entries()].map(([key, list]) => ({
    key,
    csp: list[0]?.csp ?? '',
    cells: list,
    signature: list.map((c) => c.band).join(''),
  }));
}
