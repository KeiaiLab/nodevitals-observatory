// App — react-router 8.x data router 라우트 정의 (m5-design.md §4.2):
//   /login                          공개
//   /        RequireAuth + Shell    (index → /overview 리다이렉트)
//   ├─ /overview  ├─ /map  └─ /explorer
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router';
import { AuthProvider, RequireAuth } from '@/auth/AuthContext';
import Shell from '@/components/layout/Shell';
import Explorer from '@/pages/Explorer';
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
