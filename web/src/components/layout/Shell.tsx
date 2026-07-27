// Shell — 공통 셸: 사이드바 네비(제품 IA 8 대분류) + 상단 바(테마·로그아웃).
// 라우트 콘텐츠는 <Outlet/> 으로 렌더된다.
//
// 메뉴 구조 SSOT = lib/navigation.ts. 구현된 화면만 링크를 갖고 나머지는
// 비활성으로 보인다(데모). 실서비스에서는 미구현 항목을 감춰 실제 제공 화면만
// 남는다 — 관제 콘솔에 "준비 중"이 즐비하면 그 자체가 결함으로 읽힌다.
import { LogOut, Moon, Sun } from 'lucide-react';
import { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router';
import { useAuth } from '@/auth/AuthContext';
import { productName } from '@/lib/branding';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import {
  Sidebar,
  SidebarContent,
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
import { useDemoStatus } from '@/hooks/useDemoStatus';
import {
  NAV_TOP,
  type NavLeaf,
  type NavTop,
  readyRatio,
  titleForPath,
  topById,
  topForPath,
} from '@/lib/navigation';

const THEME_KEY = 'nv-theme';

function initialDark(): boolean {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved === 'dark') return true;
  if (saved === 'light') return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
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

/** 좌측 서브메뉴 — 상단에서 고른 대메뉴의 항목만 렌더한다.
 *  구현되지 않은 항목(to 없음)은 비활성 + "준비 중" — 눌러서 빈 화면이 나오는
 *  것보다 정직하고, 동시에 제품 범위를 보여준다. 실서비스(데모 off)에서는
 *  아예 감춘다(관제 콘솔에 "준비 중"이 즐비하면 그 자체가 결함으로 읽힌다). */
function NavSubMenu({
  top,
  pathname,
  showPlanned,
}: {
  top: NavTop;
  pathname: string;
  showPlanned: boolean;
}) {
  return (
    <>
      {top.groups.map((group, gi) => {
        const items = showPlanned ? group.items : group.items.filter((i) => i.to);
        if (items.length === 0) return null;
        return (
          <SidebarGroup key={group.label ?? `g${gi}`} className="py-1">
            {group.label ? <SidebarGroupLabel>{group.label}</SidebarGroupLabel> : null}
            <SidebarGroupContent>
              <SidebarMenu>
                {items.map((item) => (
                  <SidebarMenuItem key={item.label}>
                    {item.to ? (
                      <SidebarMenuButton asChild isActive={isLeafActive(item, pathname)}>
                        <NavLink to={item.to}>
                          <span>{item.label}</span>
                        </NavLink>
                      </SidebarMenuButton>
                    ) : (
                      <SidebarMenuButton
                        disabled
                        aria-disabled
                        title="준비 중 — 아직 제공되지 않는 화면입니다"
                        className="cursor-not-allowed"
                      >
                        <span>{item.label}</span>
                        <span className="ml-auto text-[10px] text-muted-foreground">준비 중</span>
                      </SidebarMenuButton>
                    )}
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        );
      })}
    </>
  );
}

/** 상단 대메뉴 6개. 클릭하면 그 대메뉴의 첫 구현 화면으로 이동하고 좌측
 *  서브메뉴가 통째로 바뀐다 — 구현 화면이 없는 대메뉴는 이동 없이 좌측만
 *  전환한다(빈 라우트로 보내면 404 처럼 보인다). */
function TopMenu({
  activeId,
  onSelect,
  showPlanned,
}: {
  activeId: string;
  onSelect: (id: string) => void;
  showPlanned: boolean;
}) {
  return (
    <nav className="flex items-center gap-0.5 overflow-x-auto" aria-label="대메뉴">
      {NAV_TOP.map((top) => {
        const ratio = readyRatio(top);
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
            {showPlanned ? (
              <span className="text-[10px] tabular-nums opacity-60">
                {ratio.ready}/{ratio.total}
              </span>
            ) : null}
          </button>
        );
      })}
    </nav>
  );
}

export default function Shell() {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const demoStatus = useDemoStatus();

  const brand = productName(demoStatus?.enabled);
  useEffect(() => {
    document.title = brand;
  }, [brand]);

  // 타이틀·기본 펼침 섹션은 최장 프리픽스 매치로 고른다(navigation.ts) —
  // startsWith 첫 매치는 /gpu/health 에서 '/gpu' 를 먼저 잡는 오판을 낸다.
  const title = titleForPath(location.pathname);
  const showPlanned = demoStatus?.enabled === true;

  // 상단 대메뉴 선택 = 경로에서 유도하되, 사용자가 직접 고른 것이 있으면 그쪽이
  // 우선한다 — 구현 화면이 없는 대메뉴(자원·토폴로지 등)를 눌렀을 때 경로는
  // 그대로여도 좌측이 바뀌어야 한다.
  const pathTop = topForPath(location.pathname);
  const [pickedTop, setPickedTop] = useState<string | null>(null);
  const activeTopId = pickedTop ?? pathTop;
  const activeTop = topById(activeTopId);

  // 경로가 바뀌면(링크 이동) 수동 선택을 놓아준다 — 안 그러면 좌측 메뉴가
  // 현재 화면과 다른 대메뉴에 머문다.
  useEffect(() => {
    setPickedTop(null);
  }, [location.pathname]);

  function onSelectTop(id: string) {
    setPickedTop(id);
    // 그 대메뉴에 구현 화면이 있으면 첫 화면으로 이동한다. 없으면 좌측만 바꾼다.
    const first = topById(id)
      .groups.flatMap((g) => g.items)
      .find((i) => i.to);
    if (first?.to && first.to !== location.pathname) navigate(first.to);
  }

  async function handleLogout() {
    await logout();
    navigate('/login', { replace: true });
  }

  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarHeader>
          {/* 데모 모드에서만 공동 브랜딩 — 실서비스 관제 콘솔에는 파트너
              로고가 붙지 않는다. */}
          <div className="flex items-center gap-2 px-2 py-1.5">
            {demoStatus?.enabled ? (
              <img
                src="/brand/lguplus.svg"
                alt="LG U+"
                width={46}
                height={15}
                className="h-4 w-auto shrink-0"
              />
            ) : null}
            <span className="truncate text-sm font-semibold">{brand}</span>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <NavSubMenu top={activeTop} pathname={location.pathname} showPlanned={showPlanned} />
        </SidebarContent>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-5" />
          <TopMenu activeId={activeTopId} onSelect={onSelectTop} showPlanned={showPlanned} />
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <ThemeToggle />
            {/* public demo 는 로그인 자체가 없다 — 로그아웃 버튼을 두면 방문자를
                자격증명 없는 로그인 화면에 가두게 되므로 숨긴다. */}
            {demoStatus?.public ? null : (
              <Button variant="ghost" size="sm" onClick={handleLogout}>
                <LogOut />
                로그아웃
              </Button>
            )}
          </div>
        </header>
        <main className="flex flex-1 flex-col gap-3 p-4">
          <h1 className="text-lg font-semibold">{title}</h1>
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
