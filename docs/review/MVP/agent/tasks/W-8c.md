---
id: W-8c
title: Worker identity: runbook for enrolment, rotation and revocation
area: ops
size: XS
depends: [W-8a, W-8b]
edit:
  - docs/ops/worker-identity.md (new)
context: []
verify:
  - test -s docs/ops/worker-identity.md
  - grep -q "urn:photo:worker" docs/ops/worker-identity.md
---
## Goal
Short operator runbook (<= 70 lines, English).
## Content
1. Enrol: run `deploy/framevela/issue_worker_cert.py`, copy the three files to the GPU host over the VPN (`scp` shown with placeholders), set `PHOTO_WORKER_CERT`, `PHOTO_WORKER_KEY` env names exactly as used by `WorkerConfig.from_env` (read it, do not guess) and `PHOTO_WORKER_ID` equal to the UUID in the certificate.
2. Rotate: issue a new certificate before expiry (90 days; calendar reminder at day 60), restart the worker.
3. Revoke: append the worker UUID to the revocations file read by `workerapi.FileRevocations` (path as configured in W-7h), effective within seconds, then also remove the host from the VPN peer list.
4. Verify: `curl --cert ... https://<vpn-address>:8444/health/ready`; a revoked worker must get 401.
5. What the legacy shared certificate still allows (object gateway only) and the date by which it must be retired.
## Do not
- Do not invent environment variable names: copy them from `WorkerConfig.from_env`.
