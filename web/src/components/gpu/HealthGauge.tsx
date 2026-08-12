// HealthGauge — 번인 Health 점수 반원 게이지 (자체 SVG, React 속성만 — CSP-safe).
// 배경 3존 아크(0-60 장애빨강 / 60-85 주의주황 / 85-100 통과초록) 위에 값 아크를
// strokeDasharray 비율로 겹쳐 그린다. path d 는 극좌표 계산 함수로 생성한다.

export interface HealthGaugeProps {
  score: number;
  verdict?: string;
  /** SVG 폭(px). 기본 220. */
  size?: number;
}

// 판정 3존 — 색은 index.css 의 --metric-* 토큰(라이트/다크 동일 hue).
const ZONES = [
  { from: 0, to: 60, color: 'var(--metric-fault)' },
  { from: 60, to: 85, color: 'var(--metric-thermal)' },
  { from: 85, to: 100, color: 'var(--metric-pod)' },
] as const;

/** score(0~100) → 반원 각도(도). 0점=180°(왼쪽 끝), 100점=0°(오른쪽 끝). */
function angleFor(pct: number): number {
  return 180 - (pct / 100) * 180;
}

/** 극좌표 → SVG 좌표. SVG 는 y 가 아래로 증가하므로 sin 을 빼서 위쪽 반원을 그린다. */
function polar(cx: number, cy: number, r: number, angleDeg: number): { x: number; y: number } {
  const rad = (angleDeg * Math.PI) / 180;
  return { x: cx + r * Math.cos(rad), y: cy - r * Math.sin(rad) };
}

/** fromPct→toPct 구간 반원 아크 path — 왼쪽에서 오른쪽으로 시계방향(sweep=1) 진행. */
function arcPath(cx: number, cy: number, r: number, fromPct: number, toPct: number): string {
  const start = polar(cx, cy, r, angleFor(fromPct));
  const end = polar(cx, cy, r, angleFor(toPct));
  return `M ${start.x.toFixed(2)} ${start.y.toFixed(2)} A ${r} ${r} 0 0 1 ${end.x.toFixed(2)} ${end.y.toFixed(2)}`;
}

/** verdict 텍스트 색 — 존 경계와 동일 기준(85 이상 초록, 60 이상 주황, 미만 빨강). */
function verdictColor(score: number): string {
  if (score >= 85) return 'var(--metric-pod)';
  if (score >= 60) return 'var(--metric-thermal)';
  return 'var(--metric-fault)';
}

export default function HealthGauge({ score, verdict, size = 220 }: HealthGaugeProps) {
  const clamped = Math.max(0, Math.min(100, score));
  const zoneWidth = Math.max(10, Math.round(size * 0.055));
  const pad = zoneWidth / 2 + 2;
  const cx = size / 2;
  const cy = size / 2;
  const r = size / 2 - pad;
  const height = size / 2 + pad;
  const arcLen = Math.PI * r;
  const valueLen = (clamped / 100) * arcLen;

  return (
    <div className="flex flex-col items-center gap-1">
      <svg
        width={size}
        height={height}
        viewBox={`0 0 ${size} ${height}`}
        role="img"
        aria-label={`Health ${Math.round(clamped)} / 100`}
      >
        {ZONES.map((z) => (
          <path
            key={z.from}
            d={arcPath(cx, cy, r, z.from, z.to)}
            fill="none"
            stroke={z.color}
            strokeWidth={zoneWidth}
            strokeLinecap="butt"
            opacity={0.35}
          />
        ))}
        <path
          d={arcPath(cx, cy, r, 0, 100)}
          fill="none"
          stroke="var(--foreground)"
          strokeWidth={Math.max(6, Math.round(zoneWidth * 0.62))}
          strokeLinecap={clamped > 0 ? 'round' : 'butt'}
          strokeDasharray={`${valueLen.toFixed(2)} ${(arcLen + zoneWidth).toFixed(2)}`}
        />
        <text
          x={cx}
          y={cy - size * 0.06}
          textAnchor="middle"
          fill="var(--foreground)"
          fontSize={size * 0.19}
          fontWeight={700}
        >
          {Math.round(clamped)}
        </text>
        <text
          x={cx}
          y={cy - size * 0.06 + size * 0.075}
          textAnchor="middle"
          fill="var(--muted-foreground)"
          fontSize={size * 0.055}
        >
          /100
        </text>
      </svg>
      {verdict ? (
        <span className="font-medium text-sm" style={{ color: verdictColor(clamped) }}>
          {verdict}
        </span>
      ) : null}
    </div>
  );
}
