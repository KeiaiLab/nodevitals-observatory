// navigation — 2단 메뉴 IA(정보구조) SSOT.
//
//   상단 대메뉴 6개  →  클릭하면 좌측에 그 대메뉴의 서브메뉴가 뜬다
//
// 60여 개 항목을 한 사이드바에 쌓으면 스크롤만 길어지고 "이 제품이 무엇을
// 하는가"가 안 보인다. 상단에서 영역을 고르고 좌측에서 화면을 고르는 2단이
// 관제 콘솔의 관행이자, 대분류 6개를 한눈에 보여주는 방식이다.
//
// 규칙 1: 구현된 화면만 to 를 갖는다. 나머지는 비활성 + "준비 중" —
//         눌러서 빈 화면이 나오는 것보다 정직하고, 제품 범위도 함께 보인다.
// 규칙 2: 구현 화면 1개는 항목 1개에만 붙인다. 같은 화면을 여러 항목에 걸면
//         어디를 눌러도 같은 곳으로 가는 미로가 된다.
import {
  Boxes,
  ChartLine,
  Radar,
  Settings,
  ShieldAlert,
  Wrench,
} from 'lucide-react';

export interface NavLeaf {
  label: string;
  /** 없으면 미구현 → 비활성(클릭 불가). */
  to?: string;
  /** 정확 일치로만 active 판정 (하위 경로를 가진 인덱스 라우트). */
  exact?: boolean;
}

/** 좌측 서브메뉴 안의 소그룹. label 이 없으면 구분선 없이 이어 붙인다. */
export interface NavGroup {
  label?: string;
  items: NavLeaf[];
}

export interface NavTop {
  id: string;
  label: string;
  icon: typeof Radar;
  groups: NavGroup[];
}

export const NAV_TOP: readonly NavTop[] = [
  {
    id: 'realtime',
    label: '실시간 관제',
    icon: Radar,
    groups: [
      {
        items: [
          { label: '통합 상황판', to: '/gpu', exact: true },
          { label: '실시간 토폴로지' },
          { label: 'GPU 실시간 상태' },
          { label: '노드 실시간 상태', to: '/map' },
          { label: '클러스터 실시간 상태' },
          { label: '실시간 메트릭', to: '/explorer' },
        ],
      },
      {
        label: '수집',
        items: [
          { label: '메트릭 수집 상태', to: '/overview' },
          { label: '데이터 누락 현황' },
        ],
      },
    ],
  },
  {
    id: 'incident',
    label: '장애 센터',
    icon: ShieldAlert,
    groups: [
      {
        items: [
          { label: '활성 장애' },
          { label: '장애 분석', to: '/gpu/incident' },
          { label: '무음 장애 탐지', to: '/gpu/health' },
          { label: '장애 이력' },
          { label: '장애·이벤트 분석' },
        ],
      },
      {
        label: '알림',
        items: [
          { label: '알림 규칙' },
          { label: '알림 채널' },
          { label: '장애 판정 규칙' },
        ],
      },
    ],
  },
  {
    id: 'inventory',
    label: '자원·토폴로지',
    icon: Boxes,
    groups: [
      {
        items: [
          { label: '전체 자원 탐색기' },
          { label: 'CSP / 리전 / AZ' },
          { label: '클러스터' },
          { label: '노드 풀' },
          { label: '노드' },
          { label: 'GPU / MIG' },
          { label: '네트워크 패브릭' },
          { label: '워크로드 연결정보' },
        ],
      },
    ],
  },
  {
    id: 'analytics',
    label: '누적 분석',
    icon: ChartLine,
    groups: [
      {
        items: [
          { label: 'GPU 사용량 분석', to: '/gpu/efficiency' },
          { label: '용량·포화도 분석', to: '/gpu/serving' },
          { label: '테넌트·워크로드 분석', to: '/gpu/roadmap' },
          { label: '성능 추이 분석' },
          { label: '저활용 분석' },
          { label: '자동복구 효과 분석' },
          { label: '정기 리포트' },
        ],
      },
    ],
  },
  {
    id: 'remediation',
    label: '자동복구',
    icon: Wrench,
    groups: [
      {
        items: [
          { label: '자동복구 실행', to: '/gpu/remediation' },
          { label: '번인 테스트', to: '/gpu/validation' },
          { label: '자동복구 현황' },
          { label: '격리 노드' },
          { label: '복귀 검증' },
        ],
      },
      {
        label: '정책',
        items: [
          { label: '복구 정책' },
          { label: '상태 전이 규칙' },
          { label: '유지보수 정책' },
          { label: '예외·억제 정책' },
        ],
      },
    ],
  },
  {
    id: 'admin',
    label: '관리',
    icon: Settings,
    groups: [
      {
        label: '조직·권한',
        items: [
          { label: '사용자' },
          { label: '역할·권한' },
          { label: '테넌트' },
          { label: '조직' },
        ],
      },
      {
        label: '멀티클라우드 연동',
        items: [
          { label: '관리형 K8s 연결' },
          { label: 'CSP 이벤트 연동' },
          { label: '연동 상태·자격증명' },
        ],
      },
      {
        label: '수집·저장',
        items: [
          { label: 'GPU 에이전트' },
          { label: 'K8s 에이전트' },
          { label: '노드 에이전트' },
          { label: '메트릭 저장소' },
          { label: '수집 스키마' },
        ],
      },
      {
        label: '시스템',
        items: [{ label: '감사 로그' }, { label: '시크릿 관리' }, { label: '시스템 설정' }],
      },
    ],
  },
] as const;

function leaves(top: NavTop): NavLeaf[] {
  return top.groups.flatMap((g) => g.items);
}

/** 경로에 가장 잘 맞는 (대메뉴, 항목) — 최장 프리픽스 매치. startsWith 첫
 *  매치는 /gpu/health 에서 '/gpu' 를 먼저 잡는 오판을 낸다. */
function matchPath(pathname: string): { top: NavTop; leaf: NavLeaf } | null {
  let best: { top: NavTop; leaf: NavLeaf; len: number } | null = null;
  for (const top of NAV_TOP) {
    for (const leaf of leaves(top)) {
      if (!leaf.to) continue;
      const hit = leaf.exact
        ? pathname === leaf.to
        : pathname === leaf.to || pathname.startsWith(`${leaf.to}/`);
      if (hit && (!best || leaf.to.length > best.len)) {
        best = { top, leaf, len: leaf.to.length };
      }
    }
  }
  return best ? { top: best.top, leaf: best.leaf } : null;
}

/** 현재 경로의 화면 제목. */
export function titleForPath(pathname: string): string {
  return matchPath(pathname)?.leaf.label ?? 'GPU 플릿';
}

/** 현재 경로가 속한 대메뉴 id — 상단 탭 활성 표시와 좌측 메뉴 선택의 근거. */
export function topForPath(pathname: string): string {
  return matchPath(pathname)?.top.id ?? NAV_TOP[0].id;
}

/** 대메뉴 id → 정의. */
export function topById(id: string): NavTop {
  return NAV_TOP.find((t) => t.id === id) ?? NAV_TOP[0];
}

/** 구현된 화면 수 / 전체 — 상단 탭의 진척 배지. */
export function readyRatio(top: NavTop): { ready: number; total: number } {
  const all = leaves(top);
  return { ready: all.filter((l) => l.to).length, total: all.length };
}
