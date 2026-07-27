// navigation — 좌측 메뉴 IA(정보구조) SSOT.
//
// 8개 대분류는 제품의 목표 구조다. 그중 *실제로 구현된 화면만* to 를 갖고,
// 나머지는 to 가 없어 비활성으로 렌더된다 — 눌러서 빈 화면이 나오는 것보다
// "아직 없다"가 정직하고, 동시에 제품의 전체 범위를 보여준다.
//
// 규칙: 구현 화면 1개는 항목 1개에만 붙인다. 같은 화면을 여러 항목에 걸면
// 메뉴가 어디를 눌러도 같은 곳으로 가는 미로가 된다.
import {
  Activity,
  BellRing,
  Boxes,
  ChartLine,
  Layers,
  Radar,
  Settings,
  ShieldAlert,
} from 'lucide-react';

export interface NavLeaf {
  label: string;
  /** 없으면 미구현 → 비활성(클릭 불가). */
  to?: string;
  /** 정확 일치로만 active 판정 (하위 경로를 가진 인덱스 라우트). */
  exact?: boolean;
}

export interface NavSection {
  id: string;
  label: string;
  icon: typeof Activity;
  items: NavLeaf[];
}

export const NAV_SECTIONS: readonly NavSection[] = [
  {
    id: 'realtime',
    label: '1. 실시간 관제',
    icon: Radar,
    items: [
      { label: '통합 상황판', to: '/gpu', exact: true },
      { label: '장애 센터' },
      { label: '실시간 토폴로지' },
      { label: 'GPU 실시간 상태' },
      { label: '노드 실시간 상태', to: '/map' },
      { label: '클러스터 실시간 상태' },
      { label: '실시간 메트릭', to: '/explorer' },
      { label: '자동복구 현황' },
    ],
  },
  {
    id: 'analytics',
    label: '2. 누적 분석',
    icon: ChartLine,
    items: [
      { label: 'GPU 사용량 분석', to: '/gpu/efficiency' },
      { label: '성능 추이 분석' },
      { label: '장애·이벤트 분석' },
      { label: '저활용 분석' },
      { label: '용량·포화도 분석', to: '/gpu/serving' },
      { label: '자동복구 효과 분석' },
      { label: '테넌트·워크로드 분석', to: '/gpu/roadmap' },
      { label: '정기 리포트' },
    ],
  },
  {
    id: 'inventory',
    label: '3. 자원 및 토폴로지',
    icon: Boxes,
    items: [
      { label: '전체 자원 탐색기' },
      { label: 'CSP / 리전' },
      { label: '클러스터' },
      { label: '노드 풀' },
      { label: '노드' },
      { label: 'GPU' },
      { label: '네트워크 패브릭' },
      { label: '워크로드 연결정보' },
    ],
  },
  {
    id: 'incident',
    label: '4. 장애 및 복구',
    icon: ShieldAlert,
    items: [
      { label: '활성 장애' },
      { label: '장애 이력' },
      { label: '무음 장애 탐지', to: '/gpu/health' },
      { label: '자동복구 실행', to: '/gpu/remediation' },
      { label: '격리 노드' },
      { label: '번인 테스트', to: '/gpu/validation' },
      { label: '복귀 검증' },
      { label: '복구 정책' },
    ],
  },
  {
    id: 'multicloud',
    label: '5. 멀티클라우드',
    icon: Layers,
    items: [
      { label: '멀티클라우드 현황' },
      { label: 'NHN Cloud' },
      { label: 'Kakao Cloud' },
      { label: 'Naver Cloud' },
      { label: '관리형 K8s 연결' },
      { label: 'CSP 이벤트 연동' },
      { label: '연동 상태·자격증명' },
    ],
  },
  {
    id: 'observability',
    label: '6. 관측 및 수집',
    icon: Activity,
    items: [
      { label: '메트릭 수집 상태', to: '/overview' },
      { label: 'GPU 에이전트' },
      { label: 'K8s 에이전트' },
      { label: '노드 에이전트' },
      { label: '데이터 누락 현황' },
      { label: '메트릭 저장소' },
      { label: '수집 스키마' },
    ],
  },
  {
    id: 'policy',
    label: '7. 알림 및 운영 정책',
    icon: BellRing,
    items: [
      { label: '알림 규칙' },
      { label: '알림 채널' },
      { label: '장애 판정 규칙' },
      { label: '상태 전이 규칙' },
      { label: '자동복구 정책' },
      { label: '유지보수 정책' },
      { label: '예외·억제 정책' },
    ],
  },
  {
    id: 'admin',
    label: '8. 관리',
    icon: Settings,
    items: [
      { label: '사용자' },
      { label: '역할·권한' },
      { label: '테넌트' },
      { label: '조직' },
      { label: '감사 로그' },
      { label: '시크릿 관리' },
      { label: '시스템 설정' },
    ],
  },
] as const;

/** 경로 → 화면 제목. 최장 프리픽스 매치 — startsWith 첫 매치는 /gpu/health 에서
 *  '/gpu' 를 먼저 잡는 오판을 낸다. */
export function titleForPath(pathname: string): string {
  let best: { label: string; len: number } | null = null;
  for (const section of NAV_SECTIONS) {
    for (const item of section.items) {
      if (!item.to) continue;
      const hit = item.exact
        ? pathname === item.to
        : pathname === item.to || pathname.startsWith(`${item.to}/`);
      if (hit && (!best || item.to.length > best.len)) {
        best = { label: item.label, len: item.to.length };
      }
    }
  }
  return best?.label ?? 'GPU 플릿';
}

/** 해당 경로를 품은 섹션 id — 진입 시 그 대분류만 펼쳐 둔다. */
export function sectionForPath(pathname: string): string {
  let best: { id: string; len: number } | null = null;
  for (const section of NAV_SECTIONS) {
    for (const item of section.items) {
      if (!item.to) continue;
      const hit = item.exact
        ? pathname === item.to
        : pathname === item.to || pathname.startsWith(`${item.to}/`);
      if (hit && (!best || item.to.length > best.len)) {
        best = { id: section.id, len: item.to.length };
      }
    }
  }
  return best?.id ?? NAV_SECTIONS[0].id;
}
