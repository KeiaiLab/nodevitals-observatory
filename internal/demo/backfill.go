package demo

import "log/slog"

// Backfill 은 부팅 시점의 과거 데이터를 채운다 — 콘솔이 뜨자마자 24h 추이
// 차트와 1h 상세가 비어 보이지 않게 하는 장치다.
//
// 2-pass 구조인 이유: tsdb head 는 시리즈별 시간 오름차순 append 를 전제한다.
// 집계 시리즈를 24h 백필한 뒤 per-GPU 창(1h)에서 emitFast(집계 포함)를 다시
// 돌리면 같은 집계 시리즈에 시간이 이어지는 순서로 append 돼야 하므로,
// pass 1 은 [now-agg, now-gpu) 까지만 집계를 쓰고 pass 2 가 [now-gpu, now] 를
// 전체(per-GPU + 집계) emit 으로 이어받는다.
func (e *Engine) Backfill(nowMS int64) error {
	const stepMS = int64(15_000)
	aggMS := e.cfg.BackfillAgg.Milliseconds()
	gpuMS := e.cfg.BackfillGPU.Milliseconds()
	if gpuMS > aggMS {
		aggMS = gpuMS
	}

	aggStart := nowMS - aggMS
	gpuStart := nowMS - gpuMS

	// pass 1 — 집계·서빙·SLO (장기 추이용, per-GPU 제외)
	total := (gpuStart - aggStart) / stepMS
	done := int64(0)
	for t := aggStart; t < gpuStart; t += stepMS {
		if err := e.emitDerived(t, e.computeAggOnly(t)); err != nil {
			return err
		}
		done++
		if total > 0 && done%(total/10+1) == 0 {
			slog.Info("데모 백필 진행(집계)", "pct", done*100/total)
		}
	}

	// pass 2 — 전체 emit (per-GPU + 노드 + 집계). 60s 경계에서 slow 세트 동반.
	total = (nowMS - gpuStart) / stepMS
	done = 0
	for t := gpuStart; t <= nowMS; t += stepMS {
		if err := e.emitFast(t); err != nil {
			return err
		}
		if (t/stepMS)%4 == 0 {
			if err := e.emitSlow(t); err != nil {
				return err
			}
		}
		done++
		if total > 0 && done%(total/10+1) == 0 {
			slog.Info("데모 백필 진행(per-GPU)", "pct", done*100/total)
		}
	}
	return nil
}
