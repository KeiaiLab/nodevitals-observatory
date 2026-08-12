// usePolledQuery — Overview/Map 의 useEffect+setInterval 폴링 패턴을 훅으로
// 일반화한다. 탭이 숨겨지면(visibilitychange) 폴링을 멈춘다 — 7,000셀 벽면의
// 백그라운드 낭비 방지.
import { useCallback, useEffect, useRef, useState } from 'react';
import { UnauthorizedError } from '@/lib/api';

export interface PolledQuery<T> {
  data: T | null;
  error: string | null;
  updatedAt: number | null;
  refresh: () => void;
}

export function usePolledQuery<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
  deps: readonly unknown[] = [],
): PolledQuery<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [updatedAt, setUpdatedAt] = useState<number | null>(null);
  // fetcher 는 렌더마다 새 참조가 되기 쉬워 ref 로 고정하고, 재구독 트리거는
  // 호출자가 deps 로 명시한다(콜백 아이덴티티에 이펙트를 묶지 않는 계약).
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const [tick, setTick] = useState(0);

  const refresh = useCallback(() => setTick((n) => n + 1), []);

  useEffect(() => {
    let cancelled = false;

    async function run() {
      try {
        const result = await fetcherRef.current();
        if (cancelled) return;
        setData(result);
        setError(null);
        setUpdatedAt(Date.now());
      } catch (err) {
        if (cancelled) return;
        // 401 은 AuthContext 가 로그인 화면 전환으로 처리한다 — 여기서 오류
        // 배너를 겹쳐 그리지 않는다(Overview 기존 관례).
        if (err instanceof UnauthorizedError) return;
        setError(err instanceof Error ? err.message : String(err));
      }
    }

    run();
    const id = window.setInterval(() => {
      if (!document.hidden) run();
    }, intervalMs);
    const onVisible = () => {
      if (!document.hidden) run();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      cancelled = true;
      window.clearInterval(id);
      document.removeEventListener('visibilitychange', onVisible);
    };
    // eslint 부재 환경 — deps 는 호출자가 명시한 재구독 신호다.
    // biome-ignore lint/correctness/useExhaustiveDependencies: fetcher 는 ref 로 고정
  }, [intervalMs, tick, ...deps]);

  return { data, error, updatedAt, refresh };
}
