// CspLogo — CSP 카드 왼쪽 브랜드 아이콘. csp id 로 /csp/{id}.svg 를 가리킨다.
//
// 자산 파일명을 브랜드명이 아니라 *csp id* 로 두는 이유: 플릿 구성은
// OBSERVATORY_DEMO_FLEET 로 갈아끼울 수 있고, 그때 새 id 를 위한 자산만
// 얹으면 되기 때문이다. 자산이 없는 id 는 모노그램으로 떨어져 화면이 항상
// 성립한다 — 깨진 이미지 아이콘은 시연에서 그 자체로 결함처럼 보인다.
import { useState } from 'react';

/** 모노그램 폴백 색 — id 해시로 고정해 재렌더·재시딩에도 같은 색이 나온다. */
const FALLBACK_COLORS = [
  'var(--metric-gpu)',
  'var(--metric-thermal)',
  'var(--metric-cpu)',
  'var(--metric-mem)',
  'var(--metric-pod)',
] as const;

function fallbackColor(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) >>> 0;
  }
  return FALLBACK_COLORS[h % FALLBACK_COLORS.length];
}

export interface CspLogoProps {
  id: string;
  /** 표시명 — 모노그램 첫 글자에 쓴다. */
  name: string;
}

export default function CspLogo({ id, name }: CspLogoProps) {
  const [failed, setFailed] = useState(false);

  if (failed) {
    return (
      <span
        aria-hidden
        className="flex size-7 shrink-0 items-center justify-center rounded-md text-[11px] font-semibold text-white"
        style={{ backgroundColor: fallbackColor(id) }}
      >
        {name.trim().charAt(0) || '?'}
      </span>
    );
  }

  return (
    // alt 는 빈 문자열 — 바로 오른쪽에 표시명이 텍스트로 있어, 대체 텍스트를
    // 넣으면 스크린리더가 같은 이름을 두 번 읽는다.
    <img
      src={`/csp/${id}.svg`}
      alt=""
      aria-hidden
      width={28}
      height={28}
      className="size-7 shrink-0 rounded-md"
      onError={() => setFailed(true)}
    />
  );
}
