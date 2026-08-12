// Shell — 공통 셸: 사이드바 네비 + 상단 대메뉴(테마·로그아웃).
// 라우트 콘텐츠는 <Outlet/> 으로 렌더된다.
//
// 메뉴 구조 SSOT = lib/navigation.ts. 어느 모드에서도 *열리는 화면만* 그린다:
// 미구현("준비 중") 항목은 노출하지 않고, 데모에서는 시연에 실제로 쓰는
// 화면(DEMO_ROUTES)으로 한 번 더 좁힌다. 눌러서 아무 일도 안 일어나는 줄이
// 목록의 다수를 차지하면 제품이 비어 보이고, 정작 눌러야 할 화면이 묻힌다.
import { Cpu, LogOut, Moon, Sun } from 'lucide-react';
import { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router';
import { useAuth } from '@/auth/AuthContext';
import { productName, productTagline } from '@/lib/branding';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from '@/components/ui/sidebar';
import { formatBuildTime, useBuildInfo } from '@/hooks/useBuildInfo';
import { useDemoStatus } from '@/hooks/useDemoStatus';
import {
  GlobalSearch,
  HelpButton,
  LiveClock,
  NotificationBell,
  UserAvatar,
} from '@/components/layout/HeaderExtras';
import {
  type NavLeaf,
  type NavTop,
  titleForPath,
  topById,
  topForPath,
  visibleGroups,
  visibleTops,
} from '@/lib/navigation';

const THEME_KEY = 'nv-theme';

/** 목업 좌측 하단 형식(2025.06.08 14:30:45). */
function formatStamp(): string {
  const d = new Date();
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}.${p(d.getMonth() + 1)}.${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** 기본값은 라이트다 — OS 선호를 따르지 않는다. 시각 기준선(docs/mockups/demo1·4·5)이
 *  전부 라이트라, OS 가 다크인 발표자 노트북에서 열면 아무도 대조해 본 적 없는
 *  화면이 시연에 나간다. 명시적으로 토글한 선택은 그대로 존중한다. */
function initialDark(): boolean {
  return localStorage.getItem(THEME_KEY) === 'dark';
}

function ThemeToggle() {
  const [dark, setDark] = useState(initialDark);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
  }, [dark]);

  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="테마 전환"
      onClick={() => {
        const next = !dark;
        setDark(next);
        localStorage.setItem(THEME_KEY, next ? 'dark' : 'light');
      }}
    >
      {dark ? <Sun /> : <Moon />}
    </Button>
  );
}

function isLeafActive(item: NavLeaf, pathname: string): boolean {
  if (!item.to) return false;
  return item.exact ? pathname === item.to : pathname.startsWith(item.to);
}

/** 좌측 서브메뉴 — 상단에서 고른 대메뉴 중 *실제로 열리는* 항목만 렌더한다.
 *  데모에서는 시연에 쓰는 화면(navigation.DEMO_ROUTES)으로 한 번 더 좁힌다:
 *  준비 중 13개 + 시연 밖 구현 4개가 27줄 중 17줄이라, 남기면 정작 눌러야 할
 *  10개가 죽은 줄 사이에 묻힌다. 눌러서 아무 일도 안 일어나는 줄은 두지 않는다. */
function NavSubMenu({
  top,
  pathname,
  demo,
}: {
  top: NavTop;
  pathname: string;
  demo: boolean;
}) {
  return (
    <>
      {visibleGroups(top, demo).map((group, gi) => (
        <SidebarGroup key={group.label ?? `g${gi}`} className="py-1">
          {group.label ? <SidebarGroupLabel>{group.label}</SidebarGroupLabel> : null}
          <SidebarGroupContent>
            <SidebarMenu>
              {group.items.map((item) => (
                <SidebarMenuItem key={item.label}>
                  <SidebarMenuButton asChild isActive={isLeafActive(item, pathname)}>
                    <NavLink to={item.to as string}>
                      <span>{item.label}</span>
                    </NavLink>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      ))}
    </>
  );
}

/** 상단 대메뉴. 클릭하면 그 대메뉴의 첫 화면으로 이동하고 좌측 서브메뉴가
 *  통째로 바뀐다. 열리는 항목이 하나도 없는 탭(데모의 '관리' 등)은 눌러도 빈
 *  사이드바뿐이라 아예 그리지 않는다. */
function TopMenu({
  activeId,
  onSelect,
  demo,
}: {
  activeId: string;
  onSelect: (id: string) => void;
  demo: boolean;
}) {
  return (
    <nav className="flex items-center gap-0.5 overflow-x-auto" aria-label="대메뉴">
      {visibleTops(demo).map((top) => {
        const active = top.id === activeId;
        return (
          <button
            key={top.id}
            type="button"
            onClick={() => onSelect(top.id)}
            aria-current={active ? 'page' : undefined}
            className={`flex shrink-0 items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm transition-colors ${
              active
                ? 'bg-accent font-medium text-accent-foreground'
                : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
            }`}
          >
            <top.icon className="size-3.5 shrink-0" />
            <span className="whitespace-nowrap">{top.label}</span>
          </button>
        );
      })}
    </nav>
  );
}

/** 헤더 알림 배지 — 미확인 Critical+Major 수. 데모가 아니면 질의하지 않는다.
 *  각 화면의 상태 폴링과 별개로 가볍게 도는 전용 조회(30초). */
function useHeaderAlertCount(enabled: boolean): number | undefined {
  const [count, setCount] = useState<number | undefined>(undefined);
  useEffect(() => {
    if (!enabled) {
      setCount(undefined);
      return;
    }
    let cancelled = false;
    const tick = async () => {
      try {
        const res = await fetch('/api/v1/demo/state', { credentials: 'include' });
        if (!res.ok) return;
        const body = (await res.json()) as {
          data?: { alerts?: Array<{ severity: string; acked?: boolean }> };
        };
        if (cancelled) return;
        const n = (body.data?.alerts ?? []).filter(
          (a) => !a.acked && (a.severity === 'critical' || a.severity === 'major'),
        ).length;
        setCount(n);
      } catch {
        // 조용히 무시 — 배지는 보조 정보다.
      }
    };
    tick();
    const id = window.setInterval(() => {
      if (!document.hidden) tick();
    }, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [enabled]);
  return count;
}

export default function Shell() {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const demoStatus = useDemoStatus();

  const brand = productName(demoStatus?.enabled);
  const tagline = productTagline(demoStatus?.enabled);
  useEffect(() => {
    document.title = brand;
  }, [brand]);

  // 타이틀·기본 펼침 섹션은 최장 프리픽스 매치로 고른다(navigation.ts) —
  // startsWith 첫 매치는 /gpu/health 에서 '/gpu' 를 먼저 잡는 오판을 낸다.
  const title = titleForPath(location.pathname);
  // 데모는 시연 화면만 노출한다(navigation.DEMO_ROUTES). 실서비스는 구현된
  // 화면 전부 — 거기서는 /explorer·/overview 도 정상 기능이다.
  const demo = demoStatus?.enabled === true;

  // 상단 대메뉴 선택 = 경로에서 유도하되, 사용자가 직접 고른 것이 있으면 그쪽이
  // 우선한다 — 구현 화면이 없는 대메뉴(자원·토폴로지 등)를 눌렀을 때 경로는
  // 그대로여도 좌측이 바뀌어야 한다.
  const pathTop = topForPath(location.pathname);
  const [pickedTop, setPickedTop] = useState<string | null>(null);
  // 알림 벨 배지 — 미확인 Critical+Major 수. demo/state 를 헤더가 또 폴링하지
  // 않도록 가벼운 전용 조회(30초)로 센다.
  const alertCount = useHeaderAlertCount(demoStatus?.enabled === true);
  // 사이드바 하단 갱신 시각 — 30초마다 한 번이면 충분하다(초 단위 시계는 헤더에 있다).
  const [lastUpdate, setLastUpdate] = useState(() => formatStamp());
  const build = useBuildInfo();
  useEffect(() => {
    const id = window.setInterval(() => setLastUpdate(formatStamp()), 30_000);
    return () => window.clearInterval(id);
  }, []);
  const activeTopId = pickedTop ?? pathTop;
  const activeTop = topById(activeTopId);

  // 경로가 바뀌면(링크 이동) 수동 선택을 놓아준다 — 안 그러면 좌측 메뉴가
  // 현재 화면과 다른 대메뉴에 머문다.
  useEffect(() => {
    setPickedTop(null);
  }, [location.pathname]);

  function onSelectTop(id: string) {
    setPickedTop(id);
    // 좌측에 실제로 그려질 첫 화면으로 이동한다 — 감춰진 항목으로 보내면
    // 사이드바에는 없는 곳에 서 있게 된다.
    const first = visibleGroups(topById(id), demo).flatMap((g) => g.items)[0];
    if (first?.to && first.to !== location.pathname) navigate(first.to);
  }

  async function handleLogout() {
    await logout();
    navigate('/login', { replace: true });
  }

  return (
    <SidebarProvider>
      <Sidebar>
        {/* 브랜드 블록 — 목업(demo1/4/5) 좌상단은 [색 있는 마크] + 굵은 제품명 +
            보조 한 줄이고, 우측 헤더 첫 줄(h-14)과 밑선이 맞는다. 기존은 14px
            텍스트 한 줄뿐이라 제품 얼굴이 없었다. 마크는 lucide 아이콘 재사용 —
            SVG 자산을 새로 만들 이유가 없다. */}
        <SidebarHeader className="h-14 justify-center border-b p-0">
          <div className="flex items-center gap-2.5 px-3">
            <span
              className="flex size-8 shrink-0 items-center justify-center rounded-lg text-[var(--primary-foreground)]"
              style={{ background: 'var(--primary)' }}
              aria-hidden
            >
              <Cpu className="size-4.5" />
            </span>
            <span className="flex min-w-0 flex-col leading-tight">
              <span className="truncate text-base font-semibold">{brand}</span>
              <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                {/* 데모 모드에서만 공동 브랜딩 — 실서비스 관제 콘솔에는 파트너
                    로고가 붙지 않는다. */}
                {demoStatus?.enabled ? (
                  <img
                    src="/brand/lguplus.svg"
                    alt="LG U+"
                    width={46}
                    height={15}
                    className="h-3 w-auto shrink-0"
                  />
                ) : null}
                <span className="truncate">{tagline}</span>
              </span>
            </span>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <NavSubMenu top={activeTop} pathname={location.pathname} demo={demo} />
        </SidebarContent>
        {/* 목업 demo4/demo5 좌측 하단과 같은 갱신 시각 — 관제 콘솔에서 "이 화면이
            살아 있다"를 말해주는 한 줄이다. */}
        <SidebarFooter className="border-t">
          <div className="px-2 py-1 text-[11px] leading-tight text-muted-foreground">
            <div>마지막 업데이트</div>
            <div className="tabular-nums">{lastUpdate}</div>
            {/* 위는 *데이터* 갱신 시각, 아래는 *배포* 시각 — 둘은 다른 사실이라
                구분선을 두고 갈라 놓는다. 서버가 답하지 못하면 통째로 생략한다. */}
            {build ? (
              <div className="mt-1.5 border-t pt-1.5">
                <div className="flex items-center gap-1">
                  <span>버전</span>
                  <span className="font-medium tabular-nums text-foreground">v{build.version}</span>
                  {build.commit && build.commit !== 'unknown' ? (
                    <span className="font-mono text-[10px] opacity-70">{build.commit}</span>
                  ) : null}
                </div>
                <div className="tabular-nums">배포 {formatBuildTime(build.startedAt)}</div>
              </div>
            ) : null}
          </div>
        </SidebarFooter>
      </Sidebar>
      {/* min-w-0 는 선택이 아니다. flex 아이템의 기본 min-width 는 auto(=min-content)
          라, 이게 없으면 본문에서 가장 넓은 표의 min-content 가 그대로 페이지 하한이
          되어 문서 전체가 가로로 넘친다. 이것과 아래 행1 의 *:min-w-0 는 둘 다
          있어야 성립한다 — 로그인(비-public) 1280 /gpu/ruleset 실측(같은 번들에
          CSS 로 각 항을 껐다 켜며 짝지어 측정):
            둘 다 없음 → 넘침 76px, 뷰포트 밖 요소 33개(룰셋 토글 15개 전부 포함)
            여기 min-w-0 만  → 넘침 19px, [로그아웃] 이 아직 밖
            아래 *:min-w-0 만 → 넘침 76px (변화 없음 — 본문 min-content 가 그대로 하한)
            둘 다          → 넘침 0, 밖 0, 토글 15/15 화면 안
          public 모드는 [로그아웃] 버튼(95px)이 없어 네 경우 모두 넘침 0 이다 —
          public 만 재면 이 결함은 보이지 않는다. 재측정 시 두 모드를 다 켜라. */}
      <SidebarInset className="min-w-0">
        {/* 헤더는 두 줄이다. 대메뉴를 우측 클러스터(검색·시계·갱신·벨·도움말·
            테마·아바타)와 같은 줄에 두면 둘이 폭을 다투는데, 우측은 shrink-0 이라
            부족분을 대메뉴가 100% 떠안는다 — 실측(1440 뷰포트) 대메뉴 가시폭
            184/713px 로 6개 중 1개만 보였고 1280 에서는 0개였다. 줄을 나누면
            헤더 min-content 가 1714 → 993px 로 떨어져 1280~1920 전 구간에서
            6개가 모두 보이고 문서 가로 넘침(1440→1696)도 사라진다. 전 라우트
            공통 문제라 여기 한 곳에서 끝낸다. */}
        {/* bg-card — 본문 배경이 옅은 회색이 되면서(index.css) 헤더는 흰 면으로
            떠야 목업처럼 상단이 분리돼 보인다. 높이는 불변(h-14 + pb-2). */}
        <header className="flex shrink-0 flex-col border-b bg-card">
          {/* *:min-w-0 — 이 줄의 어떤 항목도 자기 콘텐츠 폭을 하한으로 주장하지
              않는다. 실효 대상은 검색뿐이다(트리거·구분선·우측 클러스터는 이미
              shrink-0 이라 줄지 않는다 — 실측: 전 자식 지정과 검색만 지정이 전
              측정칸에서 동일, 트리거 28px 불변). 로그인 모드 1280 에서 검색이
              384→308px 로 줄며 부족분 76px 를 흡수하고, placeholder 는 자체
              truncate 로 말줄임된다(Ctrl+K 뱃지는 shrink-0 이라 그대로 남는다). */}
          <div className="flex h-14 items-center gap-2 px-4 *:min-w-0">
            <SidebarTrigger />
            <Separator orientation="vertical" className="h-5" />
            {/* 검색은 목업과 같이 좌측이다. 우측 클러스터 안에 두면 shrink-0 에
                묶여 이 줄의 최소폭이 384px 만큼 커진다 — 좌측에 두면 좁은 폭에서
                자신이 줄어 부족분을 흡수한다. */}
            {demoStatus?.enabled ? <GlobalSearch onNavigate={(to) => navigate(to)} demo={demo} /> : null}
            <div className="ml-auto flex shrink-0 items-center gap-1">
              {/* 목업 헤더 7요소 — 데모에서만. 실서비스는 산출원(알림 카운트 등)이
                  달라 기존 최소 구성을 유지한다. */}
              {demoStatus?.enabled ? (
                <>
                  <LiveClock />
                  {/* 벨은 배지가 세는 그 알림이 있는 곳으로 보낸다 — 미확인
                      Critical+Major 를 세어 놓고 눌러도 아무 데도 못 가면
                      배지가 셈만 하는 장식이 된다. 목적지(무음 장애 탐지)는
                      같은 서버 알림 배열을 확인 버튼과 함께 그리는 화면이다.
                      구 [자동 갱신 N초] 셀렉터는 여기서 뺐다 — 라벨만 바뀌고
                      실제 폴링 주기는 각 화면 훅이 쥐고 있어 어떤 값을 골라도
                      갱신 속도가 그대로였다(제어권을 헤더로 올리려면 전 화면을
                      가로지르는 컨텍스트가 필요하다 — 별도 트랙). */}
                  <NotificationBell
                    count={alertCount}
                    onClick={() => navigate('/gpu/health')}
                  />
                  <HelpButton />
                </>
              ) : null}
              <ThemeToggle />
              {demoStatus?.enabled ? <UserAvatar /> : null}
              {/* public demo 는 로그인 자체가 없다 — 로그아웃 버튼을 두면 방문자를
                  자격증명 없는 로그인 화면에 가두게 되므로 숨긴다. */}
              {demoStatus?.public ? null : (
                <Button variant="ghost" size="sm" onClick={handleLogout}>
                  <LogOut />
                  로그아웃
                </Button>
              )}
            </div>
          </div>
          <div className="flex items-center px-4 pb-2">
            <TopMenu activeId={activeTopId} onSelect={onSelectTop} demo={demo} />
          </div>
        </header>
        {/* 제목 위계 — 목업은 24px bold 한 개가 화면을 이끈다. 기존 18px 는 각
            화면이 자체로 그리는 섹션 제목(18px)과 같은 크기라 둘이 중복으로
            읽혔다. 단 블록 높이 총합은 유지한다 — GpuAssistant 가
            h-[calc(100vh-10.5rem)] 로 헤더+제목 높이를 하드코딩해 두었다.
            text-lg(leading 28px) → text-2xl(leading 32px) 의 +4px 를
            gap-3 → gap-2 의 -4px 로 상쇄한다. */}
        <main className="flex flex-1 flex-col gap-2 p-4">
          <h1 className="text-2xl leading-8 font-bold tracking-tight">{title}</h1>
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
