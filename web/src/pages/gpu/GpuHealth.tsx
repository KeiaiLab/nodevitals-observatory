// GpuHealth — Step 2 "무음 장애 감지". Kubernetes Ready 뒤에 숨는 GPU 열화
// (온도 램프·ECC 누적·XID 알림)를 Health Score 판정 근거와 함께 드러낸다.
// 차트는 Explorer 의 CSP-안전 Recharts 셋업(원시 컴포넌트 + var(--metric-*)
// stroke 문자열)을 그대로 미러한다 — <style> 주입·dangerouslySetInnerHTML 0.
// demo off 세션은 헬스 브라우저 모드로 강등: useFleet 셀에서 GPU 를 골라 동일
// 온도/ECC 차트만 렌더한다(시나리오 배너·타임라인은 데모 인스턴스 전용).
import { BellRing, Gauge, ShieldCheck, TrendingDown } from 'lucide-react';
import { useMemo, useState } from 'react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import AlertTimeline from '@/components/gpu/AlertTimeline';
import KpiTile from '@/components/gpu/KpiTile';
import StatusBanner, { type StatusLevel } from '@/components/gpu/StatusBanner';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { useDemo } from '@/hooks/demoContext';
import { useFleet } from '@/hooks/useFleet';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api, type MatrixData } from '@/lib/api';
import type { DemoState, ScenarioPhase, VictimState } from '@/lib/demoApi';
import { METRIC_GPU_ECC_CORR, METRIC_GPU_TEMP, selector } from '@/lib/gpuMetrics';

// 최근 1시간 창 — Explorer 의 step 산식(max(15, floor(range/500)))을 미러.
const RANGE_SECONDS = 3600;
const STEP_SECONDS = Math.max(15, Math.floor(RANGE_SECONDS / 500));
const CHART_POLL_MS = 30_000;
const TEMP_LIMIT_C = 80;
// 온도 차트 Y 하한 — 임계선이 플롯 중앙에 오도록 고정한다(위 YAxis 주석 참조).
const TEMP_AXIS_MIN_C = 60;

// 배너 설명에 고정하는 핵심 카피 — 이 화면이 존재하는 이유.
const CORE_COPY =
  'Kubernetes 는 Ready 를 보고하지만 GPU 는 열화 중이다 — CSP auto-repair 가 잡지 못하는 영역';

interface Point {
  time: number;
  value: number | null;
}

// gpu_uuid 등호 매처는 시리즈를 유일하게 특정한다 — matrix 첫 시리즈만 취한다.
function toPoints(matrix: MatrixData | null): Point[] {
  const series = matrix?.result[0];
  if (!series) return [];
  return series.values.map(([t, v]) => ({ time: t, value: Number(v) }));
}

function formatTick(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
  });
}

function scoreColor(score: number): string {
  if (score >= 90) return 'var(--metric-pod)';
  if (score >= 70) return 'var(--metric-thermal)';
  return 'var(--metric-fault)';
}

function bannerForPhase(phase: ScenarioPhase): { level: StatusLevel; title: string } {
  if (phase === 'normal') return { level: 'ok', title: '정상' };
  if (phase === 'degrading') return { level: 'warn', title: '주의 — 무음 열화 진행' };
  return { level: 'suspect', title: '장애 의심 — 격리 검토 필요' };
}

const TOOLTIP_STYLE = {
  backgroundColor: 'var(--popover)',
  borderColor: 'var(--border)',
  color: 'var(--popover-foreground)',
  fontSize: 12,
} as const;

function useRangeSeries(metric: string, uuid: string) {
  const query = usePolledQuery(
    async () => {
      const endSec = Math.floor(Date.now() / 1000);
      const res = await api.rangeQuery(
        selector(metric, { gpu_uuid: uuid }),
        endSec - RANGE_SECONDS,
        endSec,
        STEP_SECONDS,
      );
      return res.data;
    },
    CHART_POLL_MS,
    [metric, uuid],
  );
  const points = useMemo(() => toPoints(query.data), [query.data]);
  return { points, loading: query.data === null && query.error === null, error: query.error };
}

// ---- 온도 라인차트 (임계 80°C 참조선 + 초과 대역) ----

function TempChartCard({ uuid, replacing }: { uuid: string; replacing: boolean }) {
  const { points, loading, error } = useRangeSeries(METRIC_GPU_TEMP, uuid);
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>온도</CardTitle>
        <CardDescription>최근 1시간 · 30초 갱신 — 임계 {TEMP_LIMIT_C}°C 초과 시 열화 의심</CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-64 w-full" />
        ) : error ? (
          <p className="py-8 text-center text-destructive text-sm">온도 시계열 조회 실패 — {error}</p>
        ) : points.length === 0 ? (
          <p className="py-8 text-center text-muted-foreground text-sm">
            {replacing
              ? '교체 창 — 데이터 소멸. GPU 교체로 기존 uuid 시리즈가 종료되었다.'
              : '표시할 데이터가 없다.'}
          </p>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <LineChart data={points} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
              <XAxis
                dataKey="time"
                type="number"
                domain={['dataMin', 'dataMax']}
                tickFormatter={formatTick}
                tick={{ fontSize: 12 }}
                className="fill-muted-foreground"
              />
              {/* 고정 60~100°C — 'auto' 하한은 데이터(83~85°C)에 붙어 임계 80°C
                  대역(ReferenceArea)이 플롯 전체를 덮었다. 정상인데 화면이
                  새빨개지는 오독의 원인. 운용 envelope 를 고정해 임계선이
                  차트 중앙에 오고 붉은 대역이 '위쪽 구간'으로 읽히게 한다. */}
              <YAxis
                domain={[TEMP_AXIS_MIN_C, 100]}
                ticks={[60, 70, 80, 90, 100]}
                width={48}
                tick={{ fontSize: 12 }}
                className="fill-muted-foreground"
              />
              <Tooltip labelFormatter={(t) => formatTick(Number(t))} contentStyle={TOOLTIP_STYLE} />
              <Legend wrapperStyle={{ fontSize: 12 }} />
              <ReferenceArea
                y1={TEMP_LIMIT_C}
                y2={100}
                fill="var(--metric-fault)"
                fillOpacity={0.08}
                stroke="none"
              />
              <ReferenceLine
                y={TEMP_LIMIT_C}
                stroke="var(--metric-fault)"
                strokeDasharray="4 4"
                label={{
                  value: `임계 ${TEMP_LIMIT_C}°C`,
                  position: 'insideTopRight',
                  fill: 'var(--metric-fault)',
                  fontSize: 11,
                }}
              />
              <Line
                type="monotone"
                dataKey="value"
                name="GPU 온도(°C)"
                stroke="var(--metric-thermal)"
                strokeWidth={2}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}

// ---- 정정 ECC 누적 막대차트 ----

function EccChartCard({ uuid }: { uuid: string }) {
  const { points, loading, error } = useRangeSeries(METRIC_GPU_ECC_CORR, uuid);
  const nums = useMemo(
    () => points.map((p) => p.value).filter((v): v is number => v != null),
    [points],
  );
  // 카운터 시리즈 — 창 내 last-first 가 0 이면 "무증가" (교체로 카운터가
  // 리셋되면 음수가 될 수 있어 0 으로 클램프).
  const delta = nums.length >= 2 ? Math.max(0, nums[nums.length - 1] - nums[0]) : 0;
  // 정상 GPU 는 카운터가 통째로 0 이다 — 높이 0 인 막대는 아무것도 안 그려져
  // "빈 카드"(고장난 화면)로 읽힌다. 0 계열은 차트 대신 명시적 정상 패널로.
  const allZero = nums.length > 0 && Math.max(...nums) === 0;
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>정정 ECC 누적</CardTitle>
        <CardDescription>최근 1시간 — 정정 가능 메모리 오류 카운터</CardDescription>
        {nums.length > 0 ? (
          <CardAction>
            {delta === 0 ? (
              <Badge variant="outline">정정 ECC 무증가</Badge>
            ) : (
              <Badge variant="destructive">1시간 +{delta.toLocaleString('ko-KR')}건</Badge>
            )}
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent>
        {loading ? (
          <Skeleton className="h-64 w-full" />
        ) : error ? (
          <p className="py-8 text-center text-destructive text-sm">ECC 시계열 조회 실패 — {error}</p>
        ) : points.length === 0 ? (
          <p className="py-8 text-center text-muted-foreground text-sm">표시할 데이터가 없다.</p>
        ) : allZero ? (
          <div className="flex h-full min-h-[260px] flex-col items-center justify-center gap-2 rounded-md border border-dashed">
            <span
              className="font-bold text-5xl tabular-nums leading-none"
              style={{ color: 'var(--metric-pod)' }}
            >
              0
            </span>
            <p className="font-medium text-sm">최근 1시간 정정 ECC 증가 없음</p>
            <p className="text-muted-foreground text-xs">메모리 열화 신호 미검출 — 카운터 누적 0건</p>
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <BarChart data={points} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
              <XAxis
                dataKey="time"
                type="number"
                domain={['dataMin', 'dataMax']}
                tickFormatter={formatTick}
                tick={{ fontSize: 12 }}
                className="fill-muted-foreground"
              />
              <YAxis
                domain={['auto', 'auto']}
                allowDecimals={false}
                width={48}
                tick={{ fontSize: 12 }}
                className="fill-muted-foreground"
              />
              <Tooltip labelFormatter={(t) => formatTick(Number(t))} contentStyle={TOOLTIP_STYLE} />
              <Legend wrapperStyle={{ fontSize: 12 }} />
              <Bar
                dataKey="value"
                name="정정 ECC 누적"
                fill="var(--metric-fault)"
                isAnimationActive={false}
              />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}

// ---- victim 요약 카드 ----

function VictimCard({ victim }: { victim: VictimState }) {
  const { health } = victim;
  const score = Math.round(health.score);
  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>감시 대상 GPU</CardTitle>
        <CardDescription className="font-mono text-xs">{victim.uuid}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-4">
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline" className="font-mono">
            {victim.instance}
          </Badge>
          <Badge variant="outline" className="font-mono">
            {victim.device}
          </Badge>
          <Badge variant="secondary">{victim.model}</Badge>
          <Badge variant="secondary">{victim.pool}</Badge>
        </div>
        <div className="flex items-baseline gap-2">
          <span
            className="font-bold text-4xl tabular-nums leading-none"
            style={{ color: scoreColor(score) }}
          >
            {score}
          </span>
          <span className="text-muted-foreground text-sm">Health Score</span>
        </div>
        <div className="flex flex-col gap-1.5">
          <span className="font-medium text-muted-foreground text-xs">판정 근거</span>
          {health.deductions.length === 0 ? (
            <p className="text-muted-foreground text-sm">감점 항목 없음</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {health.deductions.map((d) => (
                <li key={d.code} className="flex items-start gap-2 text-sm">
                  <Badge variant="outline" className="shrink-0 font-mono text-[10px]">
                    {d.code}
                  </Badge>
                  <span className="flex-1 leading-snug">{d.message}</span>
                  <span
                    className="shrink-0 font-medium tabular-nums"
                    style={{ color: 'var(--metric-fault)' }}
                  >
                    -{d.penalty}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
        {/* 위치·소속 라벨/값 행 — 목업(demo4) 우측 상세 패널 규약. 카드 하단
            빈 공간을 실제 식별 정보로 채운다(전부 이미 받은 victim 필드). */}
        <dl className="mt-auto flex flex-col gap-1.5 border-t pt-3 text-sm">
          {[
            ['CSP', victim.csp],
            ['클러스터', victim.cluster],
            ['서빙 풀', victim.pool],
            ['GPU 모델', victim.model],
          ].map(([k, v]) => (
            <div key={k} className="flex items-center justify-between gap-3">
              <dt className="text-muted-foreground text-xs">{k}</dt>
              <dd className="truncate font-medium text-xs">{v}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

// ---- 데모 모드: 시나리오 뷰 ----

function DemoHealthView({ state }: { state: DemoState }) {
  // 알림 확인은 서버에 기록된다(ack-alert) — 로컬 표시만 바꾸면 새로고침에
  // 되돌아와 "눌러도 안 되는 버튼"이 된다.
  const { act } = useDemo();
  const { scenario, alerts } = state;
  const victim = scenario.victim;
  const banner = bannerForPhase(scenario.phase);
  const score = Math.round(victim.health.score);
  const unacked = alerts.filter((a) => !a.acked).length;
  const critical = alerts.filter((a) => a.severity === 'critical' && !a.acked).length;
  const major = alerts.filter((a) => a.severity === 'major' && !a.acked).length;
  return (
    <div className="flex flex-col gap-4">
      <StatusBanner
        level={banner.level}
        title={banner.title}
        description={CORE_COPY}
        since={scenario.phaseStartAt}
      />
      {/* 상단 KPI 4타일 — 목업(demo1) 상단 타일 행 규약. 이 화면의 논지가
          "K8s 는 Ready 인데 헬스는 감점 중"이므로 두 값을 나란히 세운다.
          전부 이미 받은 state 에서 파생 — 신규 질의 0. */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <KpiTile
          label="Kubernetes 노드 상태"
          value="Ready"
          hint="CSP auto-repair 판정 — 이상 없음"
          tone="ok"
          icon={<ShieldCheck className="size-4" />}
        />
        <KpiTile
          label="GPU Health Score"
          value={String(score)}
          sub="/ 100"
          hint={score >= 90 ? '경미한 감점' : '열화 진행 — 조치 대상'}
          tone={score >= 90 ? 'ok' : score >= 70 ? 'warn' : 'danger'}
          icon={<Gauge className="size-4" />}
          progress={score}
        />
        <KpiTile
          label="감점 항목"
          value={`${victim.health.deductions.length}건`}
          hint={
            victim.health.deductions.length === 0
              ? '감점 없음'
              : victim.health.deductions.map((d) => d.code).join(' · ')
          }
          tone={victim.health.deductions.length === 0 ? 'ok' : 'warn'}
          icon={<TrendingDown className="size-4" />}
        />
        <KpiTile
          label="미확인 알림"
          value={`${unacked}건`}
          hint={`전체 ${alerts.length}건`}
          tone={critical > 0 ? 'danger' : unacked > 0 ? 'warn' : 'ok'}
          icon={<BellRing className="size-4" />}
          badges={[
            ...(critical > 0 ? [{ text: `Critical ${critical}`, tone: 'danger' as const }] : []),
            ...(major > 0 ? [{ text: `Major ${major}`, tone: 'warn' as const }] : []),
          ]}
        />
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <VictimCard victim={victim} />
        <div className="lg:col-span-2">
          {/* uuid 를 key 로 — replacing 창에서 GPU 교체 시 차트 상태를 리셋한다. */}
          <TempChartCard
            key={victim.uuid}
            uuid={victim.uuid}
            replacing={scenario.phase === 'replacing'}
          />
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <EccChartCard key={victim.uuid} uuid={victim.uuid} />
        </div>
        <Card className="h-full">
          <CardHeader>
            <CardTitle>알림 타임라인</CardTitle>
            <CardDescription>{alerts.length}건</CardDescription>
          </CardHeader>
          {/* 6건 이상이면 카드 높이(옆 ECC 차트 기준)를 넘어 마지막 항목이
              잘렸다 — 잘림 대신 스크롤 + 하단 안내(목업 이벤트 피드 규약). */}
          <CardContent className="flex flex-col gap-2">
            <div className="max-h-[300px] overflow-y-auto pr-1">
              <AlertTimeline
                alerts={alerts}
                onAck={(id) => {
                  void act('ack-alert', { id });
                }}
              />
            </div>
            <p className="border-t pt-2 text-center text-muted-foreground text-xs">
              전체 {alerts.length}건 — 스크롤하여 확인
            </p>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

// ---- 실서비스(demo off): 헬스 브라우저 강등 모드 ----

function BrowserHealthView() {
  const { cells } = useFleet();
  const [instanceSel, setInstanceSel] = useState('');
  const [deviceSel, setDeviceSel] = useState('');

  const instances = useMemo(
    () => [...new Set((cells ?? []).map((c) => c.instance).filter(Boolean))].sort(),
    [cells],
  );
  // 파생 선택: 미선택/무효 선택은 첫 항목으로 수렴 — effect 없이 항상 유효한
  // GPU 하나가 선택돼 있게 한다.
  const instance = instanceSel && instances.includes(instanceSel) ? instanceSel : (instances[0] ?? '');
  const devices = useMemo(
    () => (cells ?? []).filter((c) => c.instance === instance).map((c) => c.device),
    [cells, instance],
  );
  const device = deviceSel && devices.includes(deviceSel) ? deviceSel : (devices[0] ?? '');
  const uuid =
    (cells ?? []).find((c) => c.instance === instance && c.device === device)?.uuid ?? '';

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>헬스 브라우저</CardTitle>
          <CardDescription>
            플릿에서 GPU 를 선택해 온도·ECC 신호를 직접 조회한다. 데모 시나리오는 데모 인스턴스
            전용.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {cells === null ? (
            <Skeleton className="h-9 w-full max-w-xl" />
          ) : cells.length === 0 ? (
            <p className="text-muted-foreground text-sm">조회 가능한 GPU 시리즈가 없다.</p>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={instance}
                onValueChange={(v) => {
                  setInstanceSel(v);
                  setDeviceSel('');
                }}
              >
                <SelectTrigger className="w-64" aria-label="인스턴스 선택">
                  <SelectValue placeholder="인스턴스" />
                </SelectTrigger>
                <SelectContent>
                  {instances.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={device} onValueChange={setDeviceSel}>
                <SelectTrigger className="w-40" aria-label="디바이스 선택">
                  <SelectValue placeholder="디바이스" />
                </SelectTrigger>
                <SelectContent>
                  {devices.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {uuid ? (
                <Badge variant="outline" className="font-mono text-xs">
                  {uuid}
                </Badge>
              ) : null}
            </div>
          )}
        </CardContent>
      </Card>
      {uuid ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <TempChartCard key={`t-${uuid}`} uuid={uuid} replacing={false} />
          <EccChartCard key={`e-${uuid}`} uuid={uuid} />
        </div>
      ) : null}
    </div>
  );
}

function HealthSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-24 w-full" />
      <div className="grid gap-4 lg:grid-cols-3">
        <Skeleton className="h-72" />
        <Skeleton className="h-72 lg:col-span-2" />
      </div>
    </div>
  );
}

export default function GpuHealth() {
  const { demoMode, state } = useDemo();
  if (demoMode === null) return <HealthSkeleton />;
  if (demoMode === false) return <BrowserHealthView />;
  if (!state) return <HealthSkeleton />;
  return <DemoHealthView state={state} />;
}
