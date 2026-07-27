// GpuIncident — 장애 분석. 장애 개체에서 시작해 클라우드 경계까지 한 축으로
// 편다(사용자 지시 2026-07-27):
//
//   장애 → GPU UUID / MIG → 물리 서버 → Kubernetes Node → Node Pool
//        → Cluster → CSP / Region / AZ
//
// 위에서 아래로 훑으면 "어디를 봐야 하나"로 끝나지만, 장애에서 위로 펴면 각
// 계층에서 분모가 커지는 게 보인다 — 그게 blast radius 다. 분자(영향 GPU)는
// 계층이 올라가도 고정이라, 화면이 영향 범위를 과장하지 않는다.
import { AlertTriangle, ArrowDown, CheckCircle2 } from 'lucide-react';
import AlertTimeline from '@/components/gpu/AlertTimeline';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemo } from '@/hooks/demoContext';
import type { ChainLink } from '@/lib/demoApi';
import { formatCount, formatPct } from '@/lib/gpuMetrics';

const TONE_BORDER: Record<string, string> = {
  crit: 'border-l-metric-fault',
  major: 'border-l-metric-thermal',
  warn: 'border-l-metric-thermal/60',
  info: 'border-l-metric-cpu',
  muted: 'border-l-muted-foreground/40',
  ok: 'border-l-metric-pod',
};

const TONE_TEXT: Record<string, string> = {
  crit: 'text-metric-fault',
  major: 'text-metric-thermal',
  warn: 'text-metric-thermal',
  info: 'text-metric-cpu',
  muted: 'text-muted-foreground',
  ok: 'text-metric-pod',
};

function ChainRow({ link, last }: { link: ChainLink; last: boolean }) {
  const share = link.total > 0 ? (link.scope / link.total) * 100 : 0;
  return (
    <li className="flex flex-col items-stretch">
      <div className={`flex items-center gap-3 border-l-4 bg-card p-3 ${TONE_BORDER[link.tone] ?? ''}`}>
        <div className="min-w-[8.5rem] shrink-0">
          <p className="text-[10px] uppercase tracking-wide text-muted-foreground">{link.level}</p>
          <p className={`truncate font-semibold text-sm ${TONE_TEXT[link.tone] ?? ''}`} title={link.value}>
            {link.value}
          </p>
        </div>
        <p className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={link.detail}>
          {link.detail}
        </p>
        <div className="shrink-0 text-right">
          <p className="text-xs font-semibold tabular-nums">
            {formatCount(link.scope)} / {formatCount(link.total)}
          </p>
          <p className="text-[10px] text-muted-foreground">
            이 계층의 {formatPct(share)}
          </p>
        </div>
        {/* 영향 비율 막대 — 위로 갈수록 짧아진다(분모가 커지므로). */}
        <div className="hidden h-1.5 w-24 shrink-0 overflow-hidden rounded-full bg-muted sm:block">
          <div
            className="h-full rounded-full bg-metric-fault"
            style={{ width: `${Math.max(share, 0.6)}%` }}
          />
        </div>
      </div>
      {last ? null : (
        <div className="flex justify-start pl-[1.15rem]">
          <ArrowDown className="size-3.5 text-muted-foreground/50" />
        </div>
      )}
    </li>
  );
}

export default function GpuIncident() {
  const { demoMode, state } = useDemo();

  if (demoMode === null || (demoMode === true && !state)) {
    return (
      <div className="flex flex-col gap-3" aria-busy="true">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    );
  }

  if (demoMode !== true || !state) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>장애 분석</CardTitle>
          <CardDescription>
            장애 체인은 데모 모드의 시나리오 데이터로 구성된다 — 실서비스 인스턴스에서는
            표시할 진행 중 장애가 없다.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const { incident, alerts } = state;

  return (
    <div className="flex flex-col gap-3">
      <Card className={incident.active ? 'gap-0 border-metric-fault/40 py-0' : 'gap-0 py-0'}>
        <CardContent className="flex flex-wrap items-center gap-3 p-4">
          {incident.active ? (
            <AlertTriangle className="size-5 shrink-0 text-metric-fault" />
          ) : (
            <CheckCircle2 className="size-5 shrink-0 text-metric-pod" />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <p className="font-semibold">{incident.title}</p>
              <Badge variant={incident.active ? 'destructive' : 'secondary'} className="text-[10px]">
                {incident.code}
              </Badge>
              <Badge variant="outline" className="text-[10px]">
                {incident.severity}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              {incident.active
                ? '장애 개체에서 클라우드 경계까지 영향 범위를 펼친다 — 위로 갈수록 분모가 커지고 분자(영향 GPU)는 고정이다.'
                : '진행 중인 장애가 없다. 아래 체인은 마지막 대상 개체의 위치를 보여준다.'}
            </p>
          </div>
          {incident.at > 0 ? (
            <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
              {new Date(incident.at).toLocaleTimeString('ko-KR')}
            </span>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">영향 체인</CardTitle>
          <CardDescription className="text-xs">
            GPU → 물리 서버 → Kubernetes Node → Node Pool → Cluster → CSP / Region / AZ
          </CardDescription>
        </CardHeader>
        <CardContent>
          {incident.links.length === 0 ? (
            <p className="text-sm text-muted-foreground">체인을 구성할 대상이 없다.</p>
          ) : (
            <ul className="flex flex-col gap-0 overflow-hidden rounded-md border">
              {incident.links.map((link, i) => (
                <ChainRow
                  key={link.level}
                  link={link}
                  last={i === incident.links.length - 1}
                />
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">관련 알림</CardTitle>
          <CardDescription className="text-xs">
            체인의 근거가 된 신호. 확인 처리는 헬스 화면과 같은 서버 상태를 공유한다.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AlertTimeline alerts={alerts.slice(0, 8)} />
        </CardContent>
      </Card>
    </div>
  );
}
