// ScoringFunnel — Step 3 "지능형 워크로드 스케줄링" 인터랙티브 배치 후보 계산기.
// Phase 3 비전 화면이지만 숫자는 전부 라이브 플릿(useFleet 셀)에서 계산한다 —
// 신규 API 호출·폴링 없이 부모가 넘긴 셀 스냅샷만 쓴다(계산은 버튼 클릭 시 1회).
import { useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';
import { Slider } from '@/components/ui/slider';
import type { FleetCell } from '@/hooks/useFleet';
import { formatCount, formatPct } from '@/lib/gpuMetrics';

// 워크로드 유형별 "여유 GPU" 판정 상한 — 학습은 전용 점유가 필요해 사실상
// 유휴만, 배치는 틈새 활용을 허용한다. 선택이 결과를 실제로 바꾸는 축이다.
export interface WorkloadKind {
  id: 'inference' | 'training' | 'batch';
  label: string;
  freeUtilMax: number;
  note: string;
}

const WORKLOADS: readonly WorkloadKind[] = [
  { id: 'inference', label: '추론', freeUtilMax: 40, note: '버스트 여유 확보' },
  { id: 'training', label: '학습', freeUtilMax: 10, note: '전용 점유 — 유휴만' },
  { id: 'batch', label: '배치', freeUtilMax: 60, note: '틈새 활용 허용' },
] as const;

const MIN_GPUS = 1;
const MAX_GPUS = 16;
const FUNNEL_WIDTHS = ['100%', '85%', '70%', '55%'] as const;

// 점수 가중치 — 여유도 60 / 동일 노드 집적도 25 / 풀 격리 15 (합 100).
const W_HEADROOM = 0.6;
const W_DENSITY = 0.25;
const W_ISOLATION = 0.15;

// 후보 막대 색 — Tailwind JIT 이 스캔할 수 있도록 리터럴 클래스로 둔다
// (Progress 는 indicator 스타일 prop 이 없어 arbitrary variant 로 칠한다).
const CANDIDATE_BAR = [
  'h-2 [&_[data-slot=progress-indicator]]:bg-[var(--metric-pod)]',
  'h-2 [&_[data-slot=progress-indicator]]:bg-[var(--metric-thermal)]',
] as const;

interface NodeCandidate {
  instance: string;
  cluster: string;
  pool: string;
  /** 요청 모델 중 여유 판정을 통과한 GPU 수 */
  freeCount: number;
  /** 요청 모델과 일치하는 이 노드의 전체 GPU 수 */
  matchCount: number;
  /** 여유 GPU 평균 사용률(%) */
  avgUtil: number;
  headroom: number;
  density: number;
  isolation: number;
  score: number;
}

interface FunnelCounts {
  total: number;
  modelMatch: number;
  freeNodes: number;
  afterExclusion: number;
}

interface PlanResult {
  model: string;
  need: number;
  workload: WorkloadKind;
  computedAt: number;
  funnel: FunnelCounts;
  candidates: NodeCandidate[];
}

/** 여유 GPU 중 가장 많은 풀을 노드 대표 풀로 삼는다(표시 전용). */
function dominantPool(list: FleetCell[]): string {
  const counts = new Map<string, number>();
  for (const c of list) counts.set(c.pool, (counts.get(c.pool) ?? 0) + 1);
  let best = '';
  let bestCount = -1;
  for (const [pool, n] of counts) {
    if (n > bestCount) {
      best = pool;
      bestCount = n;
    }
  }
  return best;
}

/** 배치 후보 계산 — 노드(instance) 단위로 묶어 요청 모델·여유 장수를 만족하는
 *  노드를 고르고, 장애·온도이상 GPU 를 하나라도 가진 노드는 통째로 제외한다. */
function computePlan(
  cells: FleetCell[],
  excludedUuids: ReadonlySet<string>,
  model: string,
  need: number,
  workload: WorkloadKind,
): PlanResult {
  const matching = cells.filter((c) => c.model === model);

  const byNode = new Map<string, FleetCell[]>();
  for (const c of matching) {
    const list = byNode.get(c.instance);
    if (list) list.push(c);
    else byNode.set(c.instance, [c]);
  }

  // 제외 노드는 모델 무관 전수에서 판정한다 — 같은 노드의 다른 GPU 가 장애여도
  // 그 노드는 배치 대상이 아니다.
  const excludedNodes = new Set<string>();
  for (const c of cells) {
    if (excludedUuids.has(c.uuid)) excludedNodes.add(c.instance);
  }

  let freeNodes = 0;
  const candidates: NodeCandidate[] = [];

  for (const [instance, list] of byNode) {
    const free = list.filter((c) => c.util < workload.freeUtilMax);
    if (free.length < need) continue;
    freeNodes += 1;
    if (excludedNodes.has(instance)) continue;

    const avgUtil = free.reduce((sum, c) => sum + c.util, 0) / free.length;
    const headroom = Math.max(0, Math.min(100, 100 - avgUtil));
    // 요청 장수를 단일 노드에서 전부 채우면 만점(여기 도달한 노드는 항상 충족).
    const density = Math.min(100, (free.length / need) * 100);
    const isolation = (free.filter((c) => c.pool === '').length / free.length) * 100;
    const score = W_HEADROOM * headroom + W_DENSITY * density + W_ISOLATION * isolation;

    candidates.push({
      instance,
      cluster: free[0]?.cluster ?? '',
      pool: dominantPool(free),
      freeCount: free.length,
      matchCount: list.length,
      avgUtil,
      headroom,
      density,
      isolation,
      score,
    });
  }

  candidates.sort((a, b) => b.score - a.score || a.instance.localeCompare(b.instance));

  return {
    model,
    need,
    workload,
    computedAt: Date.now(),
    funnel: {
      total: cells.length,
      modelMatch: matching.length,
      freeNodes,
      afterExclusion: candidates.length,
    },
    candidates: candidates.slice(0, 2),
  };
}

function FunnelBox({
  width,
  label,
  value,
  hint,
}: {
  width: string;
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div
      className="flex flex-col items-center gap-0.5 rounded-md border bg-muted/60 px-3 py-1.5 text-center"
      style={{ width }}
    >
      <span className="text-xs">
        {label} <span className="font-semibold tabular-nums">{value}</span>
      </span>
      {hint ? <span className="text-[10px] text-muted-foreground">{hint}</span> : null}
    </div>
  );
}

function CandidateRow({
  candidate,
  rank,
  need,
}: {
  candidate: NodeCandidate;
  rank: 0 | 1;
  need: number;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-sm">
          Candidate {rank === 0 ? 'A' : 'B'} — 매칭률{' '}
          <span className="font-semibold tabular-nums">{Math.round(candidate.score)}%</span>
        </span>
        <Badge variant={rank === 0 ? 'default' : 'outline'}>
          {rank === 0 ? '최적 배치안' : '대체 추천'}
        </Badge>
      </div>
      <Progress value={Math.round(candidate.score)} className={CANDIDATE_BAR[rank]} />
      <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-muted-foreground text-xs">
        <span className="font-mono text-foreground">{candidate.instance}</span>
        <span>클러스터 {candidate.cluster || '—'}</span>
        <span>
          여유 GPU {candidate.freeCount}장 (동일 모델 {candidate.matchCount}장) / 요청 {need}장
        </span>
        <span>평균 사용률 {formatPct(candidate.avgUtil)}</span>
        <span>풀 {candidate.pool || '미배정'}</span>
      </div>
      <p className="text-[10px] text-muted-foreground tabular-nums">
        점수 분해 — 여유도 {Math.round(candidate.headroom)} × 60% · 집적도{' '}
        {Math.round(candidate.density)} × 25% · 풀 격리 {Math.round(candidate.isolation)} × 15%
      </p>
    </div>
  );
}

export interface ScoringFunnelProps {
  /** useFleet 셀 스냅샷 — null 이면 아직 로딩 중이다. */
  cells: FleetCell[] | null;
  /** 요청 가능한 GPU 모델 목록(데모 fleet.models 또는 셀 파생). */
  models: string[];
  /** 장애·온도이상 GPU uuid — 이 uuid 를 가진 노드는 후보에서 제외한다. */
  excludedUuids: ReadonlySet<string>;
}

export default function ScoringFunnel({ cells, models, excludedUuids }: ScoringFunnelProps) {
  const [modelChoice, setModelChoice] = useState<string>('');
  const [need, setNeed] = useState<number>(8);
  const [workloadId, setWorkloadId] = useState<WorkloadKind['id']>('inference');
  const [plan, setPlan] = useState<PlanResult | null>(null);

  // 모델 목록은 비동기로 채워지므로 state 기본값 대신 파생값으로 첫 항목을 쓴다
  // (useEffect 동기화 불필요).
  const activeModel = modelChoice || models[0] || '';
  const workload = WORKLOADS.find((w) => w.id === workloadId) ?? WORKLOADS[0];
  const ready = cells !== null && cells.length > 0 && activeModel !== '';

  function onCalculate() {
    if (!cells || activeModel === '') return;
    setPlan(computePlan(cells, excludedUuids, activeModel, need, workload));
  }

  return (
    <Card>
      <CardHeader>
        <Badge variant="outline" className="w-fit">
          Phase 3 · 2027 비전
        </Badge>
        <CardTitle>지능형 워크로드 스케줄링</CardTitle>
        <CardDescription>
          요청 조건을 입력하면 현재 플릿에서 배치 후보 노드를 실제로 계산한다.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {/* 요청 폼 */}
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="scoring-model">GPU 모델</Label>
            <Select
              value={activeModel}
              onValueChange={setModelChoice}
              disabled={models.length === 0}
            >
              <SelectTrigger id="scoring-model" className="w-full">
                <SelectValue placeholder="모델 목록 불러오는 중…" />
              </SelectTrigger>
              <SelectContent>
                {models.map((m) => (
                  <SelectItem key={m} value={m}>
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="scoring-workload">워크로드 유형</Label>
            <Select
              value={workloadId}
              onValueChange={(v) => setWorkloadId(v as WorkloadKind['id'])}
            >
              <SelectTrigger id="scoring-workload" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {WORKLOADS.map((w) => (
                  <SelectItem key={w.id} value={w.id}>
                    {w.label} — {w.note}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5 sm:col-span-2">
            <Label htmlFor="scoring-need">
              필요 장수 <span className="font-semibold tabular-nums">{need}장</span>
            </Label>
            <Slider
              id="scoring-need"
              min={MIN_GPUS}
              max={MAX_GPUS}
              step={1}
              value={[need]}
              onValueChange={(v) => setNeed(v[0] ?? MIN_GPUS)}
              aria-label="필요 GPU 장수"
            />
            <div className="flex justify-between text-[10px] text-muted-foreground tabular-nums">
              <span>{MIN_GPUS}장</span>
              <span>{MAX_GPUS}장</span>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={onCalculate} disabled={!ready}>
            배치 후보 계산
          </Button>
          <span className="text-muted-foreground text-xs">
            {ready
              ? `여유 판정 기준 — 사용률 ${workload.freeUtilMax}% 미만 (${workload.label})`
              : '플릿 인벤토리를 불러오는 중이다.'}
          </span>
        </div>

        <Separator />

        {plan === null ? (
          <p className="py-6 text-center text-muted-foreground text-sm">
            요청을 입력하고 계산해 보세요.
          </p>
        ) : (
          <div className="flex flex-col gap-4">
            {/* 깔때기 — 계산 결과 숫자로 채운다 */}
            <div className="flex flex-col items-center gap-1.5">
              <FunnelBox
                width={FUNNEL_WIDTHS[0]}
                label="전체 GPU"
                value={`${formatCount(plan.funnel.total)}장`}
              />
              <FunnelBox
                width={FUNNEL_WIDTHS[1]}
                label="모델 일치"
                value={`${formatCount(plan.funnel.modelMatch)}장`}
                hint={plan.model}
              />
              <FunnelBox
                width={FUNNEL_WIDTHS[2]}
                label="여유 충족 노드"
                value={`${formatCount(plan.funnel.freeNodes)}개`}
                hint={`${plan.need}장 이상 · 사용률 ${plan.workload.freeUtilMax}% 미만`}
              />
              <FunnelBox
                width={FUNNEL_WIDTHS[3]}
                label="장애·격리 제외 후"
                value={`${formatCount(plan.funnel.afterExclusion)}개`}
                hint={`장애·온도이상 포함 노드 ${plan.funnel.freeNodes - plan.funnel.afterExclusion}개 제외`}
              />
            </div>

            <Separator />

            {/* 후보 */}
            {plan.candidates.length === 0 ? (
              <p className="py-4 text-center text-muted-foreground text-sm">
                조건을 만족하는 노드가 없다 — 필요 장수를 줄이거나 다른 모델·워크로드 유형을
                선택한다.
              </p>
            ) : (
              <div className="flex flex-col gap-4">
                {plan.candidates.map((c, index) => (
                  <CandidateRow
                    key={c.instance}
                    candidate={c}
                    rank={index === 0 ? 0 : 1}
                    need={plan.need}
                  />
                ))}
              </div>
            )}

            <p className="text-muted-foreground text-xs">
              요청 {plan.model} {plan.need}장 · {plan.workload.label} — 라이브 플릿 기준 ·{' '}
              {new Date(plan.computedAt).toLocaleTimeString('ko-KR')} 계산
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
