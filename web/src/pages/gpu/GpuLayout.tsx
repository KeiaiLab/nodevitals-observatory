// GpuLayout — /gpu 하위 라우트의 레이아웃. DemoContext 공급(모드 감지 +
// /demo/state 폴링 + 액션) + DemoRail(데모 모드 한정) + Outlet.
// 폴링 주기: 평시 5s, 진행 장면(드레인·번인)은 2s 로 가속 — 스테퍼·카운터가
// 라이브로 움직여야 시연이 산다.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Outlet } from 'react-router';
import DemoRail from '@/components/gpu/DemoRail';
import { DemoContext, type DemoContextValue } from '@/hooks/demoContext';
import { useDemoStatus } from '@/hooks/useDemoStatus';
import {
  type DemoAction,
  type DemoState,
  fetchDemoState,
  runDemoAction,
} from '@/lib/demoApi';

function pollIntervalFor(state: DemoState | null): number {
  if (!state) return 5000;
  const phase = state.scenario.phase;
  const busy =
    state.scenario.victim.drain.active ||
    state.scenario.victim.burnin.active ||
    phase === 'draining' ||
    phase === 'replacing';
  return busy ? 2000 : 5000;
}

export default function GpuLayout() {
  const status = useDemoStatus();
  const demoMode = status === null ? null : status.enabled;

  const [state, setState] = useState<DemoState | null>(null);
  const stateRef = useRef(state);
  stateRef.current = state;
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((n) => n + 1), []);

  useEffect(() => {
    if (!demoMode) return;
    let cancelled = false;
    let timer = 0;

    async function poll() {
      try {
        const next = await fetchDemoState();
        if (cancelled) return;
        setState(next);
      } catch {
        // 401(세션 만료)은 페이지의 TSDB 질의가 AuthContext 전환을 일으킨다.
        // 503(백필 중)은 다음 폴링에서 자연 회복 — 여기선 조용히 재시도.
      }
      if (cancelled) return;
      timer = window.setTimeout(poll, document.hidden ? 10_000 : pollIntervalFor(stateRef.current));
    }

    poll();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [demoMode, tick]);

  const act = useCallback(
    async (action: DemoAction, body?: { uuid?: string; reason?: string; note?: string }) => {
      const outcome = await runDemoAction(action, body);
      // 성공/실패와 무관하게 즉시 재조회 — 화면이 서버 진실을 바로 반영한다.
      refresh();
      return outcome;
    },
    [refresh],
  );

  const value: DemoContextValue = useMemo(
    () => ({ demoMode, state, act, refresh }),
    [demoMode, state, act, refresh],
  );

  return (
    <DemoContext.Provider value={value}>
      <Outlet />
      {demoMode ? <DemoRail /> : null}
    </DemoContext.Provider>
  );
}
