// EventFeed — demo1 우측 컬럼 "실시간 이벤트 피드" 카드. 심각도 셀렉터로 거른
// 최신 알림 N건을 심각도 색 · 대상(노드/GPU/모델) · 상대시각 · 진행 상태 배지로
// 보인다. AlertTimeline 이 "장애 서사 타임라인"이라면 이쪽은 "요약 피드"다.
import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { Badge } from '@/components/ui/badge';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { AlertEvent } from '@/lib/demoApi';

export interface EventFeedProps {
  alerts: AlertEvent[];
  /** uuid→model 매핑용 (대상에 모델명 표기). 없으면 생략. */
  modelOf?: (a: AlertEvent) => string | undefined;
  /** "전체 이벤트 보기" 목적지. 기본 /gpu/health */
  moreHref?: string;
  limit?: number;
}

/** 상대시각 문자열 — 이벤트 피드·노드 패널 공용. nowMs 를 주면 테스트에서 고정 가능. */
export function relTime(atMs: number, nowMs = Date.now()): string {
  const sec = Math.max(0, Math.round((nowMs - atMs) / 1000));
  if (sec < 60) return '방금 전';
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}분 전`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour}시간 전`;
  return `${Math.floor(hour / 24)}일 전`;
}

/** Radix SelectItem 은 value="" 금지 — "모든 심각도"의 센티널 값. */
const ALL = '__all__';

const SEVERITY: Record<AlertEvent['severity'], { label: string; color: string }> = {
  critical: { label: 'Critical', color: 'var(--metric-fault)' },
  major: { label: 'Major', color: 'var(--metric-thermal)' },
  warning: { label: 'Warning', color: 'var(--util-band-2)' },
  info: { label: 'Info', color: 'var(--metric-cpu)' },
};

const SEVERITY_ORDER: AlertEvent['severity'][] = ['critical', 'major', 'warning', 'info'];

/** 진행 상태 배지 — 빈 값/미지정이면 배지 없음. */
const STATUS: Record<string, { label: string; color: string }> = {
  'auto-repair': { label: '자동복구 실행 중', color: 'var(--metric-cpu)' },
  repaired: { label: '복구 성공', color: 'var(--metric-pod)' },
  resolved: { label: '해결', color: 'var(--metric-pod)' },
  'collect-lag': { label: '수집 지연', color: 'var(--metric-thermal)' },
};

export default function EventFeed({ alerts, modelOf, moreHref, limit = 6 }: EventFeedProps) {
  const [severity, setSeverity] = useState<string>(ALL);
  // 상대시각은 30초 tick 이면 충분하다(분 단위 표기라 초 단위 리렌더는 낭비).
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, []);

  const shown = [...alerts]
    .filter((a) => severity === ALL || a.severity === severity)
    .sort((a, b) => b.at - a.at)
    .slice(0, limit);

  return (
    <Card>
      <CardHeader>
        <CardTitle>실시간 이벤트 피드</CardTitle>
        <CardAction>
          <Select value={severity} onValueChange={setSeverity}>
            <SelectTrigger size="sm" className="w-[120px]" aria-label="심각도 필터">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>모든 심각도</SelectItem>
              {SEVERITY_ORDER.map((key) => (
                <SelectItem key={key} value={key}>
                  {SEVERITY[key].label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {shown.length === 0 ? (
          <p className="py-6 text-center text-muted-foreground text-sm">수신된 알림이 없다.</p>
        ) : (
          <ul className="flex max-h-[520px] flex-col gap-2 overflow-auto pr-1">
            {shown.map((alert, index) => {
              const sev = SEVERITY[alert.severity];
              const status = alert.status ? STATUS[alert.status] : undefined;
              const target = [alert.instance, alert.device].filter(Boolean).join(' / ');
              const model = modelOf?.(alert);
              return (
                <li
                  key={alert.id || `${alert.at}-${alert.code}-${index}`}
                  className="rounded-md border border-border border-l-2 px-3 py-2"
                  style={{
                    borderLeftColor: sev.color,
                    backgroundColor: `color-mix(in oklab, ${sev.color} 6%, var(--card))`,
                  }}
                >
                  <div className="flex items-start gap-2">
                    <span
                      aria-hidden
                      className="mt-1.5 size-2 shrink-0 rounded-full"
                      style={{ backgroundColor: sev.color }}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline gap-2">
                        <span className="font-medium text-xs" style={{ color: sev.color }}>
                          {sev.label}
                        </span>
                        <span className="ml-auto shrink-0 text-[11px] text-muted-foreground tabular-nums">
                          {relTime(alert.at, now)}
                        </span>
                      </div>
                      <p className="truncate font-medium text-sm leading-snug">{alert.title}</p>
                      {target || model ? (
                        <p className="truncate font-mono text-[11px] text-muted-foreground">
                          {target}
                          {model ? ` (${model})` : ''}
                        </p>
                      ) : null}
                      {status ? (
                        <Badge
                          variant="outline"
                          className="mt-1 text-[10px]"
                          style={{
                            color: status.color,
                            borderColor: `color-mix(in oklab, ${status.color} 40%, transparent)`,
                            backgroundColor: `color-mix(in oklab, ${status.color} 12%, transparent)`,
                          }}
                        >
                          {status.label}
                        </Badge>
                      ) : null}
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        <Link
          to={moreHref ?? '/gpu/health'}
          className="rounded-md border border-border py-2 text-center text-muted-foreground text-xs hover:bg-accent hover:text-foreground"
        >
          전체 이벤트 보기 →
        </Link>
      </CardContent>
    </Card>
  );
}
