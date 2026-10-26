---
id: W-8a
title: Worker identity: per-worker certificate identity extraction (Go)
area: backend
size: S
depends: [W-7d]
edit:
  - backend/internal/workerapi/identity.go (new)
  - backend/internal/workerapi/identity_test.go (new)
context: []
verify:
  - cd backend && go test ./internal/workerapi -run TestIdentity -count=1
  - cd backend && go vet ./internal/workerapi
---
## Goal
A worker proves who it is by its client certificate. Identity = a URI SAN `urn:photo:worker:<uuid>`.
## Spec
- `func IdentityFromTLS(r *http.Request) (uuid.UUID, bool)`: requires `r.TLS != nil`, `len(r.TLS.VerifiedChains) > 0`, takes `VerifiedChains[0][0]` (the leaf), scans `leaf.URIs` for scheme `urn` and opaque `photo:worker:<uuid>`; exactly one such URI and a non-nil, canonical lowercase UUID, else `(uuid.Nil, false)`. The legacy shared certificate (CN `framevela-worker`, no such URI) is NOT accepted here.
- `type Revocations interface{ Revoked(id uuid.UUID) bool }` and `func WithRevocations(identity WorkerIdentity, rev Revocations) WorkerIdentity` returning ok=false for a revoked id.
- `type FileRevocations` loading a text file (one UUID per line, `#` comments) and re-reading it when the mtime changes (check at most once per second); a missing file means "nobody revoked"; unparsable lines are skipped and counted in an exported `SkippedLines()` for tests.
- Tests: build leaf certificates in the test with `crypto/x509` (self-signed CA + leaf with `URIs`), call through a fake `*http.Request` with `TLS: &tls.ConnectionState{VerifiedChains: ...}`; cases: valid, no URI, two URIs, uppercase UUID, nil UUID, no verified chain, revoked, revocation file edited between calls.
## Do not
- No network access. Do not trust `r.TLS.PeerCertificates` (unverified).
