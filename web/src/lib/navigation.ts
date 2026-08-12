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
// 규칙 4: "준비 중"은 대메뉴당 3개 이하로 두고, 항상 구현 화면 뒤에 놓는다.
//         규칙 1의 "제품 범위도 함께 보인다"는 항목이 적을 때만 성립한다 —
//         실측(스크린샷): 60개 중 46개가 회색이던 시점의 자원·토폴로지 탭은
//         12줄 중 11줄이 회색 "준비 중"이라 사이드바가 통째로 비활성 벽으로
//         보였고, 상단 탭 배지는 1/12·0/15 로 "이 제품은 비어 있다"를 상시
//         광고했다. 범위 신호가 미완성 신호로 뒤집힌 것이다. 그래서 남길
//         "준비 중"은 (a) 목업 사이드바에 실제로 있는 이름 (b) 그 대분류가
//         무엇인지 설명하는 최소한, 둘 중 하나뿐이고 나머지는 지웠다
//         (46 → 16). 내부 배관에 가까운 항목(수집 스키마·메트릭 저장소·
//         예외 억제 정책 등)은 시연에서 아무 말도 보태지 않으므로 제외한다.
//         한 대분류의 여러 미구현 항목은 상위 이름 하나로 접는다
//         (사용자/역할·권한/테넌트/조직 → '사용자·권한').
// 규칙 3(폐기): 라벨 앞에 시연 순번 ①~⑥ 을 붙이지 않는다. 한 번 시도했다가
//         되돌렸다 — 런북(docs/demo-runbook.md §1)이 이미 살아있는 Step 번호를
//         쓰는데 시나리오 문서 순서로 번호를 다시 매기면 같은 숫자가 서로 다른
//         화면을 가리킨다(실측: 런북 Step 6개 전부 다른 화면으로 오도. 예를
//         들어 "Step 4 격리"를 듣고 ④ 를 찾으면 GPU Assistant 에 도착한다).
//         순서는 런북이 알려주고, 메뉴는 이름으로 찾히게 둔다 — 그래서 라벨은
//         런북 §1 의 화면명과 글자까지 같아야 한다.
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
          { label: '노드 실시간 상태', to: '/map' },
          { label: '실시간 메트릭', to: '/explorer' },
          { label: '실시간 토폴로지' },
          { label: 'GPU 실시간 상태' },
        ],
      },
      {
        label: '수집',
        items: [{ label: '메트릭 수집 상태', to: '/overview' }],
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
          { label: '장애 상세', to: '/gpu/incident' },
          { label: 'GPU Assistant', to: '/gpu/assistant' },
          { label: '무음 장애 탐지', to: '/gpu/health' },
          { label: '활성 장애' },
          { label: '장애 이력' },
        ],
      },
      {
        label: '알림',
        items: [
          // 라벨 = 화면 제목 = 시나리오 원문 명칭('GPU 장애 룰셋 관리')으로
          // 통일한다 — 셸 헤더가 이 라벨을 그대로 제목으로 쓰므로 어긋나면
          // 시연 중 '룰셋'을 찾는 사람이 좌측 메뉴에서 못 찾는다. 새 항목을
          // 만들지 않고 이미 있던 자리를 채운다(규칙 2: 화면 1개 = 항목 1개).
          { label: 'GPU 장애 룰셋 관리', to: '/gpu/ruleset' },
          { label: '알림 채널' },
        ],
      },
    ],
  },
  {
    id: 'inventory',
    label: '자원·토폴로지',
    icon: Boxes,
    groups: [
      // 구 12항목 중 11개가 미구현이라 사이드바 전체가 회색 벽이었다(규칙 4).
      // 자원 계층(CSP·리전·노드 풀·패브릭…)을 한 줄씩 나열해도 시연에서 할 말이
      // 없으므로, 관제 콘솔이라면 당연히 있을 3종(클러스터·노드·GPU)만 남긴다.
      // 소그룹 '유휴장비'는 항목이 1개뿐이라 제목만 남으므로 없앤다.
      {
        items: [
          { label: '유휴장비 관리', to: '/gpu/idle' },
          { label: '클러스터' },
          { label: '노드' },
          { label: 'GPU / MIG' },
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
          // '저활용 분석'은 지웠다 — GPU 사용량 분석 화면이 이미 저활용 탐지
          // 탭을 담고 있어, 옆에 회색으로 두면 있는 기능을 없다고 말하게 된다.
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
          // 화면이 사전검증·번인 2 스테이지를 함께 담으므로 라벨도 그렇게 읽힌다.
          { label: '재사용 검증 (사전검증·번인)', to: '/gpu/validation' },
          { label: '자동복구 현황' },
          { label: '격리 노드' },
          // 정책 4종은 '복구 정책' 하나로 접었다 — 상태 전이/유지보수/예외 억제는
          // 이름만 봐서는 서로 구별되지 않아 회색 줄 수만 늘렸다. 접고 나니
          // '정책' 소그룹이 회색 1줄짜리 섹션이 됐고(실측 2회차 스크린샷),
          // 제목이 오히려 '이 섹션은 비어 있다'를 강조해서 제목을 없애고
          // 본문 끝에 붙인다.
          { label: '복구 정책' },
        ],
      },
    ],
  },
  {
    id: 'admin',
    label: '관리',
    icon: Settings,
    // 15항목 0구현 = 상단 배지가 '0/15'. 눌러 보면 4개 소그룹 제목까지 얹힌
    // 완전한 회색 화면이라, 시연에서 실수로 한 번만 눌러도 제품 인상이 뒤집힌다.
    // 목업 사이드바(demo1·demo4)의 관리 영역도 2~3줄뿐이므로 거기에 맞춰
    // 대분류 이름 4개만 남기고 소그룹 제목은 없앤다(0/15 → 0/4).
    groups: [
      {
        items: [
          { label: '사용자·권한' },
          { label: '멀티클라우드 연동' },
          { label: '에이전트·수집' },
          { label: '시스템 설정' },
        ],
      },
    ],
  },
] as const;

/** 시연에 실제로 쓰이는 화면. 두 자산의 합집합이다 —
 *  목업 5장(`docs/mockups/demo1~5.png`) ∪ 런북 §1 6-Step.
 *  둘이 서로 다른 화면을 쓴다: 런북은 무음장애·로드맵·자동복구·효율을 Step 으로
 *  잡고, 목업은 장애 상세·Assistant·유휴장비를 그린다. 어느 한쪽만 남기면 다른
 *  쪽 시연이 끊기므로 합집합이 "핵심"이다.
 *
 *  데모 모드에서는 이 집합 밖 항목을 메뉴에서 감춘다 — 준비 중 13개와 시연에
 *  안 쓰는 구현 화면 4개(`/map`·`/explorer`·`/overview`·`/gpu/serving`)가
 *  27줄 중 17줄을 차지해, 정작 시연할 10개가 묻힌다. 라우트는 살려 두므로
 *  URL 직접 접근은 그대로 되고, 되돌리기도 이 집합 한 곳만 고치면 된다. */
export const DEMO_ROUTES: ReadonlySet<string> = new Set([
  '/gpu/ruleset', // S1 사전 설정 (시나리오 1 · 시연 첫 화면)
  '/gpu', // Step 1 통합관제 = demo1
  '/gpu/health', // Step 2 감지
  '/gpu/roadmap', // Step 3 분석
  '/gpu/remediation', // Step 4 격리
  '/gpu/validation', // Step 5 검증 = demo5
  '/gpu/efficiency', // Step 6 효율
  '/gpu/incident', // demo2
  '/gpu/assistant', // demo3
  '/gpu/idle', // demo4
]);

/** 화면에 그릴 그룹 — 미구현은 항상 빼고, 데모면 DEMO_ROUTES 로 한 번 더 좁힌다.
 *  항목이 0인 그룹은 제목만 남으므로 버린다. */
export function visibleGroups(top: NavTop, demo: boolean): NavGroup[] {
  return top.groups
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => i.to && (!demo || DEMO_ROUTES.has(i.to))),
    }))
    .filter((g) => g.items.length > 0);
}

/** 상단에 그릴 대메뉴 — 보일 항목이 하나도 없는 탭은 눌러도 빈 사이드바뿐이라 감춘다. */
export function visibleTops(demo: boolean): NavTop[] {
  return NAV_TOP.filter((t) => visibleGroups(t, demo).length > 0);
}

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
