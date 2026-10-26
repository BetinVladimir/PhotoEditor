---
id: W-3b
title: Durable commit spool module (runtime/commit_spool.py)
area: worker
size: S
depends: []
edit:
  - workers/photo_worker/runtime/commit_spool.py (new)
  - workers/tests/test_commit_spool.py (new)
context:
  - workers/photo_worker/runtime/database.py::Lease
  - workers/photo_worker/contracts/wire.py::TaskError
  - workers/photo_worker/contracts/wire.py::TaskMetrics
verify:
  - cd workers && python -m pytest tests/test_commit_spool.py -q -p no:cacheprovider
---
## Goal
A finished GPU result must survive a worker restart or a long VPN outage. Before the terminal commit the worker writes a small spool file; it is removed only after SQL answered `committed|replayed|stale`. This card is only the storage module; wiring is in W-3c/W-3d.

## Spec: `workers/photo_worker/runtime/commit_spool.py`
```python
class CommitSpool:
    def __init__(self, root: Path, max_age_seconds: float = 86400, clock=time.time): ...
    def write(self, lease, kind: str, *, payload: bytes | None = None,
              failure=None, metrics=None) -> Path: ...
    def entries(self) -> list[tuple[Path, dict]]: ...   # valid, non-expired, oldest first
    def remove(self, path: Path) -> None: ...
def lease_to_record(lease) -> dict: ...
def record_to_lease(rec: dict): ...
```
Rules:
- `root` is created with mode 0o700. Files are named `<task_id>.<attempt>.json`, written atomically (write to `*.tmp` in the same dir, `flush`, `os.fsync`, `os.replace`), mode 0o600. Writing the same task/attempt again overwrites.
- `kind` is `'success'` (needs `payload`, stored as base64 text) or `'failure'` (needs `failure` and `metrics`, stored with `model_dump(mode='json')`; for `metrics` too). Anything else -> `ValueError`.
- Record layout: `{"v":1,"kind":..,"written_at":clock(),"lease":lease_to_record(lease), ...}`.
- `lease_to_record`: `{"command": lease.command.model_dump_json(), "worker_id": str(lease.worker_id), "fence": lease.fence, "until": lease.until.isoformat(), "deadline": lease.deadline.isoformat(), "execution_profile": lease.execution_profile, "input_digest": lease.input_digest}`.
- `record_to_lease`: import `Lease` from `.database` and `TaskCommand` from `photo_worker.contracts.wire` lazily inside the function; `TaskCommand.model_validate_json(rec["command"])`, `UUID(...)`, `datetime.fromisoformat(...)`.
- `entries()` skips and deletes corrupt files (invalid JSON, wrong `v`) and files older than `max_age_seconds` (SQL treats them as stale anyway); returns `(path, record)` sorted by `written_at`.
- No logging of payload or lease contents.

## Tests (`test_commit_spool.py`, use `tmp_path`, no DB)
Use a fake lease: `SimpleNamespace(command=SimpleNamespace(task_id=uuid4(), attempt=1, model_dump_json=lambda: '{"k":1}'), worker_id=uuid4(), fence=3, until=aware datetime, deadline=aware datetime, execution_profile={}, input_digest='d')`.
1. write+entries roundtrip for success (payload bytes preserved after base64).
2. failure record keeps error/metrics dicts (use plain `SimpleNamespace(model_dump=lambda mode: {...})`).
3. directory mode is 0o700 and file mode 0o600.
4. corrupt file and expired file (use injected `clock`) are removed by `entries()`.
5. `remove` is idempotent.
6. invalid `kind` raises `ValueError`.
