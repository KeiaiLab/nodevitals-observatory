// App — react-router 8.x data router 라우트 정의 (m5-design.md §4.2):
//   /login                          공개
//   /        RequireAuth + Shell    (index → /overview 리다이렉트)
//   ├─ /overview  ├─ /map  └─ /explorer
//   └─ /gpu (GpuLayout: DemoContext + ContactBubble)
//      ├─ index(개요) ├─ serving ├─ health ├─ incident ├─ assistant ├─ ruleset
//      ├─ roadmap ├─ remediation ├─ validation ├─ idle └─ efficiency
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router';
import { AuthProvider, RequireAuth } from '@/auth/AuthContext';
import Shell from '@/components/layout/Shell';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemoStatus } from '@/hooks/useDemoStatus';
import Explorer from '@/pages/Explorer';
import GpuAssistant from '@/pages/gpu/GpuAssistant';
import GpuEfficiency from '@/pages/gpu/GpuEfficiency';
import GpuHealth from '@/pages/gpu/GpuHealth';
import GpuIdle from '@/pages/gpu/GpuIdle';
import GpuIncident from '@/pages/gpu/GpuIncident';
import GpuLayout from '@/pages/gpu/GpuLayout';
import GpuOverview from '@/pages/gpu/GpuOverview';
import GpuRemediation from '@/pages/gpu/GpuRemediation';
import GpuRoadmap from '@/pages/gpu/GpuRoadmap';
import GpuRuleset from '@/pages/gpu/GpuRuleset';
import GpuServing from '@/pages/gpu/GpuServing';
import GpuValidation from '@/pages/gpu/GpuValidation';
import Login from '@/pages/Login';
import MapPage from '@/pages/Map';
import Overview from '@/pages/Overview';

// 진입점 — 데모 인스턴스는 GPU 콘솔이 곧 제품이므로 /gpu 로, 실서비스는 기존
// /overview 로 보낸다(판별 중에는 스켈레톤).
//
// 시연 대본은 시나리오 1(룰셋)부터 시작하지만 여기를 /gpu/ruleset 로 바꾸지
// 않는다 — 로그인 없는 공개 데모라 대부분의 방문자는 발표자가 아니고, 첫 화면이
// 설정 표면이 되면 제품의 대표 화면(통합 상황판)을 못 보고 나간다. 발표자는
// 시연 시작 URL(/gpu/ruleset)을 직접 열고, 메뉴 순번 ①~⑥(lib/navigation.ts
// 규칙 3)이 그 뒤 순회를 안내한다.
function HomeRedirect() {
  const demoStatus = useDemoStatus();
  if (demoStatus === null) {
    return <Skeleton className="h-8 w-56" />;
  }
  return <Navigate to={demoStatus.enabled ? '/gpu' : '/overview'} replace />;
}

const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <Shell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <HomeRedirect /> },
      { path: 'overview', element: <Overview /> },
      { path: 'map', element: <MapPage /> },
      { path: 'explorer', element: <Explorer /> },
      {
        path: 'gpu',
        element: <GpuLayout />,
        children: [
          { index: true, element: <GpuOverview /> },
          { path: 'serving', element: <GpuServing /> },
          { path: 'health', element: <GpuHealth /> },
          { path: 'incident', element: <GpuIncident /> },
          { path: 'assistant', element: <GpuAssistant /> },
          { path: 'ruleset', element: <GpuRuleset /> },
          { path: 'roadmap', element: <GpuRoadmap /> },
          { path: 'remediation', element: <GpuRemediation /> },
          { path: 'validation', element: <GpuValidation /> },
          { path: 'idle', element: <GpuIdle /> },
          { path: 'efficiency', element: <GpuEfficiency /> },
        ],
      },
    ],
  },
]);

export default function App() {
  return (
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>
  );
}
