// StatusBanner — 헬스 페이지 상단 상태 배너. 카드 상단 4px 컬러 라인(관제 규약:
// 상단 라인 = 상태 톤) + 대형 제목 + 설명. 색은 index.css 의 --metric-* 토큰만
// 사용한다(ok=pod 초록 / warn=thermal 주황 / suspect=fault 빨강).
import { Card, CardContent } from '@/components/ui/card';

export type StatusLevel = 'ok' | 'warn' | 'suspect';

export interface StatusBannerProps {
  level: StatusLevel;
  title: string;
  description: string;
  /** 단계 진입 시각(epoch ms) — 있으면 우측 상단에 표시 */
  since?: number;
}

const LEVEL_COLOR: Record<StatusLevel, string> = {
  ok: 'var(--metric-pod)',
  warn: 'var(--metric-thermal)',
  suspect: 'var(--metric-fault)',
};

export default function StatusBanner({ level, title, description, since }: StatusBannerProps) {
  return (
    <Card className="gap-0 py-0" style={{ borderTop: `4px solid ${LEVEL_COLOR[level]}` }}>
      <CardContent className="flex flex-col gap-1 p-4">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <span className="font-semibold text-2xl leading-tight">{title}</span>
          {since ? (
            <span className="text-muted-foreground text-xs tabular-nums">
              단계 진입 {new Date(since).toLocaleTimeString('ko-KR')}
            </span>
          ) : null}
        </div>
        <p className="text-muted-foreground text-sm leading-relaxed">{description}</p>
      </CardContent>
    </Card>
  );
}
