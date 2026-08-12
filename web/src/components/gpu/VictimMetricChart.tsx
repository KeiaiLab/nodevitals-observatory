// VictimMetricChart — demo3 GPU Assistant 의 "주요 지표" 미니차트 한 칸.
// 상단 라벨 + 대형 현재값(지표 색) + 하단 영역 추이. victim GPU 한 장의 온도·
// ECC·사용률을 useVictimSeries 가 공급한다. 색은 var(--metric-*) 직접 참조
// (Recharts 원시 컴포넌트 — GpuServing 의 CSP-안전 셋업 미러).
import { Area, AreaChart, ReferenceLine, ResponsiveContainer, XAxis, YAxis } from 'recharts';
import { Skeleton } from '@/components/ui/skeleton';

export interface VictimMetricChartProps {
  label: string;
  /** 대형 현재값(이미 포맷된 문자열, 예: "82°C"). null → Skeleton. */
  valueText: string | null;
  /** 지표 색 — CSS 변수(예: 'var(--metric-thermal)'). */
  color: string;
  values: number[];
  /** Y축 상한 고정(사용률 100 등). 생략하면 데이터 최대에 맞춘다. */
  domainMax?: number;
  /** 표본 시각(epoch 초, values 와 같은 길이). 주면 x축이 시각 눈금이 된다.
   *  없으면 현행대로 인덱스 축(눈금 없음). */
  times?: number[];
  /** 장애 시점(epoch 초) — 빨간 점선 마커. times 가 있어야 위치가 정해진다.
   *  창(30분) 밖이면 Recharts 가 그리지 않는다(도메인 밖). */
  markerAt?: number;
}

/** "82.3°C" → ["82.3", "°C"] / "128 count" → ["128", "count"]. 앞의 수치 덩어리만
 *  떼어낸다 — 수치가 없으면 통째로 앞칸에 둔다(단위만 있는 값은 없다). */
function splitUnit(text: string): [string, string] {
  const m = text.match(/^([\d.,+-]+)\s*(.*)$/);
  return m ? [m[1], m[2]] : [text, ''];
}

/** epoch 초 → "14:20". x축 눈금은 분까지면 충분하다. */
function hhmm(sec: number): string {
  const d = new Date(sec * 1000);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

export default function VictimMetricChart({
  label,
  valueText,
  color,
  values,
  domainMax,
  times,
  markerAt,
}: VictimMetricChartProps) {
  const timed = times !== undefined && times.length === values.length;
  const rows = values.map((v, i) => ({ i: timed ? times[i] : i, v }));
  return (
    <div className="flex flex-col gap-1 rounded-md border bg-card p-3">
      <span className="text-muted-foreground text-xs">{label}</span>
      {valueText === null ? (
        <Skeleton className="h-7 w-16" />
      ) : (
        // 목업은 "128 count" 를 숫자만 크게(지표 색) 두고 단위는 작은 회색으로 흘린다 —
        // 단위까지 24px 로 키우면 세 칸이 서로 다른 폭으로 요란해진다.
        <span className="flex items-baseline gap-1">
          <span className="font-semibold text-2xl leading-tight tabular-nums" style={{ color }}>
            {splitUnit(valueText)[0]}
          </span>
          {splitUnit(valueText)[1] ? (
            <span className="text-muted-foreground text-xs">{splitUnit(valueText)[1]}</span>
          ) : null}
        </span>
      )}
      <div className="mt-1 h-24">
        {rows.length < 2 ? (
          <div className="flex h-full items-center justify-center text-muted-foreground text-xs">
            데이터 수집 중…
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={rows} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
              <YAxis
                domain={[0, domainMax ?? 'dataMax']}
                width={30}
                tick={{ fontSize: 10 }}
                className="fill-muted-foreground"
                axisLine={false}
                tickLine={false}
              />
              {timed ? (
                <XAxis
                  dataKey="i"
                  type="number"
                  domain={['dataMin', 'dataMax']}
                  tickFormatter={hhmm}
                  height={16}
                  tick={{ fontSize: 10 }}
                  className="fill-muted-foreground"
                  axisLine={false}
                  tickLine={false}
                />
              ) : null}
              {timed && markerAt ? (
                <ReferenceLine
                  x={markerAt}
                  stroke="var(--metric-fault)"
                  strokeDasharray="4 4"
                  ifOverflow="hidden"
                />
              ) : null}
              <Area
                type="monotone"
                dataKey="v"
                stroke={color}
                strokeWidth={2}
                fill={color}
                fillOpacity={0.12}
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}
