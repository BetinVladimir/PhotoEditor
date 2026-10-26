---
id: W-5a
title: Deterministic Idempotency-Key for output uploads
area: worker
size: XS
depends: []
edit:
  - workers/photo_worker/runtime/gateway.py
  - workers/tests/test_gateway_upload_key.py (new)
context:
  - workers/photo_worker/runtime/gateway.py:176-206
verify:
  - cd workers && python -m pytest tests/test_gateway_upload_key.py -q -p no:cacheprovider
---
## Goal
A retried upload of the same file by the same task attempt must be recognised by the server as the same request. Today `upload()` sends `'Idempotency-Key': str(uuid4())`.

## Spec
1. Add a module-level pure function in `gateway.py`:
```python
def upload_idempotency_key(task_id, attempt: int, role: str, sha256_hex: str) -> str:
    import hashlib
    raw = f'{task_id}|{attempt}|{role}|{sha256_hex}'.encode()
    return hashlib.sha256(raw).hexdigest()[:48]
```
2. In `Gateway.upload` replace `str(uuid4())` with `upload_idempotency_key(self._lease.command.task_id, self._lease.command.attempt, role, sha)`. Keep the `uuid4` import if it is used elsewhere; otherwise remove the unused import.
3. Do not change anything else (the server side is a separate card).

## Test
Pure-function test only: same inputs -> same key; changing any one of task_id, attempt, role, sha changes the key; key matches `^[0-9a-f]{48}$`.
