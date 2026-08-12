// GpuValidation — Step 5 "GPU 사전검증 · 번인" (데모 전용 — demo off 는 안내 카드만).
//
// 재사용 검증 라인 한 페이지: 위쪽은 사전검증(구성 적합성 4항목 + 실시간 로그),
// 아래쪽은 번인이다. 대상은 `?uuid=` 로 받고(유휴장비 관리의 [재사용 검증 시작]
// 진입점), 지정이 없으면 시나리오 victim = 교체 노드다.
//
// 서사: 유휴 자산은 "구성은 멀쩡한데 번인이 ECC 를 잡아낸다"를, victim 은 노드
// 교체 후 번인 1차 실패(Health 75, "재검증 필요 (Uncordon 보류)") → 재번인 →
// 96 통과 → 재투입을 보여준다. 둘 다 "통과시키지 않는 장면"(관문 차단)이 핵심.
//
// 번인 폼(프로파일·지속시간·목표 사용률)은 서버 설정값을 초기값으로 받고
// configure-burnin 으로 저장한다 — 저장하면 단계 길이·부하 곡선이 실제로 바뀐다.
// 차트는 Explorer 의 CSP-안전 Recharts 셋업을 그대로 미러한다(isAnimationActive
// false / contentStyle var(--popover) / step=max(15, floor(range/500))).
import { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import {
  Boxes,
  ChevronRight,
  Circle,
  CircleCheck,
  CircleX,
  Clock,
  Container,
  Cpu,
  Download,
  Info,
  Maximize2,
  MonitorCog,
  Network,
  Play,
  RefreshCw,
  Server,
  TriangleAlert,
} from 'lucide-react';
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { Link, useSearchParams } from 'react-router';
import GateDiagram, { type GateStage } from '@/components/gpu/GateDiagram';
import HealthGauge from '@/components/gpu/HealthGauge';
import ReuseStepper from '@/components/gpu/ReuseStepper';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { Slider } from '@/components/ui/slider';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useDemo } from '@/hooks/demoContext';
import { usePolledQuery } from '@/hooks/usePolledQuery';
import { api } from '@/lib/api';
import { type CheckItem, PHASE_LABELS, type ScenarioPhase } from '@/lib/demoApi';
import { METRIC_GPU_UTIL, selector } from '@/lib/gpuMetrics';
import { cn } from '@/lib/utils';
import {
  fetchValidation,
  type ValidationDetail,
  type ValidationItem,
  type ValidationLog,
  type ValidationRun,
  validationMessage,
} from '@/lib/validation';

// 번인 프로파일 옵션 — B200 은 실장비 부재로 「시뮬레이션」 표기.
const BURNIN_PROFILES = [
  { value: 'High-Intensity-CUDA-v1', label: 'High-Intensity-CUDA-v1', simulated: false },
  { value: 'B200-Blackwell-v1', label: 'B200-Blackwell-v1', simulated: true },
] as const;

// 번인 부하 곡선 — 최근 30분, 15s 폴링 (Explorer step 규약 미러).
const CURVE_RANGE_SECONDS = 30 * 60;
const CURVE_POLL_MS = 15_000;
const MIN_STEP_SECONDS = 15;
const TARGET_POINTS = 500;

// 검증 상세 폴링 — 진행 중이면 2s(로그가 흘러야 한다), 아니면 15s.
const DETAIL_POLL_ACTIVE_MS = 2_000;
const DETAIL_POLL_IDLE_MS = 15_000;

/** 교체 노드(victim)에 검증 회차가 존재하는 단계 — 그 전에는 교체할 개체가
 *  아직 없어 서버가 빈 회차를 낸다(validation.go victimRuns). 기본 대상 선택에만
 *  쓴다(판정은 서버 몫). */
const VICTIM_RUN_PHASES: ScenarioPhase[] = [
  'burnin-1',
  'burnin-failed',
  'burnin-2',
  'ready-to-return',
  'returned',
];

/** phase → 관문 다이어그램 3단 상태 매핑.
 *  burnin-1/2·burnin-failed = 관문 active + 운영풀 blocked / ready-to-return =
 *  관문 done / returned = 전부 done. 명세 외 단계는 보수적 가정: 교체 진행
 *  (draining·replacing)은 K8s Ready 대기 + 풀 차단, 그 이전 단계는 전부 대기. */
function gateStages(phase: ScenarioPhase): GateStage[] {
  const make = (
    k8s: GateStage['status'],
    gate: GateStage['status'],
    pool: GateStage['status'],
  ): GateStage[] => [
    { id: 'k8s-ready', label: 'Kubernetes Ready', status: k8s },
    { id: 'gate', label: '사전 검증 관문', status: gate },
    { id: 'pool', label: '운영 풀', status: pool },
  ];
  switch (phase) {
    case 'burnin-1':
    case 'burnin-failed':
    case 'burnin-2':
      return make('done', 'active', 'blocked');
    case 'ready-to-return':
      return make('done', 'done', 'idle');
    case 'returned':
      return make('done', 'done', 'done');
    case 'draining':
    case 'replacing':
      return make('active', 'idle', 'blocked');
    default:
      return make('idle', 'idle', 'idle');
  }
}

function StatusIcon({ status }: { status: ValidationItem['status'] }) {
  if (status === 'pass') {
    return <CircleCheck className="size-4 shrink-0" style={{ color: 'var(--metric-pod)' }} />;
  }
  if (status === 'warn') {
    return <TriangleAlert className="size-4 shrink-0" style={{ color: 'var(--metric-thermal)' }} />;
  }
  if (status === 'fail') {
    return <CircleX className="size-4 shrink-0" style={{ color: 'var(--metric-fault)' }} />;
  }
  if (status === 'running') {
    return (
      <span className="flex size-4 shrink-0 items-center justify-center">
        <span className="size-2.5 animate-pulse rounded-full bg-primary" />
      </span>
    );
  }
  return <Circle className="size-4 shrink-0 text-muted-foreground/50" />;
}

// 검증 항목 상태 어휘 — 번인 체크리스트(CheckItem 4종)와 사전검증(5종)이 같은
// 표현을 쓴다. 목업 배지 문구 그대로.
const CHECK_STATUS_TEXT: Record<ValidationItem['status'], string> = {
  pass: '통과',
  warn: '주의',
  fail: '실패',
  running: '진행 중',
  pending: '대기',
};

const CHECK_STATUS_COLOR: Record<ValidationItem['status'], string | undefined> = {
  pass: 'var(--metric-pod)',
  warn: 'var(--metric-thermal)',
  fail: 'var(--metric-fault)',
  running: undefined,
  pending: undefined,
};

function StatusBadge({ status }: { status: ValidationItem['status'] }) {
  const color = CHECK_STATUS_COLOR[status];
  return (
    <Badge
      variant="outline"
      className="gap-1"
      style={
        color
          ? {
              color,
              borderColor: color,
              backgroundColor: `color-mix(in srgb, ${color} 12%, transparent)`,
            }
          : undefined
      }
    >
      <StatusIcon status={status} />
      {CHECK_STATUS_TEXT[status]}
    </Badge>
  );
}

function formatTimeTick(unixSec: number): string {
  return new Date(unixSec * 1000).toLocaleTimeString('ko-KR', {
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** 로그 타임스탬프 — 목업 "14:32:10" 형식(서버 시계 기준, EventStream 관례). */
function formatLogTime(ms: number): string {
  return new Date(ms).toLocaleTimeString('ko-KR', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

/** 실행 시각 — 목업 "2026.07.29 14:32:16" 형식. */
function formatRanAt(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}.${p(d.getMonth() + 1)}.${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** 소요 시간 — 목업 "(00:00:06)" 형식. */
function formatDuration(sec: number): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(Math.floor(sec / 3600))}:${p(Math.floor((sec % 3600) / 60))}:${p(sec % 60)}`;
}

/** 회차 전체 예상 소요(초) — 항목 계획 소요의 합. 서버가 항목마다 계획 소요를
 *  싣기 때문에 미실행 상태에서도 낼 수 있다. */
function plannedSeconds(run: ValidationRun): number {
  return run.items.reduce((acc, item) => acc + (item.durationSec ?? 0), 0);
}

// ---- 화면 조각 ----

/** 브레드크럼 — 목업 상단 경로. 이 화면에서만 쓰므로 페이지 로컬이다
 *  (다른 화면이 필요해지면 그때 components/gpu/ 로 승격). */
function Breadcrumb({ target }: { target: string }) {
  const crumbs = ['자산 관리', '유휴장비 관리', target, '재사용 검증', '사전검증'];
  return (
    <nav aria-label="경로" className="flex flex-wrap items-center gap-1 text-muted-foreground text-xs">
      {crumbs.map((crumb, index) => (
        <span key={crumb} className="flex items-center gap-1">
          {index > 0 ? <ChevronRight className="size-3 shrink-0" /> : null}
          {index === 1 ? (
            <Link to="/gpu/idle" className="hover:text-foreground hover:underline">
              {crumb}
            </Link>
          ) : (
            <span className={index === crumbs.length - 1 ? 'font-medium text-foreground' : undefined}>
              {crumb}
            </span>
          )}
        </span>
      ))}
    </nav>
  );
}

/** 자산 요약 3칸 — 자산 · GPU 모델 · 클러스터. 목업처럼 칸마다 아이콘 타일을
 *  왼쪽에 두고 세로 구분선으로 나눈다. */
function AssetSummary({ detail }: { detail: ValidationDetail }) {
  const cells = [
    { label: '자산', value: `${detail.instance} / ${detail.device}`, Icon: Server },
    { label: 'GPU 모델', value: detail.model, Icon: Cpu },
    { label: '클러스터', value: detail.cluster, Icon: Network },
  ];
  return (
    <Card className="gap-0 py-0">
      <CardContent className="grid gap-3 p-5 sm:grid-cols-3">
        {cells.map((cell, index) => (
          <div
            key={cell.label}
            className={cn(
              'flex items-center gap-3',
              index > 0 ? 'sm:border-l sm:pl-5' : undefined,
            )}
          >
            <span
              className="flex size-10 shrink-0 items-center justify-center rounded-lg"
              style={{
                color: 'var(--metric-cpu)',
                backgroundColor: 'color-mix(in srgb, var(--metric-cpu) 12%, transparent)',
              }}
            >
              <cell.Icon className="size-5" />
            </span>
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="text-muted-foreground text-xs">{cell.label}</span>
              <span className="truncate font-semibold text-base">{cell.value}</span>
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

// 항목 카테고리 아이콘 — 목업의 행머리 아이콘. 서버 item.id 로 고른다(모르는
// id 는 기본 아이콘). 색은 판정색이 아니라 카테고리색이다 — 판정은 배지가 한다.
const ITEM_ICON: Record<string, typeof Cpu> = {
  driver: MonitorCog,
  cuda: Boxes,
  network: Network,
  k8s: Container,
  'k8s-ready': Container,
};

function ItemIcon({ id }: { id: string }) {
  const Icon = ITEM_ICON[id] ?? Cpu;
  return (
    <span
      className="flex size-8 shrink-0 items-center justify-center rounded-lg"
      style={{
        color: 'var(--metric-cpu)',
        backgroundColor: 'color-mix(in srgb, var(--metric-cpu) 12%, transparent)',
      }}
    >
      <Icon className="size-4" />
    </span>
  );
}

/** 검증 항목 표 — 행 클릭 시 항목 상세를 펼친다. */
function ItemTable({ run, caption }: { run: ValidationRun; caption: string }) {
  const [openId, setOpenId] = useState<string | null>(null);
  return (
    <>
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/50 hover:bg-muted/50">
            <TableHead>검증 항목</TableHead>
            <TableHead className="w-28">현재 결과</TableHead>
            <TableHead className="w-48">최근 실행 시간</TableHead>
            <TableHead className="w-52">상세 정보</TableHead>
            <TableHead className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {run.items.map((item) => {
            const open = openId === item.id;
            return (
              <Fragment key={item.id}>
                <TableRow
                  className="cursor-pointer"
                  onClick={() => setOpenId(open ? null : item.id)}
                >
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <ItemIcon id={item.id} />
                      <div className="flex flex-col">
                        <span className="font-medium text-sm">{item.label}</span>
                        <span className="text-muted-foreground text-xs">{item.describe}</span>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={item.status} />
                  </TableCell>
                  <TableCell>
                    {item.ranAt ? (
                      <div className="flex flex-col text-xs tabular-nums">
                        <span>{formatRanAt(item.ranAt)}</span>
                        <span className="text-muted-foreground">
                          ({formatDuration(item.durationSec ?? 0)})
                        </span>
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-xs">—</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-col text-xs">
                      <span className="font-mono">{item.detail}</span>
                      {item.detailSub ? (
                        <span className="text-muted-foreground">{item.detailSub}</span>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <button
                      type="button"
                      aria-expanded={open}
                      aria-label={`${item.label} 상세`}
                      className="text-muted-foreground hover:text-foreground"
                      onClick={(e) => {
                        e.stopPropagation();
                        setOpenId(open ? null : item.id);
                      }}
                    >
                      <ChevronRight
                        className={cn('size-4 transition-transform', open ? 'rotate-90' : undefined)}
                      />
                    </button>
                  </TableCell>
                </TableRow>
                {open ? (
                  <TableRow className="bg-muted/30 hover:bg-muted/30">
                    <TableCell colSpan={5}>
                      <dl className="grid gap-2 text-xs sm:grid-cols-4">
                        <div className="flex flex-col gap-0.5 sm:col-span-2">
                          <dt className="text-muted-foreground">검증 내용</dt>
                          <dd>{item.describe}</dd>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <dt className="text-muted-foreground">측정 결과</dt>
                          <dd className="font-mono">{item.detail}</dd>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <dt className="text-muted-foreground">판정 근거</dt>
                          <dd>{item.detailSub || '—'}</dd>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <dt className="text-muted-foreground">실행 시각</dt>
                          <dd className="tabular-nums">
                            {item.ranAt ? formatRanAt(item.ranAt) : '—'}
                          </dd>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <dt className="text-muted-foreground">
                            {item.ranAt ? '소요 시간' : '예상 소요'}
                          </dt>
                          <dd className="tabular-nums">
                            {formatDuration(item.durationSec ?? 0)}
                          </dd>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <dt className="text-muted-foreground">상태</dt>
                          <dd>{CHECK_STATUS_TEXT[item.status]}</dd>
                        </div>
                      </dl>
                    </TableCell>
                  </TableRow>
                ) : null}
              </Fragment>
            );
          })}
        </TableBody>
      </Table>
      <p className="px-1 pt-2 text-muted-foreground text-[11px]">{caption}</p>
    </>
  );
}

/** 검증 개요 — 집계 4종은 서버 값 그대로 쓴다(화면이 다시 세지 않는다).
 *  sub 는 목업의 "4 / 4" 아래 "100%" 처럼 타일 안에 들어가는 보조값이다. */
function SummaryTile({
  label,
  value,
  sub,
  color,
}: {
  label: string;
  value: string;
  sub?: string;
  color?: string;
}) {
  return (
    <div
      className="flex flex-col items-center justify-center gap-1 rounded-lg border px-1 py-3"
      style={color ? { borderColor: `color-mix(in srgb, ${color} 35%, transparent)` } : undefined}
    >
      <span className="text-muted-foreground text-xs">{label}</span>
      <span className="font-bold text-2xl tabular-nums" style={color ? { color } : undefined}>
        {value}
      </span>
      {sub ? <span className="text-muted-foreground text-[11px] tabular-nums">{sub}</span> : null}
    </div>
  );
}

/** 실시간 로그 패널 — 자동 스크롤 / 다운로드 / 확대. */
function LogPanel({ log, target }: { log: ValidationLog[]; target: string }) {
  const [autoScroll, setAutoScroll] = useState(true);
  const [expanded, setExpanded] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = boxRef.current;
    if (!autoScroll || el === null || log.length === 0) return;
    el.scrollTop = el.scrollHeight;
  }, [autoScroll, log.length]);

  function download() {
    const text = log
      .map((line) => `${formatLogTime(line.at)}\t[${line.level}] ${line.text}`)
      .join('\n');
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `validation-${target.replace(/[^\w-]/g, '_')}-${new Date().toISOString().slice(0, 10)}.log`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  return (
    <Card className="gap-0 py-0">
      <CardContent className="flex flex-col gap-2 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-semibold text-sm">실시간 로그</span>
          <span className="flex items-center gap-1 text-muted-foreground text-xs">
            <span
              className="size-1.5 rounded-full"
              style={{ backgroundColor: 'var(--metric-pod)' }}
            />
            실시간
          </span>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <label className="flex cursor-pointer items-center gap-1.5 rounded-md border px-2 py-1 text-xs">
              <input
                type="checkbox"
                className="size-3.5 [accent-color:var(--metric-cpu)]"
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
              />
              자동 스크롤
            </label>
            <Button
              size="sm"
              variant="outline"
              className="h-8"
              onClick={download}
              disabled={log.length === 0}
            >
              <Download className="size-3.5" />
              로그 다운로드
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-8 px-2"
              aria-label={expanded ? '로그 축소' : '로그 확대'}
              onClick={() => setExpanded((v) => !v)}
            >
              <Maximize2 className="size-3.5" />
            </Button>
          </div>
        </div>
        <div
          ref={boxRef}
          className={cn(
            'overflow-auto rounded-lg border bg-muted/40 p-4 font-mono text-xs leading-7',
            expanded ? 'h-96' : 'h-48',
          )}
        >
          {log.length === 0 ? (
            <p className="py-8 text-center text-muted-foreground">
              검증을 실행하면 로그가 여기에 쌓인다.
            </p>
          ) : (
            log.map((line) => (
              <div key={`${line.at}-${line.text}`} className="flex gap-3 whitespace-pre-wrap">
                <span className="shrink-0 text-muted-foreground tabular-nums">
                  {formatLogTime(line.at)}
                </span>
                <span
                  className="shrink-0"
                  style={{
                    color:
                      line.level === 'ERROR'
                        ? 'var(--metric-fault)'
                        : line.level === 'WARN'
                          ? 'var(--metric-thermal)'
                          : undefined,
                  }}
                >
                  [{line.level}]
                </span>
                <span>{line.text}</span>
              </div>
            ))
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export default function GpuValidation() {
  const { demoMode, state, act, refresh } = useDemo();
  const [searchParams] = useSearchParams();

  // 폼 로컬 상태 = "입력 중" 값만(null = 서버 값 그대로 따른다). 저장 성공 시
  // null 로 되돌려, 화면 값이 서버 state 에서 다시 내려오는 것으로 반영을 확인한다.
  const [profileChoice, setProfileChoice] = useState<string | null>(null);
  const [durationChoice, setDurationChoice] = useState<number | null>(null);
  const [targetChoice, setTargetChoice] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);
  const [pollMs, setPollMs] = useState(DETAIL_POLL_ACTIVE_MS);

  // victim util 곡선 — uuid 미확정(판별 중/demo off)이면 질의 0.
  const victimUuid = state?.scenario.victim.uuid ?? '';
  const curveQuery = usePolledQuery(async () => {
    if (!victimUuid) return null;
    const endSec = Math.floor(Date.now() / 1000);
    const startSec = endSec - CURVE_RANGE_SECONDS;
    const step = Math.max(MIN_STEP_SECONDS, Math.floor(CURVE_RANGE_SECONDS / TARGET_POINTS));
    const res = await api.rangeQuery(
      selector(METRIC_GPU_UTIL, { gpu_uuid: victimUuid }),
      startSec,
      endSec,
      step,
    );
    return res.data;
  }, CURVE_POLL_MS, [victimUuid]);

  const curveRows = useMemo(() => {
    const series = curveQuery.data?.result[0];
    if (!series) return [];
    return series.values.map(([t, v]) => ({ time: t, util: Number(v) }));
  }, [curveQuery.data]);

  // 검증 대상 — ?uuid= 지정분이 우선(유휴장비 관리의 [재사용 검증 시작] 진입).
  //
  // 지정이 없으면(사이드바 기본 진입) 시나리오 victim 이지만, 교체 전 단계에는
  // victim 에 검증할 개체가 아직 없어 화면이 통째로 빈다(전 항목 대기 · 로그 0줄
  // · 버튼 2개 비활성). 그때는 재사용 검증 라인에서 지금 돌고 있는 자산을 대신
  // 연다 — 같은 화면을 ?uuid= 로 열었을 때와 동일한 장면이 나온다.
  //
  // 대신 여는 자산은 *처음 고른 것에 고정(pin)* 한다. 매 폴링마다 다시 고르면
  // 그 자산의 검증이 끝나는 순간(= 발표자가 결과를 읽는 바로 그 시점) 목록의
  // 다음 검증 중 자산으로 화면이 스스로 갈아탄다(라인 폭 6 · D=180s 라 30~45초
  // 마다 1회).
  //
  // 고정을 푸는 기준은 하나다: **볼 것이 남아 있는가**.
  //   · 진행 중(validating) · 통과(ready) · 실패(failed) = 볼 것이 있다 → 붙든다.
  //     **검증 완료는 해제 사유가 아니다** — 완료된 판정과 로그가 이 장면의
  //     결말이고, 그 상태에서 [사전검증 재실행] 이 열린다.
  //   · 유휴 목록 이탈(운영 풀 재투입 · 리셋 · 재시딩) 또는 검증 기록 만료
  //     (verdict=idle — 자동 착수분은 D+R=11분에 서버가 기록을 지운다) = 볼 것이
  //     없다 → 다음 대상으로 넘긴다. 만료를 해제 사유에 넣지 않으면 화면이 그
  //     자산에 붙박인 채 통째로 빈다(전 항목 대기 · 로그 0줄) — 이 폴백이 애초에
  //     막으려던 바로 그 상태다.
  const [pinnedUuid, setPinnedUuid] = useState('');
  const paramUuid = searchParams.get('uuid') ?? '';
  const victimHasRun = VICTIM_RUN_PHASES.includes(state?.scenario.phase ?? 'normal');
  const idleList = state?.idle;
  const pinned = pinnedUuid === '' ? undefined : idleList?.find((g) => g.uuid === pinnedUuid);
  // 실패분도 후보다 — 실패 판정·로그도 완결된 장면이고("통과시키지 않는 장면"이
  // 이 라인의 핵심), 이 줄이 없으면 라인에 완료분만 남은 순간 대상이 교체 노드로
  // 떨어져 교체 전 단계에서 화면이 빈다.
  const liveUuid =
    pinned && pinned.validation.verdict !== 'idle'
      ? pinnedUuid
      : (idleList?.find((g) => g.validation.verdict === 'validating')?.uuid ??
        idleList?.find((g) => g.validation.verdict === 'ready')?.uuid ??
        idleList?.find((g) => g.validation.verdict === 'failed')?.uuid ??
        '');
  const targetUuid = paramUuid || (victimHasRun ? victimUuid : liveUuid || victimUuid);
  const isVictim = targetUuid !== '' && targetUuid === victimUuid;

  useEffect(() => {
    if (liveUuid !== '' && liveUuid !== pinnedUuid) setPinnedUuid(liveUuid);
  }, [liveUuid, pinnedUuid]);

  // 대상이 바뀌면(고정 해제 · ?uuid= 이동 · victim 단계 진입) 이전 자산에 띄운
  // 배너를 지운다 — 남겨 두면 A 자산에 띄운 "번인 테스트를 시작했다" 가 B 자산
  // 화면 위에 그대로 남는다.
  useEffect(() => {
    setActionNotice(null);
    setActionError(null);
  }, [targetUuid]);

  // 재구독 신호는 대상 uuid + *단계* 다 — 교체 노드의 회차 창과 액션 게이트
  // 2종이 전부 단계 파생이라, 단계가 넘어간 순간 상세가 낡는다. 단계를 빼면
  // 유휴 폴링 주기(15s) 동안 [운영 풀 재투입] 이 뜨지 않는다.
  const detailQuery = usePolledQuery(async () => {
    if (!targetUuid) return null;
    try {
      return await fetchValidation(targetUuid);
    } catch (err) {
      const message = validationMessage(err);
      if (message === null) throw err; // 401 — 로그인 화면 전환이 처리한다
      throw new Error(message);
    }
  }, pollMs, [targetUuid, state?.scenario.phase]);

  const detail = detailQuery.data;
  const runActive = detail?.precheck.active === true || detail?.burnin.active === true;
  useEffect(() => {
    setPollMs(runActive ? DETAIL_POLL_ACTIVE_MS : DETAIL_POLL_IDLE_MS);
  }, [runActive]);

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
          <CardTitle>GPU 사전검증</CardTitle>
          <CardDescription>데모 시나리오 전용 화면</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-sm">
            이 화면은 데모 모드에서만 동작한다. 재사용 검증 라인(사전검증 → 번인
            테스트 → 가용성 판정 → 재투입)을 시연하며, 실서비스 모드에서는
            제공되지 않는다.
          </p>
        </CardContent>
      </Card>
    );
  }

  if (!state) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const phase = state.scenario.phase;
  const victim = state.scenario.victim;
  const burnin = victim.burnin;

  // 서버가 알려준 프로파일이 옵션 밖이면 옵션에 합류시킨다(표시 누락 방지).
  const profileOptions = BURNIN_PROFILES.some((p) => p.value === burnin.profile)
    ? [...BURNIN_PROFILES]
    : burnin.profile
      ? [{ value: burnin.profile, label: burnin.profile, simulated: false }, ...BURNIN_PROFILES]
      : [...BURNIN_PROFILES];
  const effectiveProfile = profileChoice ?? burnin.profile ?? profileOptions[0].value;
  // 초기값은 서버 설정값 — 사용자가 손대면 그 입력값이 우선한다.
  const effectiveDuration = durationChoice ?? (burnin.durationMin || 120);
  const effectiveTarget = targetChoice ?? (burnin.targetUtilPct || 95);

  const precheck = detail?.precheck;
  const reuseBurnin = detail?.burnin;
  const logLines = [...(precheck?.log ?? []), ...(reuseBurnin?.log ?? [])];

  /** 현재 폼 값을 서버에 저장한다. 성공 시 로컬 입력값을 해제해 다음 폴링의
   *  서버 값이 폼에 그대로 내려오는 것으로 반영을 확인한다. */
  async function saveBurninConfig(): Promise<boolean> {
    const outcome = await act('configure-burnin', {
      profile: effectiveProfile,
      durationMin: effectiveDuration,
      targetUtilPct: effectiveTarget,
    });
    if (outcome.ok) {
      setProfileChoice(null);
      setDurationChoice(null);
      setTargetChoice(null);
      setSaved(true);
      return true;
    }
    setSaved(false);
    setActionError(
      outcome.status === 409
        ? `번인 설정 거부 — ${outcome.error}`
        : `번인 설정 실패 (HTTP ${outcome.status}) — ${outcome.error}`,
    );
    return false;
  }

  async function onSaveProfile() {
    setBusy(true);
    setActionError(null);
    await saveBurninConfig();
    setBusy(false);
  }

  async function onStartBurnin() {
    setBusy(true);
    setActionError(null);
    // 한 번의 조작으로 설정 저장 + 실행 — 저장이 거부되면 시작하지 않는다.
    if (!(await saveBurninConfig())) {
      setBusy(false);
      return;
    }
    const outcome = await act('start-burnin');
    setBusy(false);
    // 거부 사유도 서버 문구 그대로다 — 화면이 사유를 지어내면 게이트 문구와
    // 배너 문구가 갈라진다(가드가 낸 reason 이 곧 버튼 아래 안내다).
    if (!outcome.ok) setActionError(outcome.error);
  }

  async function onReturnToService() {
    setBusy(true);
    setActionError(null);
    const outcome = await act('return-to-service');
    setBusy(false);
    if (!outcome.ok) setActionError(outcome.error);
  }

  /** 사전검증 재실행 / 번인 테스트 시작 — 서버 액션 그대로.
   *  성공 메시지도 버린 적이 없다: 착수 여부는 발표자가 화면에서 읽어야 한다. */
  async function runValidationAction(action: 'start-precheck' | 'start-reuse-burnin') {
    setBusy(true);
    setActionError(null);
    setActionNotice(null);
    const outcome = await act(action, { uuid: targetUuid });
    setBusy(false);
    if (outcome.ok) {
      setActionNotice(outcome.result.message);
      detailQuery.refresh();
      refresh();
      return;
    }
    setActionError(outcome.error);
  }

  return (
    <div className="flex flex-col gap-4">
      <Breadcrumb target={detail ? `${detail.instance} / ${detail.device}` : '—'} />

      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="font-bold text-3xl">GPU 사전검증</h1>
          {isVictim ? <Badge variant="outline">{PHASE_LABELS[phase]}</Badge> : null}
          {detail && detail.idleDays > 0 ? (
            <Badge variant="secondary">유휴 {detail.idleDays}일</Badge>
          ) : null}
        </div>
        <p className="text-muted-foreground text-sm">
          드라이버, CUDA, 네트워크, Kubernetes 구성 적합성을 검증합니다.
        </p>
      </div>

      {actionError ? (
        <Alert variant="destructive">
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}
      {actionNotice ? (
        <Alert>
          <Info className="size-4" />
          <AlertDescription>{actionNotice}</AlertDescription>
        </Alert>
      ) : null}
      {detailQuery.error ? (
        <p role="alert" className="text-destructive text-sm">
          {detailQuery.error}
        </p>
      ) : null}

      {/* 1. 재사용 4단계 스테퍼 — ReuseStepper 는 3개 화면 공용이라 손대지 않고,
          목업의 큰 번호 원(≈40px)만 이 화면에서 덧입힌다. 셀렉터가 빗나가면
          기본 크기로 되돌아갈 뿐이라 깨지지 않는다. */}
      <Card className="gap-0 py-0">
        <CardContent className="overflow-x-auto p-5 [&_span.size-8]:size-10 [&_span.size-8]:text-base">
          {detail ? (
            <ReuseStepper steps={detail.steps} />
          ) : (
            <Skeleton className="h-16 w-full" />
          )}
        </CardContent>
      </Card>

      {!detail ? (
        <Skeleton className="h-80 w-full" />
      ) : (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="flex flex-col gap-4">
            {/* 2. 자산 요약 3칸 */}
            <AssetSummary detail={detail} />

            {/* 3. 검증 항목 4행 */}
            <Card className="gap-0 py-0">
              <CardContent className="p-4">
                <h2 className="pb-3 font-semibold text-base">검증 항목</h2>
                {/* 각주 — 확장행은 접힌 행과 같은 값을 펼쳐 보여줄 뿐이라(새
                    정보는 서버 ValidationItem 스키마 확장이 선행돼야 한다)
                    "상세 결과"를 약속하지 않는 문구로 좁혔다. */}
                <ItemTable
                  run={detail.precheck}
                  caption="* 검증은 순차적으로 실행되며, 항목을 클릭하면 판정 근거를 펼쳐 볼 수 있습니다."
                />
              </CardContent>
            </Card>
          </div>

          {/* 4. 검증 개요 */}
          <Card className="gap-0 py-0">
            <CardContent className="flex flex-col gap-3 p-5">
              <h2 className="font-semibold text-lg">검증 개요</h2>
              <div className="grid grid-cols-4 gap-2">
                <SummaryTile
                  label="전체 항목"
                  value={`${detail.precheck.passed + detail.precheck.warned + detail.precheck.failed} / ${detail.precheck.total}`}
                  sub={`${Math.round(detail.precheck.progress * 100)}%`}
                />
                <SummaryTile
                  label="통과"
                  value={String(detail.precheck.passed)}
                  color="var(--metric-pod)"
                />
                <SummaryTile
                  label="주의"
                  value={String(detail.precheck.warned)}
                  color="var(--metric-thermal)"
                />
                <SummaryTile
                  label="실패"
                  value={String(detail.precheck.failed)}
                  color="var(--metric-fault)"
                />
              </div>

              {/* 예상 소요는 서버 계획(항목별 계획 소요 합)이다 — 화면이 숫자를
                  지어내면 실행 후 값과 어긋난다. 목업은 여기에 진행바를 두지
                  않는다(진행률은 위 "전체 항목" 타일이 이미 말한다). */}
              <div className="flex items-center justify-between rounded-lg border px-3 py-2.5">
                <span className="flex items-center gap-1.5 text-muted-foreground text-xs">
                  <Clock className="size-4" />
                  예상 소요 시간
                </span>
                <span className="flex flex-col items-end">
                  <span className="font-bold text-lg tabular-nums">
                    {plannedSeconds(detail.precheck)}초
                  </span>
                  <span className="text-[11px] text-muted-foreground">
                    데모 압축 — 실장비는 분 단위
                  </span>
                </span>
              </div>

              {/* 버튼 활성 조건 = 서버 게이트. 화면이 조건을 다시 유도하면 서버가
                  거부하는 상태에서 버튼이 살아 있어 시연 중 오류 배너가 뜬다. */}
              <Button
                variant="outline"
                className="h-11 border-2 font-semibold text-base"
                style={{ color: 'var(--metric-cpu)', borderColor: 'var(--metric-cpu)' }}
                onClick={() => runValidationAction('start-precheck')}
                disabled={busy || !detail.precheckGate.allowed}
              >
                <RefreshCw className="size-4" />
                사전검증 재실행
              </Button>
              <Button
                className="h-11 font-semibold text-base"
                style={{ backgroundColor: 'var(--metric-cpu)' }}
                onClick={() => runValidationAction('start-reuse-burnin')}
                disabled={busy || !detail.burninGate.allowed}
              >
                <Play className="size-4" />
                번인 테스트 시작
              </Button>
              <p className="flex items-start gap-1.5 text-muted-foreground text-xs">
                <Info className="mt-0.5 size-3.5 shrink-0" />
                {detail.burninGate.reason}
              </p>
              {/* 교체 노드는 두 버튼이 같은 사유로 잠기고 아래 번인 섹션이 이어
                  설명한다 — 같은 문장을 두 번 쓰지 않는다. */}
              {detail.precheckGate.allowed || isVictim ? null : (
                <p className="pl-5 text-muted-foreground text-xs">
                  사전검증 재실행 — {detail.precheckGate.reason}
                </p>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {/* 5. 실시간 로그 */}
      <LogPanel log={logLines} target={detail ? `${detail.instance}-${detail.device}` : 'gpu'} />

      {/* 6. 번인 테스트 (유휴 자산) — victim 은 아래 시나리오 섹션이 담당한다 */}
      {!isVictim && reuseBurnin && reuseBurnin.startedAt ? (
        <Card className="gap-0 py-0">
          <CardContent className="flex flex-col gap-3 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="font-semibold text-base">번인 테스트</h2>
              <Badge variant="outline">
                {reuseBurnin.active
                  ? `진행 중 ${Math.round(reuseBurnin.progress * 100)}%`
                  : reuseBurnin.verdict === 'fail'
                    ? '실패 — 재투입 불가'
                    : '통과 — 재투입 가능'}
              </Badge>
            </div>
            <Progress value={reuseBurnin.progress * 100} />
            <ItemTable
              run={reuseBurnin}
              caption="* 고부하 스트레스 항목 — 구성 검증이 잡지 못하는 결함을 부하에서 잡아낸다."
            />
          </CardContent>
        </Card>
      ) : null}

      {/* 7. 이하 교체 노드(victim) 번인 검증 — 런북 Step 5 */}
      {isVictim ? (
        <>
          <Card>
            <CardHeader>
              <CardTitle>검증 관문</CardTitle>
              <CardDescription>노드 교체 후 재투입 전 관문 상태</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <GateDiagram stages={gateStages(phase)} />
              <p className="text-muted-foreground text-xs">
                검증 전 운영 투입 원천 차단 — K8s Ready 를 맹신하지 않는다
              </p>
            </CardContent>
          </Card>

          <div className="grid gap-4 lg:grid-cols-2">
            {/* 번인 프로파일 폼 */}
            <Card>
              <CardHeader>
                <CardTitle>번인 프로파일</CardTitle>
                <CardDescription>고부하 스트레스 검증 실행 설정</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-4">
                <div className="flex flex-col gap-1.5">
                  <span className="text-muted-foreground text-xs">프로파일</span>
                  <Select
                    value={effectiveProfile}
                    onValueChange={(v) => {
                      setProfileChoice(v);
                      setSaved(false);
                    }}
                    disabled={burnin.active || busy}
                  >
                    <SelectTrigger className="w-full" aria-label="번인 프로파일">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {profileOptions.map((p) => (
                        <SelectItem key={p.value} value={p.value}>
                          <span className="flex items-center gap-2">
                            {p.label}
                            {p.simulated ? <Badge variant="secondary">시뮬레이션</Badge> : null}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <span className="text-muted-foreground text-xs">
                    지속시간 —{' '}
                    <span className="font-medium text-foreground">{effectiveDuration}분</span>
                  </span>
                  <Slider
                    min={30}
                    max={180}
                    step={5}
                    value={[effectiveDuration]}
                    onValueChange={(v) => {
                      setDurationChoice(v[0] ?? 120);
                      setSaved(false);
                    }}
                    disabled={burnin.active || busy}
                    aria-label="번인 지속시간(분)"
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <span className="text-muted-foreground text-xs">
                    목표 사용률 —{' '}
                    <span className="font-medium text-foreground">{effectiveTarget}%</span>
                  </span>
                  <Slider
                    min={80}
                    max={100}
                    step={1}
                    value={[effectiveTarget]}
                    onValueChange={(v) => {
                      setTargetChoice(v[0] ?? 95);
                      setSaved(false);
                    }}
                    disabled={burnin.active || busy}
                    aria-label="번인 목표 사용률(%)"
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <span className="text-muted-foreground text-xs">대상 (교체 노드 고정)</span>
                  <div className="rounded-md border bg-muted/40 px-3 py-2 font-mono text-sm">
                    {victim.instance} · {victim.device} · {victim.model}
                  </div>
                </div>

                {burnin.active ? (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-muted-foreground text-xs">
                      번인 진행 중 — {Math.round(burnin.progress)}%
                    </span>
                    <Progress value={burnin.progress} />
                    <span className="text-muted-foreground text-xs">
                      적용 중: 프로파일 {burnin.profile} · 목표 {burnin.targetUtilPct}% · 지속{' '}
                      {burnin.durationMin}분
                    </span>
                  </div>
                ) : null}

                <div className="flex flex-wrap items-center gap-2">
                  {/* configure-burnin 은 단계 가드가 없다(파라미터 범위만 본다).
                      진행 중 잠금은 서버 가드 추측이 아니라 화면의 명시 규칙 —
                      도는 창의 길이를 바꾸면 진행률이 튄다. */}
                  <Button variant="outline" onClick={onSaveProfile} disabled={busy || burnin.active}>
                    프로파일 저장
                  </Button>
                  {/* 재번인 활성 조건 = 서버 게이트. 화면이 burnin.active 로
                      다시 유도하면 ready-to-return · returned 에서 버튼이 살아
                      있는 채 409 를 받는다(상세 도착 전에는 잠근다). */}
                  <Button onClick={onStartBurnin} disabled={busy || !detail?.restartBurninGate.allowed}>
                    번인 시작
                  </Button>
                  {saved && !burnin.active ? (
                    <span className="text-xs" style={{ color: 'var(--metric-pod)' }}>
                      저장됨 — 프로파일 {burnin.profile} · 목표 {burnin.targetUtilPct}% · 지속{' '}
                      {burnin.durationMin}분
                    </span>
                  ) : null}
                </div>
                {detail && !detail.restartBurninGate.allowed ? (
                  <p className="flex items-start gap-1.5 text-muted-foreground text-xs">
                    <Info className="mt-0.5 size-3.5 shrink-0" />
                    번인 시작 — {detail.restartBurninGate.reason}
                  </p>
                ) : null}
              </CardContent>
            </Card>

            {/* 번인 체크리스트 7종 */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  번인 체크리스트
                  {burnin.attempt > 0 ? (
                    <Badge variant="outline">{burnin.attempt}차 시도</Badge>
                  ) : null}
                </CardTitle>
                <CardDescription>스트레스 항목별 판정</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {burnin.checklist.length === 0 ? (
                  <p className="py-6 text-center text-muted-foreground text-sm">
                    번인 시작 전 — 체크 항목이 준비되면 표시된다.
                  </p>
                ) : (
                  burnin.checklist.map((item: CheckItem) => (
                    <div
                      key={item.id}
                      className="flex items-center gap-2 rounded-md border px-3 py-2"
                    >
                      <StatusIcon status={item.status} />
                      <span className="text-sm">{item.label}</span>
                      <span className="ml-auto text-muted-foreground text-xs">
                        {CHECK_STATUS_TEXT[item.status]}
                      </span>
                    </div>
                  ))
                )}
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            {/* Health 게이지 + 판정 근거 */}
            <Card>
              <CardHeader>
                <CardTitle>Health 판정</CardTitle>
                <CardDescription>100점 만점 — 85점 이상 통과</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap items-center justify-center gap-6">
                <HealthGauge score={victim.health.score} verdict={burnin.verdict} />
                <div className="flex min-w-52 flex-1 flex-col gap-2">
                  <span className="text-muted-foreground text-xs">판정 근거 (감점 항목)</span>
                  {victim.health.deductions.length === 0 ? (
                    <p className="text-muted-foreground text-sm">감점 항목 없음</p>
                  ) : (
                    victim.health.deductions.map((d) => (
                      <div
                        key={d.code}
                        className="flex items-start justify-between gap-2 rounded-md border px-3 py-2"
                      >
                        <div className="flex flex-col">
                          <span className="text-sm">{d.message}</span>
                          <span className="font-mono text-muted-foreground text-xs">{d.code}</span>
                        </div>
                        <span
                          className="font-semibold text-sm tabular-nums"
                          style={{ color: 'var(--metric-fault)' }}
                        >
                          -{d.penalty}
                        </span>
                      </div>
                    ))
                  )}
                </div>
              </CardContent>
            </Card>

            {/* 번인 부하 곡선 */}
            <Card>
              <CardHeader>
                <CardTitle>번인 부하 곡선</CardTitle>
                <CardDescription>
                  {victim.instance} · {victim.device} 사용률 — 최근 30분 · 15초 갱신
                </CardDescription>
              </CardHeader>
              <CardContent>
                {curveQuery.data === null && curveQuery.error === null ? (
                  <Skeleton className="h-60 w-full" />
                ) : curveRows.length === 0 ? (
                  <p className="py-8 text-center text-muted-foreground text-sm">
                    표시할 데이터가 없다.
                  </p>
                ) : (
                  <ResponsiveContainer width="100%" height={240}>
                    <LineChart data={curveRows} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
                      <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                      <XAxis
                        dataKey="time"
                        type="number"
                        domain={['dataMin', 'dataMax']}
                        tickFormatter={formatTimeTick}
                        tick={{ fontSize: 12 }}
                        className="fill-muted-foreground"
                      />
                      <YAxis
                        domain={[0, 100]}
                        width={40}
                        tick={{ fontSize: 12 }}
                        className="fill-muted-foreground"
                      />
                      <Tooltip
                        labelFormatter={(t) => formatTimeTick(Number(t))}
                        formatter={(value) => [`${Number(value).toFixed(1)}%`, '사용률']}
                        contentStyle={{
                          backgroundColor: 'var(--popover)',
                          borderColor: 'var(--border)',
                          color: 'var(--popover-foreground)',
                          fontSize: 12,
                        }}
                      />
                      <ReferenceLine
                        y={burnin.targetUtilPct || 95}
                        stroke="var(--metric-thermal)"
                        strokeDasharray="4 4"
                        label={{
                          value: `목표 ${burnin.targetUtilPct || 95}%`,
                          position: 'insideTopRight',
                          fill: 'var(--muted-foreground)',
                          fontSize: 11,
                        }}
                      />
                      <Line
                        type="monotone"
                        dataKey="util"
                        name="사용률"
                        stroke="var(--metric-gpu)"
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
          </div>

          {/* 재투입 CTA — 노출 조건도 서버 게이트다(화면이 단계로 다시 유도하지
              않는다). 복귀 완료 안내(returned)는 액션이 아니라 상태 표시다. */}
          {detail?.returnGate.allowed ? (
            <Card style={{ borderColor: 'var(--metric-pod)' }}>
              <CardContent className="flex flex-col items-center gap-3 py-6">
                <p className="text-muted-foreground text-sm">
                  Health {Math.round(victim.health.score)}점 — 검증 관문 통과. 운영 풀 재투입이
                  가능하다.
                </p>
                <Button
                  size="lg"
                  className="h-12 w-full max-w-md text-base"
                  onClick={onReturnToService}
                  disabled={busy}
                >
                  운영 풀 재투입
                </Button>
              </CardContent>
            </Card>
          ) : null}
          {phase === 'returned' ? (
            <Alert>
              <CircleCheck className="size-4" style={{ color: 'var(--metric-pod)' }} />
              <AlertTitle>운영 풀 복귀 완료</AlertTitle>
              <AlertDescription>
                검증을 통과한 노드가 운영 풀에 재투입됐다 — Uncordon 완료.
              </AlertDescription>
            </Alert>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
