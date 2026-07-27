// CspRollupCards — CSP 4사 롤업 카드(데모 모드 전용). 카운트/할당율/장애는
// DemoState.fleet.csps 스냅샷, util 은 부모(GpuOverview)가
// nodevitals_demo_csp_util_avg_pct 1회 질의로 만든 맵을 받는다 — 카드가 각자
// 질의하지 않는다(4장 = 1 질의 계약).
//
// 어댑터는 전부 Mock 이다 — 이 데모는 실제 CSP API 에 붙지 않으며 화면이 그
// 사실을 숨기지 않는다(사용자 지시 2026-07-27). "실제 연동" 배지는 제거됐다.
import { Badge } from '@/components/ui/badge';
import { Card, CardContent } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import type { CSPSummary } from '@/lib/demoApi';
import { formatCount, formatPct } from '@/lib/gpuMetrics';

export interface CspRollupCardsProps {
  csps: CSPSummary[];
  /** csp id → 평균 util(%). null = 집계 질의 로딩 중(Skeleton). */
  utilByCsp: Record<string, number> | null;
}

export default function CspRollupCards({ csps, utilByCsp }: CspRollupCardsProps) {
  if (csps.length === 0) return null;
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="CSP 롤업">
      {csps.map((csp) => {
        return (
          <Card
            key={csp.id}
            className="gap-0 py-0"
            style={{ borderTop: '3px solid var(--muted)' }}
          >
            <CardContent className="flex flex-col gap-2 p-3">
              <div className="flex items-center justify-between gap-2">
                <span className="truncate font-medium text-sm" title={csp.display}>
                  {csp.display}
                </span>
                <Badge variant="secondary">Mock Adapter</Badge>
              </div>
              <dl className="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
                <div className="flex flex-col">
                  <dt className="text-muted-foreground">GPU</dt>
                  <dd className="font-semibold tabular-nums">{formatCount(csp.gpuTotal)}장</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-muted-foreground">할당율</dt>
                  <dd className="font-semibold tabular-nums">{formatPct(csp.allocPct)}</dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-muted-foreground">사용률</dt>
                  <dd className="font-semibold tabular-nums">
                    {utilByCsp === null ? (
                      <Skeleton className="h-4 w-10" />
                    ) : (
                      formatPct(utilByCsp[csp.id])
                    )}
                  </dd>
                </div>
                <div className="flex flex-col">
                  <dt className="text-muted-foreground">장애</dt>
                  <dd
                    className={`font-semibold tabular-nums ${csp.faulted > 0 ? 'text-metric-fault' : ''}`}
                  >
                    {formatCount(csp.faulted)}건
                  </dd>
                </div>
              </dl>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}
