// Login — 사용자/비번 → POST /api/v1/auth/login. 실패는 단일 메시지
// (사용자/비번 구분 없음 — 계정 열거 방지, m5-design.md §4.4).
import { type FormEvent, useState } from 'react';
import { Navigate, useLocation, useNavigate } from 'react-router';
import { useAuth } from '@/auth/AuthContext';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

export default function Login() {
  const { status, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [failed, setFailed] = useState(false);
  const [pending, setPending] = useState(false);

  const from =
    (location.state as { from?: { pathname?: string } } | null)?.from?.pathname ?? '/overview';

  if (status === 'authed') {
    return <Navigate to={from} replace />;
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setFailed(false);
    try {
      await login(username, password);
      navigate(from, { replace: true });
    } catch {
      setFailed(true);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>NodeVitals Observatory</CardTitle>
          <CardDescription>관제 콘솔 로그인</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="username">사용자</Label>
              <Input
                id="username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">비밀번호</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            {failed && (
              <Alert variant="destructive">
                <AlertDescription>인증에 실패했다.</AlertDescription>
              </Alert>
            )}
            <Button type="submit" disabled={pending}>
              {pending ? '로그인 중…' : '로그인'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
