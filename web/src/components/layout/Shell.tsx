// Shell — 공통 셸: 사이드바 네비(Overview/Map/Explorer) + 상단 바(테마 토글·로그아웃).
// m5-design.md §4.3. 라우트 콘텐츠는 <Outlet/> 으로 렌더된다.
import { ChartLine, Grid3x3, LayoutDashboard, LogOut, Moon, Sun } from 'lucide-react';
import { useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router';
import { useAuth } from '@/auth/AuthContext';
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

const NAV_ITEMS = [
  { to: '/overview', label: 'Overview', icon: LayoutDashboard },
  { to: '/map', label: 'Map', icon: Grid3x3 },
  { to: '/explorer', label: 'Explorer', icon: ChartLine },
] as const;

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

export default function Shell() {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const title =
    NAV_ITEMS.find((item) => location.pathname.startsWith(item.to))?.label ?? 'Observatory';

  async function handleLogout() {
    await logout();
    navigate('/login', { replace: true });
  }

  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarHeader>
          <div className="px-2 py-1.5 text-sm font-semibold">NodeVitals Observatory</div>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>콘솔</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {NAV_ITEMS.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton asChild isActive={location.pathname.startsWith(item.to)}>
                      <NavLink to={item.to}>
                        <item.icon />
                        <span>{item.label}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-5" />
          <h1 className="text-sm font-medium">{title}</h1>
          <div className="ml-auto flex items-center gap-1">
            <ThemeToggle />
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              <LogOut />
              로그아웃
            </Button>
          </div>
        </header>
        <main className="flex-1 p-4">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
