// GpuRuleset — 장애 판정 룰셋 관리(/gpu/ruleset). 시나리오 1 [사전 설정],
// 시연 첫 화면.
//
// 룰 엔진을 새로 만들지 않는다. 이 화면은 *이미 도는 엔진 로직 2종의 뷰* 다:
//   1단계 결정론 감지 = Scenario.pushAlert 로 흐르는 알림 코드들
//   2단계 위험도 스코어 = health.go Score() 의 가중 감점표
// 두 기구를 tier 로 나눈 표 2개로 노출하고, 실동작 레버(적용 토글) 하나만
// 더한다 — 1단계를 끄면 그 코드의 알림이 이후로 끊기고(이미 등재된 이력은
// 감사 계약상 남는다), 2단계를 끄면 Health 점수가 오른다.
//
// "단계적 고도화"는 3중으로 드러낸다: ① 상단 2단 스테퍼(채택 비율 포함)
// ② 표를 tier 로 2분할 ③ 두 표의 *열 구성 자체*를 다르게 — 1단계는 심각도 +
// 불리언 조건(터진 것), 2단계는 가중치 + 위험 대역(터지기 전).
import { ListChecks, ShieldCheck, ShieldOff, TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import KpiTile from '@/components/gpu/KpiTile';
import ReuseStepper, { type ReuseStepperStep } from '@/components/gpu/ReuseStepper';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useDemo } from '@/hooks/demoContext';
import type { RuleDef, RuleTier } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

// 심각도 색은 전 화면 공통 어휘를 그대로 쓴다(critical→fault / major→thermal /
// warning→mem). 라벨 문자열로 색을 추론하면 라벨을 바꾸는 순간 색이 깨진다.
const SEVERITY_COLOR: Record<RuleDef['severity'], string> = {
  critical: 'var(--metric-fault)',
  major: 'var(--metric-thermal)',
  warning: 'var(--metric-mem)',
};

const SEVERITY_LABEL: Record<RuleDef['severity'], string> = {
  critical: 'Critical',
  major: 'Major',
  warning: 'Warning',
};

/** 단계 색 — 1단계는 확정 판정(초록), 2단계는 예측 판정(파랑). 미적용은 무채색. */
const TIER_COLOR: Record<number, string> = {
  1: 'var(--metric-pod)',
  2: 'var(--metric-cpu)',
};

function TonedBadge({ text, color }: { text: string; color: string }) {
  return (
    <Badge
      variant="outline"
      className="text-[10px]"
      style={{
        color,
        borderColor: color,
        backgroundColor: `color-mix(in srgb, ${color} 12%, transparent)`,
      }}
    >
      {text}
    </Badge>
  );
}

/** 고도화 스테퍼 — 형제 화면(사전검증 /gpu/validation)과 *같은 번호 스테퍼*
 *  컴포넌트를 쓴다. 이 화면만 박스+화살표를 쓰면 같은 제품으로 안 보인다.
 *  상태는 done/idle 2종만 쓴다 — active(검정 원)는 형제 화면에서 "진행 중"
 *  이라 9/9 적용된 단계에 붙이면 미완으로 읽힌다. */
function stepperSteps(tiers: RuleTier[]): ReuseStepperStep[] {
  return tiers.map((tier) => ({
    id: String(tier.tier),
    // 라벨은 "1단계 · 명확한 장애 감지" — 번호는 원에 이미 있으므로 뒷부분만.
    label: tier.label.split(' · ').at(-1) ?? tier.label,
    status: tier.enabled === 0 ? 'idle' : 'done',
    hint: tier.enabled > 0 ? `${tier.enabled}/${tier.total} 적용` : '전체 미적용',
  }));
}

/** 적용 토글 — 낙관적 갱신은 하지 않는다(GpuRemediation 모드 셀렉터와 동일
 *  계약). 실패하면 다음 폴링의 서버 값으로 상태가 그대로 되돌아온다. */
function ToggleCell({
  rule,
  pending,
  onToggle,
}: {
  rule: RuleDef;
  pending: string | null;
  onToggle: (rule: RuleDef) => void;
}) {
  const busy = pending === rule.code;
  return (
    // 적용 중인 룰은 outline(되돌릴 수 있음), 해제된 룰은 primary(복구를 유도).
    <Button
      variant={rule.enabled ? 'outline' : 'default'}
      size="sm"
      className="w-16"
      disabled={pending !== null}
      onClick={() => onToggle(rule)}
    >
      {busy ? '…' : rule.enabled ? '해제' : '적용'}
    </Button>
  );
}

function TierTable({
  tier,
  pending,
  onToggle,
}: {
  tier: RuleTier;
  pending: string | null;
  onToggle: (rule: RuleDef) => void;
}) {
  const tier2 = tier.tier === 2;
  const color = TIER_COLOR[tier.tier];
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          {tier.label}
          <TonedBadge text={`${tier.enabled}/${tier.total} 적용`} color={color} />
        </CardTitle>
        <CardDescription>{tier.describe}</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            {/* 목업 표는 헤더가 옅은 회색 밴드다(demo4) — 흰 바탕 밑줄만이면
                열 이름이 본문과 같은 무게로 읽힌다. */}
            <TableRow className="bg-muted/50 hover:bg-muted/50">
              {/* 폭 고정은 값 열만 — 서술 2열은 남은 폭을 나눠 갖는다. 이 표의
                  min-content 는 897(1단계)/918(2단계)px 로 1440 뷰포트의 본문
                  폭(1184)보다 작다(실측) → 우측 [적용] 열까지 가로 스크롤 없이
                  들어온다. 폭 지정을 더 넣으면 오히려 열이 부풀어 역효과다.
                  (시연 폭에서 [적용] 이 화면 밖으로 밀리던 원인은 이 표가 아니라
                   헤더 한 줄의 min-content 1714px 였다 — Shell.tsx 참조. Table
                   컨테이너는 overflow-x-auto 라 본문 최소폭에 기여하지 않는다.) */}
              <TableHead className="w-52">룰</TableHead>
              <TableHead className="w-24">심각도</TableHead>
              {tier2 ? <TableHead className="w-16 text-right">가중치</TableHead> : null}
              <TableHead>{tier2 ? '위험 조건' : '감지 조건'}</TableHead>
              <TableHead>연결된 자동 조치</TableHead>
              <TableHead className="w-24 text-right">조건 매칭</TableHead>
              <TableHead className="w-20 text-right">적용</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tier.rules.map((rule) => (
              <TableRow key={rule.code} className={rule.enabled ? undefined : 'opacity-50'}>
                <TableCell>
                  <span className="flex flex-col">
                    <span className="font-medium">{rule.name}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">{rule.code}</span>
                  </span>
                </TableCell>
                <TableCell>
                  <TonedBadge
                    text={SEVERITY_LABEL[rule.severity]}
                    color={SEVERITY_COLOR[rule.severity]}
                  />
                </TableCell>
                {tier2 ? (
                  <TableCell
                    className="text-right font-semibold tabular-nums"
                    style={{ color: 'var(--metric-fault)' }}
                  >
                    −{rule.weight ?? 0}
                  </TableCell>
                ) : null}
                <TableCell className="text-muted-foreground text-sm">{rule.condition}</TableCell>
                <TableCell className="text-muted-foreground text-sm">{rule.action}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {rule.matchCount > 0 ? (
                    formatCount(rule.matchCount)
                  ) : (
                    <span className="text-muted-foreground">0</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  <ToggleCell rule={rule} pending={pending} onToggle={onToggle} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {/* 각주는 목업처럼 * 로 시작하는 한 줄 — 세 줄짜리 회색 문단은 표보다
            무겁게 읽혀 시연 화면의 무게중심을 뺏는다. */}
        <p className="mt-3 text-muted-foreground text-[11px]">
          {tier2
            ? '* 조건 매칭 = 이 조건에 걸린 GPU 수 (한 장이 여러 조건에 걸릴 수 있다). 적용을 해제하면 Health 판정이 실제로 바뀝니다.'
            : '* 적용을 해제하면 해당 코드의 알림이 이후로 발행되지 않습니다 — 이미 등재된 알림은 감사 이력으로 남습니다.'}
        </p>
      </CardContent>
    </Card>
  );
}

function LoadingShell() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}

export default function GpuRuleset() {
  const { demoMode, state, act } = useDemo();
  const [pending, setPending] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  if (demoMode === null) return <LoadingShell />;

  if (!demoMode) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>GPU 장애 룰셋</CardTitle>
          <CardDescription>데모 시나리오 전용 화면</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-sm">
            이 화면은 데모 모드에서만 동작한다. 실서비스 모드에서는 제공되지 않는다.
          </p>
        </CardContent>
      </Card>
    );
  }

  if (!state) return <LoadingShell />;

  const tiers = state.ruleset.tiers;
  const tier1 = tiers.find((t) => t.tier === 1);
  const tier2 = tiers.find((t) => t.tier === 2);
  const enabled = tiers.reduce((n, t) => n + t.enabled, 0);
  const total = tiers.reduce((n, t) => n + t.total, 0);
  // 조건 매칭은 *조건에 걸린 GPU 수* 다 — 적용 여부로 거르지 않는다. 거르면
  // 룰을 끄는 순간 KPI 가 "조건에 걸린 GPU 0장"이라고 말해(THERMAL 해제 시
  // 743 → 13) 시연이 전달할 메시지("조건엔 걸리지만 적용을 껐다")와 반대가 된다.
  // 미적용 룰의 존재는 옆의 '미적용 룰' KPI 와 표의 흐린 행이 이미 말한다.
  const matchOf = (t: RuleTier | undefined) =>
    t ? t.rules.reduce((n, r) => n + r.matchCount, 0) : 0;
  // 복구 모드는 읽기 전용 표시다 — 변경 진입점은 자동복구 화면 하나뿐이다.
  const modeLabel = state.scenario.modeOptions.find((o) => o.id === state.scenario.mode)?.label;

  const onToggle: (rule: RuleDef) => void = async (rule) => {
    if (pending !== null) return;
    setPending(rule.code);
    setActionError(null);
    const outcome = await act('set-rule-enabled', {
      code: rule.code,
      enabled: rule.enabled ? 'off' : 'on',
    });
    setPending(null);
    if (outcome.ok) return;
    setActionError(
      outcome.status === 409
        ? `룰 변경 거부 — ${outcome.error}`
        : `룰 변경 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
    );
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        {/* 제목(h1)은 셸이 좌측 메뉴 라벨로 이미 찍는다(Shell.tsx) — 라벨을 화면
            제목과 같은 문자열로 통일했으므로 여기서 또 찍으면 같은 제목이 두 줄로
            겹친다(목업은 브레드크럼 + 제목 1개). 배지·설명문만 남긴다. */}
        {/* 목업 헤더 리듬: 제목 아래 회색 한 줄. 배지는 그 줄 오른쪽에 붙인다
            (사전검증 화면의 제목 옆 '유휴 319일' 배지와 같은 위치 어휘). */}
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-muted-foreground text-sm">
            명확한 장애 감지부터 위험도 기반 예방 관리까지 단계적으로 고도화하는 구조입니다.
          </p>
          {modeLabel ? <Badge variant="outline">복구 모드 · {modeLabel}</Badge> : null}
        </div>
      </div>

      {actionError ? (
        <Alert variant="destructive">
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      {/* 1. 고도화 단계 스테퍼 — 사전검증 화면의 스테퍼 카드와 같은 구성
          (헤더 없이 스테퍼만 + 아래 한 줄 캡션). 카드 제목을 다시 얹으면
          같은 정보가 3중(셸 제목 / 카드 제목 / 단계 라벨)이 된다. */}
      <Card>
        <CardContent className="flex flex-col gap-2 py-2">
          {/* 단계가 2개뿐이라 폭을 안 묶으면 원 2개가 1150px 양 끝으로 벌어져
              연결선만 남는다 — 목업 스테퍼의 단계 간격(≈350px)에 맞춘다. */}
          <div className="mx-auto w-full max-w-lg">
            <ReuseStepper steps={stepperSteps(tiers)} />
          </div>
          <p className="text-center text-muted-foreground text-xs">
            1단계는 이미 터진 장애를 본다 — 2단계는 터지기 전을 본다 · 아래 표의 적용 토글이 이 값을
            바꾼다
          </p>
        </CardContent>
      </Card>

      {/* 2. KPI 4종 */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <KpiTile
          label="적용 룰"
          value={formatCount(enabled)}
          sub={`/ ${total}`}
          hint={
            tier1 && tier2
              ? `1단계 ${tier1.enabled}/${tier1.total} · 2단계 ${tier2.enabled}/${tier2.total}`
              : undefined
          }
          tone="ok"
          icon={<ListChecks />}
        />
        <KpiTile
          label="1단계 조건 매칭"
          value={formatCount(matchOf(tier1))}
          hint="결정론 조건에 걸린 GPU (조건별 합 — 중복 포함)"
          tone="danger"
          icon={<TriangleAlert />}
        />
        <KpiTile
          label="2단계 조건 매칭"
          value={formatCount(matchOf(tier2))}
          hint="위험도 조건에 걸린 GPU (조건별 합 — 중복 포함)"
          tone="warn"
          icon={<ShieldCheck />}
        />
        <KpiTile
          label="미적용 룰"
          value={formatCount(total - enabled)}
          // 복원 경로는 표의 [적용] 버튼 하나뿐이다 — 여기 있지도 않은 ↺ 를
          // 가리키면 시연자가 없는 컨트롤을 찾는다(실측: ↺ 는 전 화면에 없음).
          hint={total - enabled > 0 ? '판정에서 제외 중 — 표의 [적용] 로 복원' : '전 룰 적용 중'}
          // 4번째만 tone 이 없으면 상단 컬러 라인이 빠져 KPI 줄이 들쭉날쭉해진다
          // (목업 KPI 줄은 타일 6개가 전부 같은 구조). 미적용 0 = 정상이라 ok.
          tone={total - enabled > 0 ? 'danger' : 'ok'}
          icon={<ShieldOff />}
        />
      </div>

      {/* 3. 단계별 룰 표 — 열 구성이 다르다(1단계=불리언 조건 / 2단계=가중치·대역) */}
      {tier1 ? <TierTable tier={tier1} pending={pending} onToggle={onToggle} /> : null}
      {tier2 ? <TierTable tier={tier2} pending={pending} onToggle={onToggle} /> : null}
    </div>
  );
}
