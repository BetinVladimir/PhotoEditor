---
id: B-2a
title: Minimal Prometheus-text metrics registry (stdlib only)
area: backend
size: S
depends: []
edit:
  - backend/internal/metrics/metrics.go (new)
  - backend/internal/metrics/metrics_test.go (new)
context: []
verify:
  - cd backend && go test ./internal/metrics -count=1 -race
---
## Goal
No new Go dependency: `github.com/prometheus/client_golang` is not in `go.mod`. Provide just enough to expose counters, gauges and histograms in the Prometheus text format.

## Spec: package `metrics`
```go
type Registry struct{ ... }
var Default = NewRegistry()
func NewRegistry() *Registry
func (r *Registry) Counter(name, help string, labels ...string) *CounterVec
func (r *Registry) GaugeFunc(name, help string, fn func() float64)
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *HistogramVec
func (c *CounterVec) Inc(labelValues ...string)
func (c *CounterVec) Add(v float64, labelValues ...string)
func (h *HistogramVec) Observe(v float64, labelValues ...string)
func (r *Registry) Handler() http.Handler          // text/plain; version=0.0.4
```
Rules: metric/label names must match `[a-zA-Z_:][a-zA-Z0-9_:]*` else panic at registration; registering the same name twice returns the existing collector if the type and labels match, otherwise panics; label value count mismatch panics; label values are escaped (`\\`, `\"`, `\n`); output is deterministic (metrics sorted by name, series sorted by label string); histograms emit `_bucket{le="..."}` cumulative incl. `+Inf`, `_sum`, `_count`; all operations safe for concurrent use; cardinality guard: at most 2000 series per metric (extra series are dropped silently).

## Tests
Exposition text equality for a counter with labels, a gauge func, a histogram with 3 buckets; escaping; double registration; concurrent Inc from 20 goroutines (`-race`); series cap.
