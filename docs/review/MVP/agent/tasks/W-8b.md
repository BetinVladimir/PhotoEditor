---
id: W-8b
title: Worker identity: issue per-worker certificates (provisioning tool)
area: deploy
size: S
depends: []
edit:
  - deploy/framevela/issue_worker_cert.py (new)
  - deploy/framevela/test_issue_worker_cert.py (new)
context:
  - deploy/framevela/provision.py:36-60
verify:
  - python3 -m pytest deploy/framevela/test_issue_worker_cert.py -q -p no:cacheprovider
---
## Goal
Replace the single shared worker certificate by one certificate per worker, issued from the existing internal CA, valid for 90 days.
## Spec (`issue_worker_cert.py`, stdlib + the `openssl` CLI like `provision.py`)
- `python3 issue_worker_cert.py --ca-dir DIR --out-dir DIR --worker-id <uuid> [--days 90]`; `--worker-id` defaults to a fresh `uuid4`. Reads `DIR/ca.crt` and `DIR/ca.key`; writes `<out>/worker-<uuid>.key` (mode 0600), `.csr`, `.crt` with: subject `/CN=worker-<uuid>`, `subjectAltName=URI:urn:photo:worker:<uuid>`, `extendedKeyUsage=clientAuth`, `basicConstraints=CA:FALSE`, `keyUsage=digitalSignature`. Prints ONLY the worker id and the paths (never key material).
- Refuses (exit 2) to overwrite existing files and to run when `ca.key` is group/world readable.
- Test with a temporary CA created by the test through `openssl`: issue, then run `openssl x509 -text` and assert the URI SAN, clientAuth, validity ~ 90 days, and `openssl verify -CAfile` succeeds; assert the second run exits 2.
## Do not
- Do not modify `provision.py` or any existing credential. No private key on stdout.
