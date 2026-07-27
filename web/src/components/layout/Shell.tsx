// Shell — 공통 셸: 사이드바 네비(제품 IA 8 대분류) + 상단 바(테마·로그아웃).
// 라우트 콘텐츠는 <Outlet/> 으로 렌더된다.
//
// 메뉴 구조 SSOT = lib/navigation.ts. 구현된 화면만 링크를 갖고 나머지는
// 비활성으로 보인다(데모). 실서비스에서는 미구현 항목을 감춰 실제 제공 화면만
// 남는다 — 관제 콘솔에 "준비 중"이 즐비하면 그 자체가 결함으로 읽힌다.
import { ChevronDown, LogOut, Moon, Sun } from 'lucide-react';
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
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import {
  NAV_SECTIONS,
  type NavLeaf,
  type NavSection,
  sectionForPath,
  titleForPath,
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

/** 대분류 하나. 구현되지 않은 항목(to 없음)은 비활성으로 렌더한다 — 눌러서
 *  빈 화면이 나오는 것보다 "아직 없다"가 정직하고, 동시에 제품 범위를 보여준다.
 *  실서비스(데모 off)에서는 미구현 항목을 아예 감춘다(제품 IA 는 데모 서사다). */
function NavSectionGroup({
  section,
  pathname,
  defaultOpen,
  showPlanned,
}: {
  section: NavSection;
  pathname: string;
  defaultOpen: boolean;
  showPlanned: boolean;
}) {
  const items = showPlanned ? section.items : section.items.filter((i) => i.to);
  if (items.length === 0) return null;
  const readyCount = section.items.filter((i) => i.to).length;

  return (
    <Collapsible defaultOpen={defaultOpen} className="group/collapsible">
      <SidebarGroup className="py-0.5">
        <SidebarGroupLabel asChild>
          <CollapsibleTrigger className="flex w-full items-center gap-2">
            <section.icon className="size-3.5 shrink-0" />
            <span className="truncate">{section.label}</span>
            {showPlanned ? (
              <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">
                {readyCount}/{section.items.length}
              </span>
            ) : null}
            <ChevronDown className="size-3.5 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-180" />
          </CollapsibleTrigger>
        </SidebarGroupLabel>
        <CollapsibleContent>
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
        </CollapsibleContent>
      </SidebarGroup>
    </Collapsible>
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
  const openSection = sectionForPath(location.pathname);
  const showPlanned = demoStatus?.enabled === true;

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
          {NAV_SECTIONS.map((section) => (
            <NavSectionGroup
              key={section.id}
              section={section}
              pathname={location.pathname}
              defaultOpen={section.id === openSection}
              showPlanned={showPlanned}
            />
          ))}
        </SidebarContent>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-5" />
          <h1 className="text-sm font-medium">{title}</h1>
          <div className="ml-auto flex items-center gap-1">
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
        <main className="flex-1 p-4">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
