// FleetFilterBar — 플릿 히트맵의 CSP/클러스터/풀/테넌트 4-Select + 초기화.
// 옵션은 useFleet().options(벽면 응답 라벨 유래)를 그대로 받는다. Radix Select
// 는 빈 문자열 value 아이템을 런타임에서 금지하므로 "전체"는 센티널 값으로
// 표현하고 FleetFilter 에는 undefined 로 환원한다(applyFleetFilter 의
// falsy=전체 계약과 등가).
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
}

/** Radix SelectItem 은 value="" 금지 — "전체" 선택의 센티널 값. */
const ALL = '__all__';

interface FieldDef {
  key: keyof FleetFilter;
  /** "전체" 상태에서 트리거에 그대로 보이는 라벨 (예: "CSP 전체"). */
  allLabel: string;
  optionsKey: keyof FleetOptions;
}

const FIELDS: readonly FieldDef[] = [
  { key: 'csp', allLabel: 'CSP 전체', optionsKey: 'csps' },
  { key: 'cluster', allLabel: '클러스터 전체', optionsKey: 'clusters' },
  { key: 'pool', allLabel: '풀 전체', optionsKey: 'pools' },
  { key: 'tenant', allLabel: '테넌트 전체', optionsKey: 'tenants' },
] as const;

export default function FleetFilterBar({ options, filter, onChange }: FleetFilterBarProps) {
  const active = FIELDS.some((field) => Boolean(filter[field.key]));

  return (
    <div className="flex flex-wrap items-center gap-2">
      {FIELDS.map((field) => (
        <Select
          key={field.key}
          value={filter[field.key] ?? ALL}
          onValueChange={(value) => {
            const next: FleetFilter = { ...filter };
            next[field.key] = value === ALL ? undefined : value;
            onChange(next);
          }}
        >
          <SelectTrigger size="sm" className="min-w-32" aria-label={field.allLabel}>
            <SelectValue placeholder={field.allLabel} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{field.allLabel}</SelectItem>
            {options[field.optionsKey].map((opt) => (
              <SelectItem key={opt} value={opt}>
                {opt}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ))}
      <Button variant="ghost" size="sm" disabled={!active} onClick={() => onChange({})}>
        초기화
      </Button>
    </div>
  );
}
