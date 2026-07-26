// KpiTile — GPU 관제 규약의 KPI 타일: 상단 컬러 라인(상태) + 라벨 + 대형 값 +
// 보조 텍스트. Overview 의 KpiTile 패턴을 일반화한 공용 버전이다.
import { Card, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

export interface KpiTileProps {
  label: string;
  /** null → Skeleton (로딩) */
  value: string | null;
  hint?: string;
  /** 상태 톤 — 카드 상단 3px 라인 색으로 표현(관제 규약: 상단 라인 + 배지) */
  tone?: 'default' | 'danger' | 'warn' | 'ok' | 'info';
}

const TONE_COLOR: Record<NonNullable<KpiTileProps['tone']>, string | null> = {
  default: null,
  danger: 'var(--metric-fault)',
  warn: 'var(--metric-thermal)',
  ok: 'var(--metric-pod)',
  info: 'var(--metric-cpu)',
};

export default function KpiTile({ label, value, hint, tone = 'default' }: KpiTileProps) {
  const accent = TONE_COLOR[tone];
  return (
    <Card
      className="gap-0 py-0"
      style={accent ? { borderTop: `3px solid ${accent}` } : undefined}
    >
      <CardContent className="flex flex-col gap-1 p-3">
        <span className="text-muted-foreground text-xs">{label}</span>
        {value === null ? (
          <Skeleton className="h-7 w-16" />
        ) : (
          <span className="font-semibold text-2xl leading-tight tabular-nums">{value}</span>
        )}
        {hint ? <span className="text-muted-foreground text-[11px]">{hint}</span> : null}
      </CardContent>
    </Card>
  );
}
