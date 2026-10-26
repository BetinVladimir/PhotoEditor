---
id: B-1a
title: Rate limiter core (token bucket, in memory)
area: backend
size: S
depends: []
edit:
  - backend/internal/httpapi/ratelimit.go (new)
  - backend/internal/httpapi/ratelimit_test.go (new)
context:
  - backend/internal/httpapi/clientip.go
verify:
  - cd backend && go test ./internal/httpapi -run 'TestLimiter' -count=1
---
## Goal
A tiny, dependency-free limiter that can later be replaced by a shared store.

## Spec: `ratelimit.go` (package `httpapi`)
```go
type Rule struct {
	Burst int           // bucket size
	Per   time.Duration // time to refill the whole bucket
}
type RateLimiter interface {
	// Allow consumes one token for key. When denied, retryAfter is how long until a token is available.
	Allow(key string, rule Rule, now time.Time) (ok bool, retryAfter time.Duration)
}
func NewMemoryLimiter() *MemoryLimiter
```
`MemoryLimiter`: `sync.Mutex` + `map[string]*bucket{tokens float64; last time.Time}`. Refill rate = `Burst / Per` tokens per second, capped at `Burst`. New key starts full. Rule with `Burst < 1` or `Per <= 0` always allows. Lazy cleanup: every 1024th call, delete buckets whose `last` is older than 15 minutes. Must be safe for concurrent use.

Default rules (exported var so tests/ops can read it):
```go
var DefaultRules = map[string]Rule{
	"portal_session":   {Burst: 10, Per: time.Minute},
	"portal_download":  {Burst: 30, Per: time.Minute},
	"portal_upload":    {Burst: 120, Per: time.Minute},
	"auth_bootstrap":   {Burst: 30, Per: time.Hour},
}
```

## Tests (`TestLimiter...`, injected `now`, no sleeping)
1. burst allowed then denied, `retryAfter` > 0 and <= Per/Burst rounded up;
2. refill after the retry time passes;
3. different keys are independent;
4. concurrent use: 50 goroutines x 10 calls on one key with Burst 100 -> exactly 100 allowed (run with `-race` friendly code);
5. Rule{} (zero) always allows;
6. cleanup removes idle buckets (expose a small unexported `len()` helper for the test).
