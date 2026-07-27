// AlertTimeline — 알림 이벤트 세로 타임라인. 최신이 위로 오도록 정렬하고,
// 심각도는 점 색(critical=fault 빨강 / warning=thermal 주황 / info=cpu 파랑)으로
// 표현한다. 데모 시나리오의 XID·ECC·온도 알림 서사가 여기 쌓인다.
import { useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import type { AlertEvent } from '@/lib/demoApi';

export interface AlertTimelineProps {
  alerts: AlertEvent[];
  /** 주면 각 항목에 [확인] 버튼이 붙는다(ack-alert 호출부 담당). 안 주면 현행 그대로. */
  onAck?: (id: number) => void;
}

const SEVERITY_COLOR: Record<AlertEvent['severity'], string> = {
  critical: 'var(--metric-fault)',
  major: 'var(--metric-thermal)',
  warning: 'var(--metric-mem)',
  info: 'var(--metric-cpu)',
};

const SEVERITY_LABEL: Record<AlertEvent['severity'], string> = {
  critical: 'Critical',
  major: 'Major',
  warning: 'Warning',
  info: '정보',
};

export default function AlertTimeline({ alerts, onAck }: AlertTimelineProps) {
  // 확인 상태의 진실은 서버(alert.acked)다 — 로컬 배열은 클릭 직후 폴링이
  // 돌아오기 전까지의 즉시 피드백용이며, 둘의 합집합으로 표시한다.
  const [acked, setAcked] = useState<number[]>([]);
  const isAcked = (alert: AlertEvent) => alert.acked === true || acked.includes(alert.id);
  const sorted = [...alerts].sort((a, b) => b.at - a.at);

  if (sorted.length === 0) {
    return <p className="py-6 text-center text-muted-foreground text-sm">수신된 알림이 없다.</p>;
  }

  return (
    <ol className="max-h-96 overflow-auto pr-1">
      {sorted.map((alert, index) => (
        <li
          key={alert.id || `${alert.at}-${alert.code}-${index}`}
          className={`relative ml-1.5 border-border border-l pb-4 pl-4 last:pb-0 ${
            isAcked(alert) ? 'opacity-50' : ''
          }`}
        >
          <span
            aria-label={`심각도 ${SEVERITY_LABEL[alert.severity]}`}
            className="absolute top-1 -left-[5px] size-2.5 rounded-full ring-2 ring-card"
            style={{ backgroundColor: SEVERITY_COLOR[alert.severity] }}
          />
          <div className="flex flex-col gap-0.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-muted-foreground text-xs tabular-nums">
                {new Date(alert.at).toLocaleTimeString('ko-KR')}
              </span>
              <Badge variant="outline" className="font-mono text-[10px]">
                {alert.code}
              </Badge>
              {onAck ? (
                isAcked(alert) ? (
                  <span className="ml-auto text-muted-foreground text-[10px]">확인됨</span>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="ml-auto h-6 px-2 text-[10px]"
                    onClick={() => {
                      setAcked((prev) => [...prev, alert.id]);
                      onAck(alert.id);
                    }}
                  >
                    확인
                  </Button>
                )
              ) : null}
            </div>
            <p className="font-medium text-sm leading-snug">{alert.title}</p>
            {alert.instance || alert.device ? (
              <p className="font-mono text-muted-foreground text-xs">
                {[alert.instance, alert.device].filter(Boolean).join(' · ')}
              </p>
            ) : null}
            {alert.detail ? (
              <p className="text-muted-foreground text-xs leading-snug">{alert.detail}</p>
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}
