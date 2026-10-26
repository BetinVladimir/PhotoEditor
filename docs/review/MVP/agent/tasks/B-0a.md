---
id: B-0a
title: Config tests for TRUSTED_PROXY_CIDRS
area: backend
size: XS
depends: []
edit:
  - backend/internal/config/config_test.go
context:
  - backend/internal/config/config_test.go:1-12
  - backend/internal/config/config.go::Load
verify:
  - cd backend && go test ./internal/config -count=1
---
## Goal
`Config.TrustedProxyCIDRs` is parsed from the env var `TRUSTED_PROXY_CIDRS` (comma separated) and validated in `Load`. Add a test.

## Spec
Append `TestTrustedProxyCIDRs` to `config_test.go` (reuse the style of `TestOriginConfiguration`; development environment is enough: set `DATABASE_URL=postgres://local/test`, `APP_ENV=development`):
1. unset -> `cfg.TrustedProxyCIDRs` is empty;
2. `"10.0.0.0/8, 172.16.0.0/12 ,"` -> exactly `["10.0.0.0/8","172.16.0.0/12"]` (spaces and empty items ignored);
3. `"10.0.0.0/8,not-a-cidr"` -> `Load()` returns an error;
4. IPv6 `"fd00::/8"` accepted.
