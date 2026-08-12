// EventStream — 클러스터(K8s) 이벤트 스트림. AlertTimeline 이 "조치 대상"을
// 쌓는다면 이쪽은 계속 돌아가는 클러스터의 일상 기록이다 — 화면이 살아 있고
// 실제 워크로드가 스케줄·기동·회수되고 있음을 보여주는 배경 소음.
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import type { ClusterEvent } from '@/lib/demoApi';

export interface EventStreamProps {
  events: ClusterEvent[];
  /** 표시 상한(기본 20) — 서버는 최대 60건을 보낸다. */
  max?: number;
}

function formatClock(atMs: number): string {
  return new Date(atMs).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

export default function EventStream({ events, max = 20 }: EventStreamProps) {
  const rows = [...events].sort((a, b) => b.at - a.at).slice(0, max);

  return (
    <Card>
      <CardHeader>
        <CardTitle>클러스터 이벤트</CardTitle>
        <CardDescription>알림과 달리 조치 대상이 아닌 일상 기록</CardDescription>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="py-6 text-center text-muted-foreground text-sm">이벤트 없음</p>
        ) : (
          <ol className="flex max-h-80 flex-col gap-2 overflow-auto pr-1">
            {rows.map((e, index) => {
              const warning = e.type === 'Warning';
              return (
                <li
                  key={`${e.at}-${e.reason}-${e.object}-${index}`}
                  className="relative flex flex-col gap-0.5 pl-4"
                >
                  {warning ? (
                    <span
                      aria-label="경고"
                      className="absolute top-1.5 left-0 size-2 rounded-full bg-metric-thermal"
                    />
                  ) : null}
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-muted-foreground text-xs tabular-nums">
                      {formatClock(e.at)}
                    </span>
                    <Badge
                      variant="outline"
                      className={warning ? 'text-metric-thermal text-[10px]' : 'text-[10px]'}
                    >
                      {warning ? 'Warning' : 'Normal'}
                    </Badge>
                    <span className="font-semibold text-xs">{e.reason}</span>
                    <span className="font-mono text-muted-foreground text-[11px]">{e.object}</span>
                  </div>
                  <p className="text-muted-foreground text-xs leading-snug">{e.message}</p>
                  {e.namespace || e.node ? (
                    <p className="font-mono text-[10px] text-muted-foreground/80">
                      {[e.namespace ? `ns/${e.namespace}` : '', e.node ? `node/${e.node}` : '']
                        .filter(Boolean)
                        .join(' · ')}
                    </p>
                  ) : null}
                </li>
              );
            })}
          </ol>
        )}
      </CardContent>
    </Card>
  );
}
