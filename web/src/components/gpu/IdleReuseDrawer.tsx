// IdleReuseDrawer — 유휴장비 관리(/gpu/idle) 행 클릭 시 우측 재사용 드로어.
//
// 표는 "무엇이 유휴인가"만 말한다. 이 드로어가 "그래서 이 한 장을 어떻게
// 되돌리는가"를 말한다 — 자산 신원(모델·UUID·위치·라벨) + 재사용 4단계 진행
// 상태 + 지금 누를 수 있는 단 하나의 버튼.
//
// 검증 상태는 /demo/state 폴링에 실려 오므로 여기서 따로 질의하지 않는다.
// 자산 대장(라벨·시리얼·보증)만 노드 수명 동안 불변이라 instance 당 1회 조회
// 한다(NodeDrillSheet 와 같은 규약 — 폴링 금지).
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router';
import { ExternalLink, Info } from 'lucide-react';
import type { GateStageStatus } from '@/components/gpu/GateDiagram';
import ReuseStepper, { type ReuseStepperStep } from '@/components/gpu/ReuseStepper';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Separator } from '@/components/ui/separator';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import type { IdleGPU, IdleValidation } from '@/lib/demoApi';
import { fetchNodeAsset, type NodeAsset, nodeAssetMessage, parseLabel } from '@/lib/nodeAsset';

// ---- 유휴 재사용 어휘 (표·드로어 공통 SSOT) ----

/** 행 상태 배지 — 서버 verdict 4종의 한국어 라벨. */
export const VERDICT_LABEL: Record<IdleValidation['verdict'], string> = {
  idle: '유휴',
  validating: '검증 진행 중',
  ready: '재투입 가능',
  failed: '검증 실패',
};

/** 상태 색은 라벨 문자열이 아니라 이 표가 정한다(관제 규약 — 라벨을 바꿔도 색 불변). */
export const VERDICT_COLOR: Record<IdleValidation['verdict'], string> = {
  idle: 'var(--muted-foreground)',
  validating: 'var(--metric-cpu)',
  ready: 'var(--metric-pod)',
  failed: 'var(--metric-fault)',
};

/** 사전검증·번인 하위 상태 4종 — 라벨·색·스테퍼 상태를 한 곳에서 낸다. */
export const STEP_META: Record<
  IdleValidation['precheck'],
  { label: string; color: string; stage: GateStageStatus }
> = {
  // 목업 demo4 는 미실행을 붉은 톤으로 낸다(표 배지 + 드로어 스테퍼 힌트 모두).
  none: { label: '미실행', color: 'var(--metric-fault)', stage: 'idle' },
  running: { label: '진행 중', color: 'var(--metric-cpu)', stage: 'active' },
  pass: { label: '통과', color: 'var(--metric-pod)', stage: 'done' },
  fail: { label: '실패', color: 'var(--metric-fault)', stage: 'blocked' },
};

/** 토큰색 배지 — 목업 demo4 는 테두리 없는 옅은 tint pill 이다. */
export function ToneBadge({ text, color }: { text: string; color: string }) {
  return (
    <Badge
      variant="outline"
      className="border-transparent px-2 py-0.5 text-[11px]"
      style={{
        color,
        backgroundColor: `color-mix(in srgb, ${color} 14%, transparent)`,
      }}
    >
      {text}
    </Badge>
  );
}

/** 번인은 진행률을 라벨에 얹는다 — "진행 중 25%"(목업 표기). */
export function burninLabel(v: IdleValidation): string {
  if (v.burnin !== 'running') return STEP_META[v.burnin].label;
  return `진행 중 ${Math.round(v.burninProgress ?? 0)}%`;
}

/** "184일 3시간 22분" — 일 단위만으로는 못 만드는 표기라 분 해상도를 쓴다.
 *  표는 분을 떼고(공간), 드로어는 분까지 낸다. */
export function formatIdleSpan(minutes: number, withMinutes: boolean): string {
  const total = Math.max(0, Math.floor(minutes));
  const days = Math.floor(total / 1440);
  const hours = Math.floor((total % 1440) / 60);
  const parts = [`${days}일`, `${hours}시간`];
  if (withMinutes) parts.push(`${total % 60}분`);
  return parts.join(' ');
}

// ---- 재사용 4단계 스테퍼 ----

/** 사전검증 → 번인 테스트 → 가용성 판정 → 재투입. 앞 두 칸은 하위 상태에서,
 *  뒤 두 칸은 최종 판정(verdict)에서 나온다. 사전검증 화면(/gpu/validation)의
 *  같은 4단계와 같은 컴포넌트를 쓴다 — 두 화면이 다른 모양이면 같은 절차로
 *  읽히지 않는다. 이쪽은 서버 상세를 부르지 않으므로 스냅샷에서 파생한다. */
function reuseStages(v: IdleValidation): ReuseStepperStep[] {
  const pre = STEP_META[v.precheck];
  const burn = STEP_META[v.burnin];
  const judged: ReuseStepperStep =
    v.verdict === 'ready'
      ? { id: 'judge', label: '가용성 판정', status: 'done', hint: '통과' }
      : v.verdict === 'failed'
        ? { id: 'judge', label: '가용성 판정', status: 'blocked', hint: '실패' }
        : { id: 'judge', label: '가용성 판정', status: 'idle', hint: '대기' };
  const back: ReuseStepperStep =
    v.verdict === 'ready'
      ? { id: 'return', label: '재투입', status: 'active', hint: '가능' }
      : { id: 'return', label: '재투입', status: 'idle', hint: '대기' };
  return [
    { id: 'precheck', label: '사전검증', status: pre.stage, hint: pre.label },
    { id: 'burnin', label: '번인 테스트', status: burn.stage, hint: burninLabel(v) },
    judged,
    back,
  ];
}

// ---- 드로어 ----

export interface IdleReuseDrawerProps {
  /** null = 닫힘. */
  gpu: IdleGPU | null;
  /** "클라우드 A > KR 리전 1 > alpha-2-prod" — 표시명 조회는 페이지가 한다. */
  location: string;
  /** 이 대상의 액션 진행 중 — 버튼 중복 클릭 차단. */
  acting: boolean;
  /** 직전 액션 실패 문구(null = 없음). 드로어 CTA 도 페이지와 같은 onAct 를
   *  타므로 실패가 여기서도 보여야 한다 — 페이지 상단 경고는 시트에 가린다. */
  error: string | null;
  onClose: () => void;
  onAct: (action: 'start-idle-validation' | 'return-idle-to-service', uuid: string) => void;
}

/** 정의행(라벨/값 2열) — NodeDrillSheet 의 InfoRow 와 같은 형태. */
function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-sm' : 'break-all text-sm'}>{value || '—'}</dd>
    </>
  );
}

export default function IdleReuseDrawer({
  gpu,
  location,
  acting,
  error,
  onClose,
  onAct,
}: IdleReuseDrawerProps) {
  const instance = gpu?.instance ?? null;
  const [asset, setAsset] = useState<NodeAsset | null>(null);
  const [assetError, setAssetError] = useState<string | null>(null);

  // 자산은 노드 수명 동안 불변 — instance 가 바뀔 때 1회만 부른다(폴링 금지).
  useEffect(() => {
    setAsset(null);
    setAssetError(null);
    if (!instance) return;
    let cancelled = false;
    fetchNodeAsset(instance)
      .then((next) => {
        if (!cancelled) setAsset(next);
      })
      .catch((err: unknown) => {
        // 401 은 AuthContext 가 로그인 화면 전환으로 처리한다(문구 null).
        if (!cancelled) setAssetError(nodeAssetMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [instance]);

  const nodeAsset = asset && asset.instance === instance ? asset : null;

  // 닫히는 동안(exit 애니메이션)에도 마지막 대상을 붙든다 — 재투입 직후처럼
  // 대상이 목록에서 사라지는 경우 패널이 빈 채로 미끄러져 나간다.
  const last = useRef<IdleGPU | null>(null);
  if (gpu !== null) last.current = gpu;
  const shown = gpu ?? last.current;
  const v = shown?.validation;

  return (
    <Sheet
      open={gpu !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent side="right" className="w-full sm:max-w-xl">
        {shown === null || v === undefined ? null : (
          <>
            <SheetHeader className="gap-2 pb-2">
              <SheetTitle className="break-all pr-8 font-mono text-base">
                {shown.instance} / {shown.device}
              </SheetTitle>
              <div className="flex flex-wrap items-center gap-1.5">
                <ToneBadge
                  text={VERDICT_LABEL[v.verdict]}
                  color={VERDICT_COLOR[v.verdict]}
                />
                <Badge variant="outline" className="text-[10px] text-muted-foreground">
                  {shown.csp.toUpperCase()}
                </Badge>
              </div>
              <SheetDescription>{shown.model}</SheetDescription>
            </SheetHeader>

            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-6">
              {/* 1. 자산 상세 */}
              <dl className="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-2">
                <InfoRow label="GPU 모델" value={shown.model} />
                <InfoRow label="유휴 기간" value={formatIdleSpan(shown.idleMinutes, true)} />
                <InfoRow label="GPU UUID" value={shown.uuid} mono />
                <InfoRow label="위치" value={location} />
                <dt className="text-muted-foreground text-sm">라벨</dt>
                <dd className="flex flex-wrap gap-1">
                  {nodeAsset === null ? (
                    assetError !== null ? (
                      <span className="text-muted-foreground text-sm">{assetError}</span>
                    ) : (
                      <Skeleton className="h-5 w-40" />
                    )
                  ) : nodeAsset.labels.length === 0 ? (
                    <span className="text-sm">—</span>
                  ) : (
                    nodeAsset.labels.map((raw) => {
                      const { key, value } = parseLabel(raw);
                      return (
                        <Badge key={raw} variant="secondary" className="font-mono text-[10px]">
                          {value ? `${key}: ${value}` : key}
                        </Badge>
                      );
                    })
                  )}
                </dd>
              </dl>

              <Separator />

              {/* 2. 재사용 준비 상태 — 4단계 스테퍼 */}
              <section className="flex flex-col gap-2">
                <h3 className="font-medium text-sm">재사용 준비 상태</h3>
                <ReuseStepper steps={reuseStages(v)} />
              </section>

              {/* 3. 조치 — verdict 가 지금 누를 수 있는 버튼 하나를 정한다 */}
              <div className="flex flex-col gap-2">
                {error === null ? null : (
                  <Alert variant="destructive">
                    <AlertDescription>{error}</AlertDescription>
                  </Alert>
                )}
                {/* 진행 중에는 누를 액션이 없다 — 그렇다고 진행률을 비활성 버튼
                    모양으로 두면 시연에서 눌러도 아무 일이 없다. 지금 할 수 있는
                    단 하나(라인에서 도는 것을 보러 간다)로 잇는다: 표의 validating
                    행 권장 작업과 같은 목적지다. */}
                {v.verdict === 'validating' ? (
                  <Button asChild>
                    <Link to={`/gpu/validation?uuid=${encodeURIComponent(shown.uuid)}`}>
                      검증 진행 보기 {Math.round(v.burninProgress ?? 0)}%
                    </Link>
                  </Button>
                ) : v.verdict === 'ready' ? (
                  <Button
                    disabled={acting}
                    onClick={() => onAct('return-idle-to-service', shown.uuid)}
                  >
                    운영 풀 재투입
                  </Button>
                ) : (
                  <Button
                    disabled={acting}
                    onClick={() => onAct('start-idle-validation', shown.uuid)}
                  >
                    {v.verdict === 'failed' ? '재검증 시작' : '재사용 검증 시작'}
                  </Button>
                )}

                <Collapsible>
                  <CollapsibleTrigger asChild>
                    <Button variant="outline" className="w-full">
                      상세 정보 보기
                    </Button>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    {nodeAsset === null ? (
                      assetError !== null ? (
                        <p className="pt-3 text-muted-foreground text-sm">{assetError}</p>
                      ) : (
                        <Skeleton className="mt-3 h-40 w-full" />
                      )
                    ) : (
                      <dl className="grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-2 pt-3">
                        <InfoRow
                          label="드라이버"
                          value={nodeAsset.gpus.find((g) => g.uuid === shown.uuid)?.driverVersion ?? ''}
                        />
                        <InfoRow
                          label="CUDA"
                          value={nodeAsset.gpus.find((g) => g.uuid === shown.uuid)?.cudaVersion ?? ''}
                        />
                        <InfoRow
                          label="GPU 시리얼"
                          value={nodeAsset.gpus.find((g) => g.uuid === shown.uuid)?.serial ?? ''}
                          mono
                        />
                        <InfoRow label="노드 시리얼" value={nodeAsset.serialNumber} mono />
                        <InfoRow label="설치일" value={nodeAsset.installedAt} />
                        <InfoRow label="보증 만료" value={nodeAsset.warrantyEnds} />
                        <InfoRow
                          label="랙 / PDU"
                          value={`${nodeAsset.rack} / PDU ${nodeAsset.pdu}`}
                        />
                      </dl>
                    )}
                  </CollapsibleContent>
                </Collapsible>
              </div>

              {/* 4. 안내 */}
              <Alert>
                <Info />
                <AlertTitle>재사용 검증이란?</AlertTitle>
                <AlertDescription>
                  <p>
                    유휴 GPU를 업무에 재투입하기 위해 사전검증, 번인 테스트, 가용성 판정을 통해 정상
                    동작 여부를 확인하는 절차입니다.
                  </p>
                  <Link
                    to={`/gpu/validation?uuid=${encodeURIComponent(shown.uuid)}`}
                    className="inline-flex items-center gap-1 text-primary underline-offset-2 hover:underline"
                  >
                    자세히 보기
                    <ExternalLink className="size-3" />
                  </Link>
                </AlertDescription>
              </Alert>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
