// Shell — 공통 셸: 사이드바 네비(콘솔 3 + GPU 플릿 그룹) + 상단 바(테마 토글·
// 로그아웃). m5-design.md §4.3. 라우트 콘텐츠는 <Outlet/> 으로 렌더된다.
// GPU 플릿의 데모 전용 항목(자동복구·검증·로드맵)은 /api/v1/demo/status 감지
// 결과(demo on)일 때만 노출한다 — 실서비스 인스턴스에서는 존재 자체가 숨는다.
import {
  Activity,
  ChartLine,
  ClipboardCheck,
  Gauge,
  Grid3x3,
  LayoutDashboard,
  LogOut,
  Moon,
  Server,
  ShieldAlert,
  Sparkles,
  Sun,
} from 'lucide-react';
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

interface NavItem {
  to: string;
  label: string;
  icon: typeof LayoutDashboard;
  /** exact=true 는 정확 일치로만 active 판정 (/gpu 인덱스처럼 하위 경로 보유 시). */
  exact?: boolean;
  demoOnly?: boolean;
}

const CONSOLE_NAV: readonly NavItem[] = [
  { to: '/overview', label: 'Overview', icon: LayoutDashboard },
  { to: '/map', label: 'Map', icon: Grid3x3 },
  { to: '/explorer', label: 'Explorer', icon: ChartLine },
] as const;

// 시연 6-Step 순서 그대로 배열한다 — 발표 동선과 네비 순서의 일치.
const GPU_NAV: readonly NavItem[] = [
  { to: '/gpu', label: 'GPU 플릿', icon: Server, exact: true },
  { to: '/gpu/health', label: '헬스 · 무음 장애', icon: Activity },
  { to: '/gpu/roadmap', label: '로드맵 · AI 분석', icon: Sparkles, demoOnly: true },
  { to: '/gpu/remediation', label: '자동복구 콘솔', icon: ShieldAlert, demoOnly: true },
  { to: '/gpu/validation', label: '검증 · 번인', icon: ClipboardCheck, demoOnly: true },
  { to: '/gpu/efficiency', label: '효율 · 활용', icon: Gauge },
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

function isItemActive(item: NavItem, pathname: string): boolean {
  return item.exact ? pathname === item.to : pathname.startsWith(item.to);
}

function NavGroup({ label, items, pathname }: { label: string; items: readonly NavItem[]; pathname: string }) {
  return (
    <SidebarGroup>
      <SidebarGroupLabel>{label}</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.to}>
              <SidebarMenuButton asChild isActive={isItemActive(item, pathname)}>
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

  const gpuItems = GPU_NAV.filter((item) => !item.demoOnly || demoStatus?.enabled);

  // 타이틀은 최장 프리픽스 매치 — startsWith 첫 매치는 /gpu/health 에서
  // '/gpu' 를 먼저 잡는 오판을 낸다.
  const title =
    [...CONSOLE_NAV, ...GPU_NAV]
      .filter((item) => location.pathname === item.to || location.pathname.startsWith(`${item.to}/`) || (!item.exact && location.pathname.startsWith(item.to)))
      .sort((a, b) => b.to.length - a.to.length)[0]?.label ?? 'Observatory';

  async function handleLogout() {
    await logout();
    navigate('/login', { replace: true });
  }

  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarHeader>
          <div className="px-2 py-1.5 text-sm font-semibold">{brand}</div>
        </SidebarHeader>
        <SidebarContent>
          <NavGroup label="콘솔" items={CONSOLE_NAV} pathname={location.pathname} />
          <NavGroup label="GPU 플릿" items={gpuItems} pathname={location.pathname} />
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
