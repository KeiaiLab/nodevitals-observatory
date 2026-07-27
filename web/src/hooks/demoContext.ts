// demoContext — GpuLayout 이 공급하는 데모 상태 컨텍스트. GPU 하위 페이지들은
// useDemo() 하나로 (모드 여부, 시나리오 스냅샷, 액션 실행)을 얻는다.
import { createContext, useContext } from 'react';
import type { ActionOutcome, DemoAction, DemoActionBody, DemoState } from '@/lib/demoApi';

export interface DemoContextValue {
  /** null = /demo/status 판별 중, false = 실서비스(demo off). */
  demoMode: boolean | null;
  /** demo on + 백필 완료 후의 최신 스냅샷 (5s 폴링, 진행 장면 2s). */
  state: DemoState | null;
  /** 액션 실행 — 409 는 예외가 아닌 결과({ok:false})로 온다.
   *  바디 타입은 demoApi 의 DemoActionBody 를 그대로 쓴다(액션별 필드의 합집합) —
   *  좁히면 호출부가 객체 리터럴 초과 속성 검사에 걸려 우회 헬퍼가 필요해진다. */
  act: (action: DemoAction, body?: DemoActionBody) => Promise<ActionOutcome>;
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
