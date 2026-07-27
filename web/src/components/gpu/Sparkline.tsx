// Sparkline — 의존성 없는 SVG 미니 차트.
//
// Recharts 를 쓰지 않는 이유: 이 컴포넌트가 한 화면에 10개 넘게 깔리는데
// Recharts 는 인스턴스마다 ResponsiveContainer + ResizeObserver 를 단다.
// 선 하나 그리는 데 그 비용을 낼 이유가 없고, CSP(default-src 'self') 아래에서
// 인라인 style 주입 경로를 하나라도 줄이는 편이 안전하다.
export interface SparklineProps {
  values: number[];
  /** 선 색 — CSS 변수명(예: 'var(--metric-gpu)'). */
  color?: string;
  height?: number;
  /** 선 아래를 옅게 채운다 — 밀도 높은 스트립에서 추세가 눈에 더 잘 들어온다. */
  area?: boolean;
  className?: string;
}

const VIEW_W = 100;

export default function Sparkline({
  values,
  color = 'var(--metric-gpu)',
  height = 28,
  area = true,
  className,
}: SparklineProps) {
  const clean = values.filter((v) => Number.isFinite(v));
  if (clean.length < 2) {
    // 점 하나로는 추세를 그릴 수 없다 — 빈 자리를 남기는 대신 기준선을 둔다.
    return (
      <svg
        viewBox={`0 0 ${VIEW_W} 10`}
        preserveAspectRatio="none"
        style={{ height }}
        className={`w-full ${className ?? ''}`}
        aria-hidden
      >
        <line x1="0" y1="5" x2={VIEW_W} y2="5" stroke="var(--border)" strokeWidth="0.6" />
      </svg>
    );
  }

  const min = Math.min(...clean);
  const max = Math.max(...clean);
  // 평평한 계열은 span 이 0 이라 나눗셈이 폭발한다 — 가운데 선으로 눕힌다.
  const span = max - min || 1;
  const stepX = VIEW_W / (clean.length - 1);
  const pts = clean.map((v, i) => {
    const x = i * stepX;
    const y = 10 - ((v - min) / span) * 8 - 1; // 위아래 1 여백
    return [x, y] as const;
  });
  const line = pts.map(([x, y]) => `${x.toFixed(2)},${y.toFixed(2)}`).join(' ');
  const last = pts[pts.length - 1];

  return (
    <svg
      viewBox={`0 0 ${VIEW_W} 10`}
      preserveAspectRatio="none"
      style={{ height }}
      className={`w-full ${className ?? ''}`}
      aria-hidden
    >
      {area ? (
        <polygon points={`0,10 ${line} ${VIEW_W},10`} fill={color} opacity="0.12" />
      ) : null}
      <polyline
        points={line}
        fill="none"
        stroke={color}
        strokeWidth="0.9"
        strokeLinejoin="round"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
      {/* 마지막 점 — "지금 값"이 어디인지 한눈에. preserveAspectRatio="none" 이라
          원은 찌그러지므로 세로선으로 표시한다(스케일 왜곡에 안전). */}
      <line
        x1={last[0]}
        y1={last[1] - 1.2}
        x2={last[0]}
        y2={last[1] + 1.2}
        stroke={color}
        strokeWidth="1.2"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
