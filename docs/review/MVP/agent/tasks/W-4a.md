---
id: W-4a
title: TaskDatabase._connect - env timeouts and TCP keepalive for VPN
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/database.py
  - workers/tests/test_database_connect_params.py (new)
context:
  - workers/photo_worker/runtime/database.py::TaskDatabase._connect
  - workers/photo_worker/runtime/database.py::TaskDatabase.ping
verify:
  - cd workers && python -m pytest tests/test_database_connect_params.py tests/test_heartbeat_chain_source_pins.py -q -p no:cacheprovider
---
## Goal
The DB connection crosses a VPN: dead peers must be detected in ~30 s instead of hanging, and timeouts must be configurable. Behaviour on a healthy network stays the same.

## Spec
Edit ONLY `TaskDatabase._connect` (lines of this method are outside the text range pinned by `test_heartbeat_chain_source_pins.py`; do not touch other functions). New body:
- helper at module level (above the class): `def _env_int(name, default, lo, hi)` reading `os.environ`, returning `default` if unset/invalid and clamping to `[lo, hi]` (add `import os` and `import sys` if missing).
- `connect_timeout = _env_int('WORKER_DB_CONNECT_TIMEOUT', 10, 1, 60)`; `statement_ms = _env_int('WORKER_DB_STATEMENT_TIMEOUT_MS', 15000, 1000, 120000)`.
- kwargs for `psycopg.connect`: `connect_timeout`, `keepalives=1`, `keepalives_idle=15`, `keepalives_interval=5`, `keepalives_count=3`, and only when `sys.platform.startswith('linux')`: `tcp_user_timeout=15000`; `options=f'-c role=photo_worker -c statement_timeout={statement_ms} -c lock_timeout=3000'`.
- Keep the `_require_role` / `connection.close()` handling exactly as today.

## Test
Monkeypatch `psycopg.connect` (in module `photo_worker.runtime.database`) to record kwargs and return a dummy object with `close()`; monkeypatch `TaskDatabase._require_role` to a no-op. Assert defaults (10, 15000, keepalive values), env overrides, clamping (`WORKER_DB_CONNECT_TIMEOUT=999` -> 60), invalid value falls back to default, and that `_require_role` failure closes the connection.
