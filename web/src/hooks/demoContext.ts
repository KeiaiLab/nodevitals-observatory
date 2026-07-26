// demoContext — GpuLayout 이 공급하는 데모 상태 컨텍스트. GPU 하위 페이지들은
// useDemo() 하나로 (모드 여부, 시나리오 스냅샷, 액션 실행)을 얻는다.
import { createContext, useContext } from 'react';
import type { ActionOutcome, DemoAction, DemoState } from '@/lib/demoApi';

export interface DemoContextValue {
  /** null = /demo/status 판별 중, false = 실서비스(demo off). */
  demoMode: boolean | null;
  /** demo on + 백필 완료 후의 최신 스냅샷 (5s 폴링, 진행 장면 2s). */
  state: DemoState | null;
  /** 액션 실행 — 409 는 예외가 아닌 결과({ok:false})로 온다. */
  act: (action: DemoAction, body?: { uuid?: string; reason?: string; note?: string }) => Promise<ActionOutcome>;
  /** 액션 직후 즉시 재조회. */
  refresh: () => void;
}

export const DemoContext = createContext<DemoContextValue>({
  demoMode: null,
  state: null,
  act: async () => ({ ok: false, status: 0, error: 'DemoProvider 밖에서 호출됨' }),
  refresh: () => {},
});

export function useDemo(): DemoContextValue {
  return useContext(DemoContext);
}
