// FleetFilterBar — 전 화면 공통 검색 필터 7종(사용자 지시 2026-07-27):
// CSP | 리전 | 클러스터 | 테넌트 | GPU 모델 | 상태 | 심각도.
//
// csp/region/cluster/tenant/model 은 벽면 응답 라벨에서 옵션을 얻고, 상태·
// 심각도는 고정 어휘다(서버 상황판 key 와 동일 — 화면과 집계가 같은 말을 쓴다).
// Radix Select 는 빈 문자열 value 를 런타임에서 금지하므로 "전체"는 센티널로
// 표현하고 FleetFilter 에는 undefined 로 환원한다(applyFleetFilter 의
// falsy=전체 계약과 등가).
import { X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import type { FleetFilter, FleetOptions } from '@/hooks/useFleet';

export interface FleetFilterBarProps {
  options: FleetOptions;
  filter: FleetFilter;
  onChange: (next: FleetFilter) => void;
  /** 리전 id → 표시명(예: naver-kr1 → KR-1). 없으면 id 를 그대로 쓴다. */
  regionLabels?: Record<string, string>;
}

/** Radix SelectItem 은 value="" 금지 — "전체" 선택의 센티널 값. */
const ALL = '__all__';

const STATUS_OPTIONS = [
  { value: 'normal', label: '정상' },
  { value: 'degraded', label: '성능 저하' },
  { value: 'fault', label: '장애' },
  { value: 'isolated', label: '격리' },
  { value: 'recovering', label: '복구 중' },
] as const;

const SEVERITY_OPTIONS = [
  { value: 'critical', label: 'Critical' },
  { value: 'major', label: 'Major' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
] as const;

/** 워크로드 — 기본(전체)은 학습·추론을 같이 본다(사용자 지시 2026-07-28). */
const WORKLOAD_OPTIONS = [
  { value: 'training', label: '학습' },
  { value: 'inference', label: '추론' },
] as const;

interface FieldDef {
  key: keyof FleetFilter;
  /** "전체" 상태에서 트리거에 그대로 보이는 라벨 (예: "CSP 전체"). */
  allLabel: string;
  optionsKey?: keyof FleetOptions;
  fixed?: ReadonlyArray<{ value: string; label: string }>;
}

const FIELDS: readonly FieldDef[] = [
  { key: 'workload', allLabel: '학습·추론 모두', fixed: WORKLOAD_OPTIONS },
  { key: 'csp', allLabel: 'CSP 전체', optionsKey: 'csps' },
  { key: 'region', allLabel: '리전 전체', optionsKey: 'regions' },
  { key: 'cluster', allLabel: '클러스터 전체', optionsKey: 'clusters' },
  { key: 'tenant', allLabel: '테넌트 전체', optionsKey: 'tenants' },
  { key: 'model', allLabel: 'GPU 모델 전체', optionsKey: 'models' },
  { key: 'status', allLabel: '상태 전체', fixed: STATUS_OPTIONS },
  { key: 'severity', allLabel: '심각도 전체', fixed: SEVERITY_OPTIONS },
] as const;

export default function FleetFilterBar({
  options,
  filter,
  onChange,
  regionLabels,
}: FleetFilterBarProps) {
  const active = FIELDS.some((field) => Boolean(filter[field.key]));

  return (
    <div className="flex flex-wrap items-center gap-1.5" aria-label="공통 검색 필터">
      {FIELDS.map((field) => {
        const values: Array<{ value: string; label: string }> = field.fixed
          ? field.fixed.map((o) => ({ value: o.value, label: o.label }))
          : (options[field.optionsKey as keyof FleetOptions] as string[]).map((v) => ({
              value: v,
              label: field.key === 'region' ? (regionLabels?.[v] ?? v) : v,
            }));
        return (
          <Select
            key={field.key}
            value={filter[field.key] ?? ALL}
            onValueChange={(next) =>
              onChange({ ...filter, [field.key]: next === ALL ? undefined : next })
            }
          >
            <SelectTrigger size="sm" className="h-8 w-auto min-w-[7rem] text-xs">
              <SelectValue placeholder={field.allLabel} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL} className="text-xs">
                {field.allLabel}
              </SelectItem>
              {values.map((opt) => (
                <SelectItem key={opt.value} value={opt.value} className="text-xs">
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        );
      })}
      {active ? (
        <Button variant="ghost" size="sm" className="h-8 px-2 text-xs" onClick={() => onChange({})}>
          <X className="size-3.5" />
          초기화
        </Button>
      ) : null}
    </div>
  );
}
