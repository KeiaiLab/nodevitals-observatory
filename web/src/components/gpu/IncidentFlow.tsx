// IncidentFlow — 「원인 및 영향 경로」 가로 7-노드 흐름도(목업 demo2).
//
// 세로 리스트(ChainRow)는 "각 계층에서 분모가 커진다"를 잘 말하지만, 운영자가
// 한눈에 봐야 하는 건 *장애가 어디서 시작해 어디까지 닿았나* 라는 한 줄 경로다.
// 그래서 축을 가로로 눕히고, GPU 수 축(ChainLink)과 워크로드 축(IncidentImpact)
// 을 같은 줄에 섞는다 — 목업의 박스 순서 그대로:
//
//   장애 노드 → GPU → 영향 Pod/Job → Namespace/Tenant → Cluster → CSP/Region → 영향 Service
//
// 목업이 뺀 두 계층은 버리지 않고 흡수한다: 물리 서버(시리얼·랙·PDU)는 장애 노드
// 박스의 부가 정보로, Node Pool 은 Cluster 박스로. 데이터 손실 0.
import { ArrowRight, Box, Boxes, Cloud, Cpu, type LucideIcon, Server, Users } from 'lucide-react';
import { Fragment } from 'react';
import type { ChainLink, IncidentImpact } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

export interface IncidentFlowProps {
  links: ChainLink[];
  impact: IncidentImpact;
  /** 진행 중인 장애가 없으면 전 박스가 "연관 없음"(회색)으로 내려간다. */
  active: boolean;
}

// 3색 범례 — 목업의 영향 있음 / 정상 / 연관 없음. tone 은 서버가 계층별로
// 이미 매긴 값이라 여기서 다시 판정하지 않는다(같은 사실을 두 번 세지 않는다).
type FlowState = 'impacted' | 'unaffected' | 'unrelated';

const STATE_COLOR: Record<FlowState, string> = {
  impacted: 'var(--metric-fault)',
  unaffected: 'var(--metric-pod)',
  unrelated: 'var(--muted-foreground)',
};

// 박스 안 한 줄 상태 문구 — 서버 tone 에서 파생한다. 문자열을 박아 넣으면
// 서버가 계층 상태를 바꾸는 순간 화면이 거짓말을 한다.
const STATE_LABEL: Record<FlowState, string> = {
  impacted: '영향 있음',
  unaffected: '정상',
  unrelated: '연관 없음',
};

const LEGEND: Array<{ state: FlowState; label: string }> = [
  { state: 'impacted', label: '영향 있음 (Impacted)' },
  { state: 'unaffected', label: '정상 (Unaffected)' },
  { state: 'unrelated', label: '연관 없음 (Not Related)' },
];

function stateOfTone(tone: string, active: boolean): FlowState {
  if (!active) return 'unrelated';
  return tone === 'crit' || tone === 'major' ? 'impacted' : 'unaffected';
}

interface FlowNode {
  /** 박스 위 캡션 — 목업의 계층 이름. */
  caption: string;
  /** 박스 상단 아이콘 — 목업이 계층을 글자보다 먼저 알아보게 하는 장치. */
  icon: LucideIcon;
  value: string;
  /** 강조 1줄(상태·오류코드 등). */
  emphasis?: string;
  detail?: string;
  /** GPU 수 축 박스만 — "8 / 3,376". */
  ratio?: string;
  state: FlowState;
}

function FlowBox({ node }: { node: FlowNode }) {
  const color = STATE_COLOR[node.state];
  const Icon = node.icon;
  return (
    <div className="flex h-full min-w-0 flex-col items-center gap-1">
      <span className="truncate text-muted-foreground text-[10px]">{node.caption}</span>
      <div
        className="flex min-h-28 w-full flex-1 flex-col items-center justify-center gap-0.5 rounded-md border-2 bg-card px-1.5 py-2 text-center"
        style={{ borderColor: color }}
      >
        <Icon className="mb-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
        {/* 이름은 잘라내지 않고 2줄까지 접는다 — 칸이 좁아 "naver-b-g…" 로 잘리면
            어느 노드인지 화면에서 사라진다(목업도 두 줄로 흘린다). */}
        <span
          className="line-clamp-2 w-full wrap-break-word font-semibold text-[11px] leading-tight"
          title={node.value}
        >
          {node.value}
        </span>
        {node.emphasis ? (
          <span
            className="w-full truncate font-semibold text-[11px]"
            style={{ color }}
            title={node.emphasis}
          >
            {node.emphasis}
          </span>
        ) : null}
        {node.detail ? (
          <span className="w-full truncate text-[10px] text-muted-foreground" title={node.detail}>
            {node.detail}
          </span>
        ) : null}
        {node.ratio ? (
          <span className="text-[10px] text-muted-foreground tabular-nums">{node.ratio}</span>
        ) : null}
      </div>
    </div>
  );
}

export default function IncidentFlow({ links, impact, active }: IncidentFlowProps) {
  if (links.length < 6) {
    return <p className="text-muted-foreground text-sm">경로를 구성할 대상이 없다.</p>;
  }
  const [gpu, server, node, pool, cluster, csp] = links;
  const ratio = (l: ChainLink) => `${formatCount(l.scope)} / ${formatCount(l.total)}`;

  const nodes: FlowNode[] = [
    {
      caption: '장애 노드',
      icon: Server,
      value: node.value,
      emphasis: `상태: ${node.detail}`,
      // 목업이 생략한 물리 서버 계층을 여기로 흡수 — 시리얼·랙·PDU 가 사라지면
      // 현장에서 어느 장비를 만져야 하는지가 화면에서 없어진다.
      detail: `${server.value} · ${server.detail}`,
      ratio: ratio(node),
      state: stateOfTone(node.tone, active),
    },
    {
      caption: 'GPU',
      icon: Cpu,
      value: gpu.value,
      emphasis: gpu.detail,
      ratio: ratio(gpu),
      state: stateOfTone(gpu.tone, active),
    },
    {
      caption: '영향 Pod/Job',
      icon: Box,
      value: `${formatCount(impact.pods)}개`,
      emphasis: `Running: ${formatCount(impact.podsReady)} / ${formatCount(impact.pods)}`,
      detail: '드레인 대상 워크로드',
      state: active ? 'impacted' : 'unrelated',
    },
    {
      caption: 'Namespace / Tenant',
      icon: Users,
      value: impact.namespace,
      emphasis: `${impact.tenant} (${formatCount(impact.tenants)}개)`,
      detail: '소유 테넌트',
      state: active ? 'impacted' : 'unrelated',
    },
    {
      caption: 'Cluster',
      icon: Boxes,
      value: cluster.value,
      // Node Pool 계층 흡수 — 클러스터 안 어느 풀인지가 교체 요청의 단위다.
      emphasis: `상태: ${STATE_LABEL[stateOfTone(cluster.tone, active)]}`,
      detail: `노드 풀 ${pool.value}`,
      ratio: ratio(cluster),
      state: stateOfTone(cluster.tone, active),
    },
    {
      caption: 'CSP / Region',
      icon: Cloud,
      value: csp.value,
      emphasis: `상태: ${STATE_LABEL[stateOfTone(csp.tone, active)]}`,
      detail: csp.detail,
      ratio: ratio(csp),
      state: stateOfTone(csp.tone, active),
    },
    {
      caption: '영향 Service',
      icon: Boxes,
      value: impact.service,
      emphasis: STATE_LABEL[active ? 'impacted' : 'unrelated'],
      detail: `Service ${formatCount(impact.services)}개`,
      state: active ? 'impacted' : 'unrelated',
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      {/* 7 박스가 *항상* 한 화면에 들어와야 한다 — 고정폭 + overflow-x-auto 는
          좁은 칸에서 뒤 2박스(CSP/Region·영향 Service)를 화면 밖으로 밀어내
          "클라우드 경계까지"라는 이 그림의 결론을 잘라 먹는다. 그래서 박스는
          flex-1 min-w-0 로 남은 폭을 균등 분배하고, 넘치는 글자는 truncate 가
          받는다(잘린 그래프보다 잘린 글자가 낫다). 화살표는 캡션(1줄) 높이만큼
          내려 박스 세로 중앙에 맞춘다. */}
      <ol className="flex items-stretch gap-1">
        {nodes.map((n, i) => (
          <Fragment key={n.caption}>
            <li className="flex min-w-0 flex-1">
              <FlowBox node={n} />
            </li>
            {i < nodes.length - 1 ? (
              <ArrowRight
                className="mt-3 size-3.5 shrink-0 self-center text-muted-foreground/60"
                aria-hidden
              />
            ) : null}
          </Fragment>
        ))}
      </ol>
      <ul className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1">
        {LEGEND.map((l) => (
          <li key={l.state} className="flex items-center gap-1.5 text-muted-foreground text-xs">
            <span
              className="size-3 rounded-[3px] border-2"
              style={{ borderColor: STATE_COLOR[l.state] }}
            />
            {l.label}
          </li>
        ))}
      </ul>
    </div>
  );
}
