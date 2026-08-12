// GpuIdle — 유휴장비 관리(/gpu/idle). 유휴 GPU 를 재사용 검증 라인에 태워
// 운영 풀로 되돌리는 자산 워크플로 화면이다.
//
// GpuEfficiency(누적 분석 > GPU 사용량 분석)와 축이 다르다: 그쪽은 "할당 대비
// 실사용 갭"(경영 설득), 여기는 "유휴 자산의 검증·재투입"(운영 작업). 겹치는
// 것은 유휴 목록 하나뿐이라 개조하지 않고 별도 화면으로 둔다.
//
// 데이터는 전부 /demo/state 폴링에 실려 온다 — 이 화면 전용 질의는 없다.
// KPI 6종은 *필터된 행*(rows)에서 파생한다. 서버 집계(state.idleSummary)는 필터
// 축(클러스터·상태·사전검증·검색) 어느 것으로도 갈리지 않아, 그대로 쓰면 필터를
// 걸어도 카드 6장이 통째로 안 움직인다. 모집단은 표 상단 "총 N개 항목"과 같다 —
// 페이지 슬라이스(visible)가 아니라 필터 결과 전체라 카드와 표가 같은 이야기를
// 한다. 무필터 시 서버 집계와 값이 일치한다(실측 185/63/164/6/13/4).
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import {
  Boxes,
  CircleCheck,
  CircleDot,
  CircleX,
  Clock,
  Target,
  ChevronLeft,
  ChevronRight,
  MoreVertical,
  Search,
} from 'lucide-react';
import IdleReuseDrawer, {
  burninLabel,
  formatIdleSpan,
  STEP_META,
  ToneBadge,
  VERDICT_COLOR,
  VERDICT_LABEL,
} from '@/components/gpu/IdleReuseDrawer';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
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
import type { IdleGPU, IdleValidation } from '@/lib/demoApi';
import { formatCount } from '@/lib/gpuMetrics';

/** Select 의 "전체" 센티널 — 빈 문자열은 Radix Select 가 값으로 받지 않는다
 *  (FleetFilterBar 와 같은 관례). */
const ALL = '__all__';

const PAGE_SIZES = [6, 10, 20, 50] as const;
/** 목업 페이저 "6 / 페이지 · 1-6 of 128개 항목". */
const DEFAULT_PAGE_SIZE = 6;
/** 페이지 번호 윈도우 — 앞쪽 N개 + 말줄임 + 마지막. */
const PAGE_WINDOW = 5;

/** "30일 이상 유휴" 판정선 — 서버 idleLongThreshold(internal/demo/idle.go)와 같은 값.
 *  카드가 서버 집계와 무필터에서 일치하려면 같은 선을 써야 한다. */
const IDLE_LONG_DAYS = 30;

const VERDICTS: IdleValidation['verdict'][] = ['idle', 'validating', 'ready', 'failed'];
/** 서버가 내는 4종 전부 — 'running' 을 빼면 "진행 중" 배지가 뜬 행을 필터로
 *  고를 수 없다(표시 어휘와 필터 어휘가 어긋난다). */
const PRECHECKS: IdleValidation['precheck'][] = ['none', 'running', 'pass', 'fail'];

/** 액션 실패 문구의 주어 — 형제 화면(GpuRuleset·GpuRemediation)과 같은 관례. */
const ACTION_LABEL: Record<'start-idle-validation' | 'return-idle-to-service', string> = {
  'start-idle-validation': '재사용 검증 착수',
  'return-idle-to-service': '운영 풀 재투입',
};

/** 목업 "2025.06.06" — 최근 검증 완료 시각. 미실행은 '-'. */
function formatDate(ms: number | undefined): string {
  if (!ms) return '-';
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}.${p(d.getMonth() + 1)}.${p(d.getDate())}`;
}

/** 권장 작업 — verdict 가 다음 한 수를 정한다(죽은 버튼을 만들지 않는다). */
const RECOMMENDED_LABEL: Record<IdleValidation['verdict'], string> = {
  idle: '재사용 검증 시작',
  validating: '번인 테스트 보기',
  ready: '재투입 관리',
  failed: '상세 보기',
};

/** 목업 demo4 의 KPI 카드 톤 — 아이콘 배지 색만 쓴다(상단 컬러 라인 없음). */
const KPI_TONE = {
  info: 'var(--metric-cpu)',
  warn: 'var(--metric-thermal)',
  ok: 'var(--metric-pod)',
  danger: 'var(--metric-fault)',
} as const;

/** 유휴 KPI 카드 — 목업은 아이콘 배지 + 라벨 한 줄, 아래 대형 숫자뿐이다.
 *  공용 KpiTile(상단 컬러 라인 + 힌트 + 진행바)과 형태가 달라 여기서만 그린다. */
function IdleKpi({
  label,
  value,
  sub,
  tone,
  icon,
}: {
  label: string;
  value: string | null;
  sub?: string;
  tone: keyof typeof KPI_TONE;
  icon: React.ReactNode;
}) {
  const c = KPI_TONE[tone];
  return (
    <Card className="gap-0 py-0">
      <CardContent className="flex flex-col gap-3 p-4">
        <span className="flex items-center gap-2 text-muted-foreground text-[13px]">
          <span
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg [&>svg]:size-4"
            style={{ color: c, backgroundColor: `color-mix(in srgb, ${c} 12%, transparent)` }}
            aria-hidden
          >
            {icon}
          </span>
          <span className="truncate">{label}</span>
        </span>
        {value === null ? (
          <Skeleton className="h-8 w-20" />
        ) : (
          <span className="flex items-baseline gap-1.5">
            <span className="font-semibold text-[2rem] leading-none tabular-nums">{value}</span>
            {sub ? <span className="text-muted-foreground text-sm tabular-nums">{sub}</span> : null}
          </span>
        )}
      </CardContent>
    </Card>
  );
}

/** 페이지 번호 목록 — 1..N 이 길어지면 앞 윈도우 + 말줄임(0) + 마지막. */
function pageNumbers(current: number, total: number): number[] {
  if (total <= PAGE_WINDOW + 2) {
    return Array.from({ length: total }, (_, i) => i + 1);
  }
  const start = Math.min(Math.max(1, current - 2), total - PAGE_WINDOW);
  const window = Array.from({ length: PAGE_WINDOW }, (_, i) => start + i);
  return window[PAGE_WINDOW - 1] >= total - 1 ? [...window, total] : [...window, 0, total];
}

export default function GpuIdle() {
  const { demoMode, state, act, refresh } = useDemo();
  const navigate = useNavigate();

  const [search, setSearch] = useState('');
  const [cluster, setCluster] = useState(ALL);
  const [verdict, setVerdict] = useState(ALL);
  const [precheck, setPrecheck] = useState(ALL);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE);
  const [selected, setSelected] = useState<string | null>(null);
  /** 액션 진행 중인 uuid — 행 단위 액션이라 전역 불리언으로 잠그면 한 행을
   *  눌렀을 때 표 전체가 회색이 된다. */
  const [actingUuid, setActingUuid] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const idle = state?.idle;
  const clusters = useMemo(
    () => [...new Set((idle ?? []).map((g) => g.cluster))].filter(Boolean).sort(),
    [idle],
  );

  // 필터 적용 — 유휴 기간 내림차순(오래 방치된 것이 먼저 보여야 한다).
  const rows = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = (idle ?? []).filter((g) => {
      if (cluster !== ALL && g.cluster !== cluster) return false;
      if (verdict !== ALL && g.validation.verdict !== verdict) return false;
      if (precheck !== ALL && g.validation.precheck !== precheck) return false;
      if (q === '') return true;
      return (
        g.instance.toLowerCase().includes(q) ||
        g.uuid.toLowerCase().includes(q) ||
        g.model.toLowerCase().includes(q)
      );
    });
    return [...filtered].sort((a, b) => b.idleMinutes - a.idleMinutes);
  }, [idle, search, cluster, verdict, precheck]);

  // KPI 6종 — 필터된 행에서 파생한다(서버 idleSummary 와 같은 분류: 뒤 4종은
  // total 의 상호배타 분할, over30d 는 그와 직교하는 장기 방치 계측선).
  const summary = useMemo(() => {
    let over30d = 0;
    let validating = 0;
    let ready = 0;
    let failed = 0;
    for (const g of rows) {
      if (g.idleDays >= IDLE_LONG_DAYS) over30d++;
      if (g.validation.verdict === 'validating') validating++;
      else if (g.validation.verdict === 'ready') ready++;
      else if (g.validation.verdict === 'failed') failed++;
    }
    return {
      total: rows.length,
      over30d,
      needsValidation: rows.length - validating - ready - failed,
      validating,
      ready,
      failed,
    };
  }, [rows]);

  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize));
  const current = Math.min(page, pageCount);
  const visible = rows.slice((current - 1) * pageSize, current * pageSize);
  const selectedGpu = useMemo(
    () => (idle ?? []).find((g) => g.uuid === selected) ?? null,
    [idle, selected],
  );

  const filterActive = search !== '' || cluster !== ALL || verdict !== ALL || precheck !== ALL;

  function resetFilters() {
    setSearch('');
    setCluster(ALL);
    setVerdict(ALL);
    setPrecheck(ALL);
    setPage(1);
  }

  async function onAct(action: 'start-idle-validation' | 'return-idle-to-service', uuid: string) {
    setActingUuid(uuid);
    setActionError(null);
    try {
      const outcome = await act(action, { uuid });
      // 재투입 성공분은 유휴 목록에서 사라진다 — 드로어가 빈 대상을 붙들지 않게
      // 선택을 먼저 놓는다.
      if (outcome.ok) {
        if (action === 'return-idle-to-service') setSelected(null);
      } else {
        // 실패를 삼키면 버튼이 죽은 것처럼 보인다 — 형제 화면과 같은 문구 관례.
        setActionError(
          outcome.status === 409
            ? `${ACTION_LABEL[action]} 거부 — ${outcome.error}`
            : `${ACTION_LABEL[action]} 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
        );
      }
      // 실패도 재조회한다 — 409 는 대개 화면 상태가 낡아서 난다.
      refresh();
    } finally {
      setActingUuid(null);
    }
  }

  /** 표의 권장 작업 — 착수만 액션이고 나머지는 상세(드로어/사전검증 화면)로 보낸다. */
  function onRecommend(g: IdleGPU) {
    if (g.validation.verdict === 'idle') {
      void onAct('start-idle-validation', g.uuid);
      return;
    }
    if (g.validation.verdict === 'validating') {
      void navigate(`/gpu/validation?uuid=${encodeURIComponent(g.uuid)}`);
      return;
    }
    setSelected(g.uuid);
  }

  /** 드로어 "위치" — CSP·리전 표시명은 플릿 요약에서만 나온다. */
  function locationOf(g: IdleGPU): string {
    const csp = state?.fleet.csps.find((c) => c.id === g.csp)?.display ?? g.csp;
    const region = state?.fleet.regions.find((r) => r.id === g.region)?.display ?? g.region;
    return [csp, region, g.cluster].filter(Boolean).join(' > ');
  }

  if (demoMode === null) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!demoMode) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>유휴장비 관리</CardTitle>
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

  const gpuTotal = state?.fleet.gpuTotal ?? 0;
  // 도착 전에는 0 이 아니라 스켈레톤 — 빈 rows 를 "유휴 0 대" 로 읽히면 안 된다.
  const kpi = (v: number) => (state === null ? null : formatCount(v));
  // 분모는 필터 여부가 정한다: 무필터면 플릿 전체(목업 "185 / 7,000"), 필터 중이면
  // 유휴 전체("19 / 185") — 필터를 걸었는데 분모가 7,000 이면 카드가 거짓말을 한다.
  const kpiDenominator = filterActive
    ? `/ ${formatCount(idle?.length ?? 0)}`
    : gpuTotal > 0
      ? `/ ${formatCount(gpuTotal)}`
      : undefined;

  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        사용하지 않는 GPU를 검증하고 안전하게 재사용할 수 있습니다.
      </p>

      {/* 1. KPI 6종 — 필터된 행에서 파생(표와 같은 모집단) */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <IdleKpi
          label={filterActive ? '필터된 유휴 GPU' : '전체 유휴 GPU'}
          value={kpi(summary.total)}
          sub={kpiDenominator}
          tone="info"
          icon={<Boxes />}
        />
        <IdleKpi label="30일 이상 유휴" value={kpi(summary.over30d)} tone="warn" icon={<Clock />} />
        <IdleKpi
          label="재사용 검증 필요"
          value={kpi(summary.needsValidation)}
          tone="danger"
          icon={<Target />}
        />
        <IdleKpi
          label="검증 진행 중"
          value={kpi(summary.validating)}
          tone="info"
          icon={<CircleDot />}
        />
        <IdleKpi label="재투입 가능" value={kpi(summary.ready)} tone="ok" icon={<CircleCheck />} />
        <IdleKpi label="검증 실패" value={kpi(summary.failed)} tone="danger" icon={<CircleX />} />
      </div>

      {/* 2. 검색 + 필터 3종 */}
      <div className="flex flex-wrap items-center gap-1.5" aria-label="유휴장비 검색 필터">
        <div className="relative w-full sm:w-72">
          <Search className="-translate-y-1/2 absolute top-1/2 left-2.5 size-3.5 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(1);
            }}
            placeholder="노드명, GPU UUID, 모델명 검색"
            className="h-8 pl-8 text-xs"
            aria-label="노드명, GPU UUID, 모델명 검색"
          />
        </div>
        <Select
          value={cluster}
          onValueChange={(v) => {
            setCluster(v);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-auto min-w-[9rem] text-xs">
            <SelectValue placeholder="클러스터 전체" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL} className="text-xs">
              클러스터 전체
            </SelectItem>
            {clusters.map((c) => (
              <SelectItem key={c} value={c} className="text-xs">
                {c}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={verdict}
          onValueChange={(v) => {
            setVerdict(v);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-auto min-w-[8rem] text-xs">
            <SelectValue placeholder="상태 전체" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL} className="text-xs">
              상태 전체
            </SelectItem>
            {VERDICTS.map((v) => (
              <SelectItem key={v} value={v} className="text-xs">
                {VERDICT_LABEL[v]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={precheck}
          onValueChange={(v) => {
            setPrecheck(v);
            setPage(1);
          }}
        >
          <SelectTrigger size="sm" className="h-8 w-auto min-w-[8rem] text-xs">
            <SelectValue placeholder="사전검증 전체" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL} className="text-xs">
              사전검증 전체
            </SelectItem>
            {PRECHECKS.map((p) => (
              <SelectItem key={p} value={p} className="text-xs">
                {STEP_META[p].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant="outline"
          size="sm"
          className="h-8 px-3 text-xs"
          disabled={!filterActive}
          onClick={resetFilters}
        >
          필터 초기화
        </Button>
      </div>

      {actionError ? (
        <Alert variant="destructive">
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      {/* 3. 표 */}
      <div className="flex flex-col gap-2">
        <p className="text-muted-foreground text-xs">
          총 {formatCount(rows.length)}개 항목
        </p>
        {state === null ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        ) : (
          <Table>
            {/* 목업 헤더 = 회색 보통 굵기, 행 높이 52px */}
            <TableHeader className="[&_th]:h-12 [&_th]:font-normal [&_th]:text-muted-foreground [&_th]:text-xs">
              <TableRow>
                <TableHead className="w-44">노드 / GPU</TableHead>
                <TableHead className="w-32">GPU 모델</TableHead>
                <TableHead className="w-32">클러스터</TableHead>
                <TableHead className="w-28">유휴 기간</TableHead>
                <TableHead className="w-28">현재 상태</TableHead>
                <TableHead className="w-24">최근 검증</TableHead>
                <TableHead className="w-24">사전검증</TableHead>
                <TableHead className="w-28">번인 테스트</TableHead>
                <TableHead className="w-36">권장 작업</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9} className="text-center text-muted-foreground">
                    조건에 맞는 유휴 GPU 가 없다.
                  </TableCell>
                </TableRow>
              ) : (
                visible.map((g) => (
                  <TableRow
                    key={g.uuid}
                    className="h-[52px] cursor-pointer"
                    // 드로어가 붙들고 있는 원본 행을 표에서도 표시한다(목업의 선택 행 강조).
                    data-state={g.uuid === selected ? 'selected' : undefined}
                    onClick={() => setSelected(g.uuid)}
                  >
                    <TableCell className="font-mono text-xs">
                      {g.instance} / {g.device}
                    </TableCell>
                    <TableCell className="text-xs">{g.model || '—'}</TableCell>
                    <TableCell className="text-xs">{g.cluster || '—'}</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {formatIdleSpan(g.idleMinutes, false)}
                    </TableCell>
                    <TableCell>
                      <ToneBadge
                        text={VERDICT_LABEL[g.validation.verdict]}
                        color={VERDICT_COLOR[g.validation.verdict]}
                      />
                    </TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {formatDate(g.validation.lastAt)}
                    </TableCell>
                    <TableCell>
                      <ToneBadge
                        text={STEP_META[g.validation.precheck].label}
                        color={STEP_META[g.validation.precheck].color}
                      />
                    </TableCell>
                    <TableCell>
                      <ToneBadge
                        text={burninLabel(g.validation)}
                        color={STEP_META[g.validation.burnin].color}
                      />
                    </TableCell>
                    <TableCell>
                      <span className="flex items-center justify-between gap-1">
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          // 목업의 권장 작업 = 파란 테두리·파란 글자.
                          style={{ color: KPI_TONE.info, borderColor: KPI_TONE.info }}
                          disabled={actingUuid === g.uuid}
                          onClick={(e) => {
                            e.stopPropagation();
                            onRecommend(g);
                          }}
                        >
                          {RECOMMENDED_LABEL[g.validation.verdict]}
                        </Button>
                        {/* 목업의 행 끝 ⋮ — 상세(드로어)로 보낸다(행 클릭과 같은 목적지). */}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7 text-muted-foreground"
                          aria-label={`${g.instance} 상세`}
                          onClick={(e) => {
                            e.stopPropagation();
                            setSelected(g.uuid);
                          }}
                        >
                          <MoreVertical className="size-4" />
                        </Button>
                      </span>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}

        {/* 4. 페이지네이션 */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-muted-foreground text-xs tabular-nums">
            {rows.length === 0
              ? '0개 항목'
              : `${(current - 1) * pageSize + 1}-${Math.min(current * pageSize, rows.length)} of ${formatCount(rows.length)}개 항목`}
          </span>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              disabled={current <= 1}
              onClick={() => setPage(current - 1)}
              aria-label="이전 페이지"
            >
              <ChevronLeft className="size-4" />
            </Button>
            {pageNumbers(current, pageCount).map((n, i) =>
              n === 0 ? (
                // biome-ignore lint/suspicious/noArrayIndexKey: 말줄임은 위치가 곧 정체성이다
                <span key={`gap-${i}`} className="px-1 text-muted-foreground text-xs">
                  …
                </span>
              ) : (
                <Button
                  key={n}
                  variant="outline"
                  size="icon"
                  className="size-8 text-xs tabular-nums"
                  // 목업의 현재 페이지 = 파란 테두리·파란 글자(채움 아님).
                  style={
                    n === current
                      ? { color: KPI_TONE.info, borderColor: KPI_TONE.info }
                      : undefined
                  }
                  onClick={() => setPage(n)}
                  aria-label={`${n} 페이지`}
                  aria-current={n === current ? 'page' : undefined}
                >
                  {n}
                </Button>
              ),
            )}
            <Button
              variant="outline"
              size="icon"
              className="size-8"
              disabled={current >= pageCount}
              onClick={() => setPage(current + 1)}
              aria-label="다음 페이지"
            >
              <ChevronRight className="size-4" />
            </Button>
            <Select
              value={String(pageSize)}
              onValueChange={(v) => {
                setPageSize(Number(v));
                setPage(1);
              }}
            >
              <SelectTrigger size="sm" className="h-8 w-auto min-w-[6.5rem] text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAGE_SIZES.map((n) => (
                  <SelectItem key={n} value={String(n)} className="text-xs">
                    {n} / 페이지
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      <IdleReuseDrawer
        gpu={selectedGpu}
        location={selectedGpu ? locationOf(selectedGpu) : ''}
        acting={actingUuid !== null && actingUuid === selected}
        error={actionError}
        onClose={() => {
          setSelected(null);
          setActionError(null);
        }}
        onAct={onAct}
      />
    </div>
  );
}
