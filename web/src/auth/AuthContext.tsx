// AuthContext — 세션 상태(unknown|authed|anon) + RequireAuth 가드 (m5-design.md §4.2).
// 마운트 시 GET /api/v1/labels 프로브로 세션 유효성 판정: 200 → authed, 401 → anon.
// 보호 API 어디서든 401 이 나면 api.ts 의 핸들러 경유로 anon 전환 → /login 이동.
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { Navigate, useLocation } from 'react-router';
import { Skeleton } from '@/components/ui/skeleton';
import { api, setUnauthorizedHandler } from '@/lib/api';

type AuthStatus = 'unknown' | 'authed' | 'anon';

interface AuthContextValue {
  status: AuthStatus;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('unknown');

  useEffect(() => {
    setUnauthorizedHandler(() => setStatus('anon'));
    let cancelled = false;
    void (async () => {
      try {
        await api.labels();
        if (!cancelled) setStatus('authed');
      } catch {
        // 401 은 핸들러가 이미 anon 전환 — 네트워크 오류도 로그인 화면으로 보낸다
        // (unknown 고착 = 무한 스켈레톤 방지).
        if (!cancelled) setStatus('anon');
      }
    })();
    return () => {
      cancelled = true;
      setUnauthorizedHandler(null);
    };
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    await api.login(username, password);
    setStatus('authed');
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setStatus('anon');
    }
  }, []);

  const value = useMemo(() => ({ status, login, logout }), [status, login, logout]);

  return <AuthContext value={value}>{children}</AuthContext>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('observatory: useAuth 는 AuthProvider 안에서만 호출할 수 있다');
  }
  return ctx;
}

/** 보호 라우트 가드 — unknown 동안 스켈레톤, anon 이면 /login 으로(복귀 경로 보존). */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const location = useLocation();

  if (status === 'unknown') {
    return (
      <div className="flex min-h-svh flex-col items-center justify-center gap-3">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-4 w-40" />
      </div>
    );
  }
  if (status === 'anon') {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }
  return <>{children}</>;
}
