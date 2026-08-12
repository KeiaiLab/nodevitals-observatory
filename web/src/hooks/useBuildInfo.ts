// useBuildInfo — 지금 콘솔이 붙어 있는 서버가 *어느 빌드* 이고 *언제 반영* 됐는지.
//
// 왜 필요한가: 시연 중 "지금 보시는 게 어느 빌드인가" 에 답할 수단이 화면에 없었다.
// 사이드바 하단의 「마지막 업데이트」는 *데이터 갱신* 시각이라 배포와 무관하고,
// 배포 버전을 알려면 kubectl 로 이미지 태그를 캐야 했다.
//
// 폴링하지 않는다 — 빌드 식별자는 프로세스 수명 동안 불변이고, startedAt 도
// 파드가 새로 떠야만 바뀐다. 재배포되면 어차피 브라우저 세션이 끊기거나
// 사용자가 새로고침한다. 30초마다 물어볼 이유가 없다.
import { useEffect, useState } from 'react';

import type { ApiEnvelope } from '@/lib/api';

export interface BuildInfo {
  version: string;
  commit: string;
  /** 이미지 빌드 시각(RFC3339). 로컬 빌드 등 미주입이면 없다. */
  builtAt?: string;
  /** 프로세스 기동 시각 = 이 빌드가 실제로 서비스에 반영된 시각. */
  startedAt: string;
  uptimeSeconds: number;
}

/** 서버가 답하지 못하면 null — 호출부는 표기를 생략한다(빈 값 렌더 금지). */
export function useBuildInfo(): BuildInfo | null {
  const [info, setInfo] = useState<BuildInfo | null>(null);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const res = await fetch('/api/v1/version', { credentials: 'include' });
        if (!res.ok) return;
        const body = (await res.json()) as ApiEnvelope<BuildInfo>;
        if (alive && body.data?.version) setInfo(body.data);
      } catch {
        // 구 백엔드(엔드포인트 부재)·네트워크 오류 → 표기 생략. 화면을 깨뜨리지 않는다.
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  return info;
}

/** "2026-07-29T08:07:11Z" → "2026.07.29 17:07" (로컬 시간대). 파싱 실패는 원문 유지. */
export function formatBuildTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}.${p(d.getMonth() + 1)}.${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}
