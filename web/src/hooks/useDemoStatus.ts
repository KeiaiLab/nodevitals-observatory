// useDemoStatus — /api/v1/demo/status 1회 조회의 모듈 캐시 훅. Shell 네비의
// 데모 전용 항목 노출 판정 전용이라 재조회가 필요 없다(모드는 프로세스
// 수명과 같다).
import { useEffect, useState } from 'react';
import { type DemoStatus, fetchDemoStatus } from '@/lib/demoApi';

let cached: Promise<DemoStatus> | null = null;

export function getDemoStatus(): Promise<DemoStatus> {
  cached ??= fetchDemoStatus();
  return cached;
}

/** null = 판별 중. */
export function useDemoStatus(): DemoStatus | null {
  const [status, setStatus] = useState<DemoStatus | null>(null);
  useEffect(() => {
    let live = true;
    getDemoStatus().then((s) => {
      if (live) setStatus(s);
    });
    return () => {
      live = false;
    };
  }, []);
  return status;
}
