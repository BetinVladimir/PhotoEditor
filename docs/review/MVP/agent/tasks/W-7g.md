---
id: W-7g
title: Worker RPC: select the control plane by configuration (SQL stays the default)
area: worker
size: S
depends: [W-7f, W-9m08]
edit:
  - workers/photo_worker/runtime/service.py
  - workers/tests/test_worker_control_mode.py (new)
context:
  - workers/photo_worker/runtime/service.py::WorkerConfig
  - workers/photo_worker/runtime/service.py:600-612
verify:
  - cd workers && python -m pytest tests/test_worker_control_mode.py tests/test_service_lazy_imports.py -q -p no:cacheprovider
---
## Goal
Let an operator point a worker at the HTTP control plane with one environment variable, without changing the default behaviour.
## Spec
- `WorkerConfig` gets an optional field `control_url: str | None = None` filled from `PHOTO_WORKER_CONTROL_URL` in `WorkerConfig.from_env` (read how the neighbouring fields are parsed; when set it must start with `https://`, otherwise `WorkerConfigurationError('PHOTO_WORKER_CONTROL_URL must be https')`; `PHOTO_WORKER_DATABASE_URL` is then NOT required).
- Where the database object is created (`database=TaskDatabase(config.database_url)` inside `_open_database_and_check_readiness`, or the equivalent line after the W-9 extraction), create `RpcTaskDatabase(config.control_url, client_tls(config.ca, config.certificate, config.private_key))` when `config.control_url` is set, else keep `TaskDatabase`. Import `RpcTaskDatabase` lazily inside the branch (`test_service_lazy_imports` must stay green).
- Startup readiness checks that raise `NotImplementedError('RPC_NOT_SUPPORTED')` in RPC mode: skip them ONLY when `control_url` is set and log one line `control plane mode: database readiness checks are enforced by the server`. List in the test which checks are skipped (`task_result_fence_ready`, `media_renderer_policy_ready`, the `*_runtime_ready` family are plain `_query` wrappers and keep working).
- Test: build config with and without `control_url` (use the existing config fixtures in neighbouring tests) and assert the class of the created database object (monkeypatch both constructors, never connect).
## Do not
- Do not remove the SQL path. Do not change defaults.
