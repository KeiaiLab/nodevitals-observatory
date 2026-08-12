// KpiTile — GPU 관제 규약의 KPI 타일: 톤 아이콘(원형 배경) + 라벨 +
// 대형 값(+보조·배지) + 힌트 + 하단 미니 시각화(스파크라인 / 막대 / 진행바).
// 목업 6카드(전체 GPU·활성 장애·수집 지연·저활용·자동복구·활성 워크로드)를
// 이 슬롯 조합으로 전부 그린다. 신규 prop 은 전부 optional — 기존 호출부 불변.
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import Sparkline from '@/components/gpu/Sparkline';

export type KpiTone = 'default' | 'danger' | 'warn' | 'ok' | 'info';

export interface KpiTileProps {
  label: string;
  /** null → Skeleton (로딩) */
  value: string | null;
  hint?: string;
  /** 상태 톤 — 카드 상단 3px 라인 색으로 표현(관제 규약: 상단 라인 + 배지) */
  tone?: KpiTone;
  /** 제목 왼쪽 아이콘(lucide 등) */
  icon?: React.ReactNode;
  /** 값 옆 배지 — 예: Critical 2 / Major 5 */
  badges?: Array<{ text: string; tone?: Exclude<KpiTone, 'default'> }>;
  /** 하단 미니 스파크라인(추세) */
  spark?: number[];
  /** 하단 미니 막대(이산 사건 분포) — 선형인 Sparkline 으로는 못 그리는 축 */
  bars?: number[];
  /** 하단 진행바 0~100 */
  progress?: number;
  /** 값 뒤 보조 텍스트 — 예: "/ 3,376" */
  sub?: string;
}

const TONE_COLOR: Record<KpiTone, string | null> = {
  default: null,
  danger: 'var(--metric-fault)',
  warn: 'var(--metric-thermal)',
  ok: 'var(--metric-pod)',
  info: 'var(--metric-cpu)',
};

const FALLBACK_ACCENT = 'var(--metric-gpu)';

/** 미니 막대 — 의존성 0 SVG(Sparkline 과 같은 이유: 타일마다 차트 라이브러리를
 *  띄울 이유가 없다). 값은 최대치 기준 상대 높이로만 읽히면 충분하다. */
function Bars({ values, color }: { values: number[]; color: string }) {
  const clean = values.filter((v) => Number.isFinite(v));
  if (clean.length === 0) {
    return (
      <svg viewBox="0 0 100 10" preserveAspectRatio="none" style={{ height: 36 }} className="w-full" aria-hidden>
        <line x1="0" y1="10" x2="100" y2="10" stroke="var(--border)" strokeWidth="0.6" />
      </svg>
    );
  }
  // 전부 0 인 계열은 max=0 이라 나눗셈이 폭발한다 — 바닥 눈금만 남긴다.
  const max = Math.max(...clean, 0) || 1;
  const step = 100 / clean.length;
  const width = step * 0.62;
  return (
    <svg viewBox="0 0 100 10" preserveAspectRatio="none" style={{ height: 36 }} className="w-full" aria-hidden>
      {clean.map((v, i) => {
        const h = Math.max(0.4, (Math.max(v, 0) / max) * 10);
        return (
          // biome-ignore lint/suspicious/noArrayIndexKey: 순수 수치 계열 — 재정렬·삽입이 없다
          <rect key={i} x={i * step + (step - width) / 2} y={10 - h} width={width} height={h} fill={color} />
        );
      })}
    </svg>
  );
}

export default function KpiTile({
  label,
  value,
  hint,
  tone = 'default',
  icon,
  badges,
  spark,
  bars,
  progress,
  sub,
}: KpiTileProps) {
  // 목업(demo1) 타일은 상단 컬러 라인이 없다 — 톤은 아이콘 원형 배경 + 미니차트
  // 색으로만 말한다. 상단 3px 라인은 KPI 6칸 줄 전체를 알록달록하게 만든다.
  const accent = TONE_COLOR[tone];
  const vizColor = accent ?? FALLBACK_ACCENT;
  return (
    <Card className="gap-0 py-0">
      <CardContent className="flex flex-col gap-1 p-4">
        <span className="flex items-center gap-2 text-muted-foreground text-xs">
          {icon ? (
            <span
              className="inline-flex size-6 shrink-0 items-center justify-center rounded-full [&>svg]:size-3.5"
              style={{
                color: vizColor,
                backgroundColor: `color-mix(in srgb, ${vizColor} 12%, transparent)`,
              }}
              aria-hidden
            >
              {icon}
            </span>
          ) : null}
          {label}
        </span>

        {value === null ? (
          <Skeleton className="h-7 w-16" />
        ) : (
          <span className="flex flex-wrap items-baseline gap-x-1.5 gap-y-1">
            <span className="font-semibold text-3xl leading-tight tabular-nums">{value}</span>
            {sub ? <span className="text-muted-foreground text-sm tabular-nums">{sub}</span> : null}
            {badges?.map((b) => {
              const c = TONE_COLOR[b.tone ?? 'info'] ?? FALLBACK_ACCENT;
              return (
                <Badge
                  key={b.text}
                  variant="outline"
                  className="px-1 py-0 text-[10px]"
                  style={{
                    color: c,
                    borderColor: c,
                    backgroundColor: `color-mix(in srgb, ${c} 12%, transparent)`,
                  }}
                >
                  {b.text}
                </Badge>
              );
            })}
          </span>
        )}

        {hint ? <span className="text-muted-foreground text-[11px]">{hint}</span> : null}

        {spark ? <Sparkline values={spark} color={vizColor} height={36} className="mt-1" /> : null}
        {bars ? (
          <span className="mt-1 block">
            <Bars values={bars} color={vizColor} />
          </span>
        ) : null}
        {progress != null ? (
          // 진행바도 타일 톤을 따른다 — 기본 bg-primary(검정)만 남으면 옆 타일의
          // 컬러 미니차트들 사이에서 이 카드만 흑백으로 튄다.
          <Progress
            value={Math.min(100, Math.max(0, progress))}
            className="mt-3 h-1.5 [&>[data-slot=progress-indicator]]:bg-[var(--kpi-viz)]"
            style={
              {
                '--kpi-viz': vizColor,
                backgroundColor: `color-mix(in srgb, ${vizColor} 20%, transparent)`,
              } as React.CSSProperties
            }
          />
        ) : null}
      </CardContent>
    </Card>
  );
}
