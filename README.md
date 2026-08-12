<p align="center">
  <a href="https://keiailab.com"><b>keiailab</b></a>
</p>

# nodevitals-observatory

**Self-contained observability console for [nodevitals](https://github.com/KeiaiLab/nodevitals).**
Own time-series engine, own PromQL subset, own UI — no Prometheus, no VictoriaMetrics, no Grafana at runtime.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)
[![Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](go.mod)

## What it is

One binary and one Helm chart that scrape `nodevitals` agents, store the samples,
answer queries, raise alerts, and draw the screens. Install the agent and this
console and hardware observability is finished — there is no second stack to run.

## Why zero dependencies

`go.mod` has no `require` block, and that is a product decision rather than an
accident. The moment this console needs a Prometheus server, a VictoriaMetrics
cluster, or a Grafana instance to be useful, "one chart, one pod, done" stops
being true — and that claim is the entire reason to choose it over wiring three
exporters to a metrics stack.

The same rule applies to Go libraries: importing `prometheus/promql` would make
the query language exactly right, and would also drag in hundreds of transitive
modules. `make all` fails the build if a `require` line appears.

## What it is not

It is **not** a general-purpose metrics platform. It scrapes `nodevitals`, not
your applications. It does not replace VictoriaMetrics or kube-prometheus-stack
for cluster-wide monitoring — those collect from dozens of targets this console
never looks at. Reaching that scope is a separate, later question.

## Status

Rebuilt from scratch in August 2026. The previous prototype proved the pieces
(time-series storage, scraping, HTTP API, UI) but grew a demo engine that took
44% of the codebase and crowded out the product; this repository keeps the
proven design decisions and leaves that structure behind.

See [`docs/superpowers/specs/`](docs/superpowers/specs) for design specs and
[`docs/superpowers/plans/`](docs/superpowers/plans) for implementation plans.

## License

MIT
