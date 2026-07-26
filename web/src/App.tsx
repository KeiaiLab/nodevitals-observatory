// App — react-router 8.x data router 라우트 정의 (m5-design.md §4.2):
//   /login                          공개
//   /        RequireAuth + Shell    (index → /overview 리다이렉트)
//   ├─ /overview  ├─ /map  └─ /explorer
//   └─ /gpu (GpuLayout: DemoContext + DemoRail)
//      ├─ index(개요) ├─ health ├─ roadmap ├─ remediation ├─ validation └─ efficiency
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router';
import { AuthProvider, RequireAuth } from '@/auth/AuthContext';
import Shell from '@/components/layout/Shell';
import Explorer from '@/pages/Explorer';
import GpuEfficiency from '@/pages/gpu/GpuEfficiency';
import GpuHealth from '@/pages/gpu/GpuHealth';
import GpuLayout from '@/pages/gpu/GpuLayout';
import GpuOverview from '@/pages/gpu/GpuOverview';
import GpuRemediation from '@/pages/gpu/GpuRemediation';
import GpuRoadmap from '@/pages/gpu/GpuRoadmap';
import GpuValidation from '@/pages/gpu/GpuValidation';
import Login from '@/pages/Login';
import MapPage from '@/pages/Map';
import Overview from '@/pages/Overview';

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
      { index: true, element: <Navigate to="/overview" replace /> },
      { path: 'overview', element: <Overview /> },
      { path: 'map', element: <MapPage /> },
      { path: 'explorer', element: <Explorer /> },
      {
        path: 'gpu',
        element: <GpuLayout />,
        children: [
          { index: true, element: <GpuOverview /> },
          { path: 'health', element: <GpuHealth /> },
          { path: 'roadmap', element: <GpuRoadmap /> },
          { path: 'remediation', element: <GpuRemediation /> },
          { path: 'validation', element: <GpuValidation /> },
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
