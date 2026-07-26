# nodevitals-observatory

Self-contained observability console for [nodevitals](https://github.com/KeiaiLab/nodevitals) —
its own time-series engine, its own PromQL subset, its own UI. No Prometheus,
no VictoriaMetrics, no Grafana at runtime.

> **Status: M5 (React console) + GPU fleet demo mode.**
> Design spec: [nodevitals repo](https://github.com/KeiaiLab/nodevitals/blob/main/docs/superpowers/specs/2026-07-24-nodevitals-observatory-design.md)

## Console

Login / Overview / Map / Explorer, plus a **GPU Fleet** suite
(`/gpu` — fleet heatmap, health & silent-failure detection, approval-based
remediation, burn-in validation with health scoring, efficiency & utilization).
GPU pages read the real agent metrics (`nodevitals_hw_gpu_*`); scenario panels
appear only in demo mode.

## GPU fleet demo mode

`-demo` (or `OBSERVATORY_DEMO=1`) replaces scraping with a synthetic
multi-cloud GPU fleet (default 7,000 GPUs across 4 providers) plus a live
operations scenario: silent degradation → operator-approved isolation →
graceful drain → node replacement → burn-in validation (fail once at health 75,
pass at 96) → return to service. The scenario loops unattended (~35 min) and
reacts instantly to operator actions.

```bash
OBSERVATORY_DEMO=1 OBSERVATORY_DATA_DIR=$(mktemp -d) \
  go run ./cmd/observatory -listen :9210
# open http://localhost:9210 → /gpu
```

| env | default | meaning |
|---|---|---|
| `OBSERVATORY_DEMO` | `0` | enable demo mode (disables scraping) |
| `OBSERVATORY_DEMO_SEED` | `42` | deterministic fleet/uuid seed |
| `OBSERVATORY_DEMO_FLEET` | 4-CSP / 7,000 GPUs | `id:Display:count,...` |
| `OBSERVATORY_DEMO_TIMESCALE` | `1` | scenario speed multiplier (rehearsal) |
| `OBSERVATORY_DEMO_BACKFILL_AGG` | `24h` | aggregate series backfill window |
| `OBSERVATORY_DEMO_BACKFILL_GPU` | `1h` | per-GPU series backfill window |

Demo API (session-authenticated except `status`): `GET /api/v1/demo/status`,
`GET /api/v1/demo/state`,
`POST /api/v1/demo/actions/{approve-isolation|start-burnin|return-to-service|reset|register-idle-reason|report-false-positive}`.

Helm: `demo.enabled=true` on a **separate release** — demo data must never mix
into a production TSDB.

## Build

```bash
cd web && pnpm install && pnpm build   # embeds SPA into internal/webui/assets
go build ./cmd/observatory
make fmt vet test                      # -race gate
```

## License

[MIT](LICENSE)
