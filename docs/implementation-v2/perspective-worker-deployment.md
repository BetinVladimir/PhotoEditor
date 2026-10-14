# Perspective CPU worker installation

Status: implementation added; tests, migrations and deployment NOT RUN. This document does not constitute photographic qualification or permission to enable an unqualified policy.

Enable the explicit worker installation with `PHOTO_PERSPECTIVE_ENABLED=true`, `PHOTO_PERSPECTIVE_CONFIG=/absolute/path/perspective.json` and `PHOTO_PERSPECTIVE_CONFIG_SHA=<SHA256 of exact file bytes>`. The default is disabled. CPU resource class/backend and the existing ICC registry and qualified object-storage environment are mandatory. Metal remains the later accelerator test target with the existing 10,000,000,000-byte cap; this perspective algorithm executes on CPU.

The operator file has exactly these keys:

```json
{
  "schema_version": 1,
  "runtime_lock": {},
  "runtime_sha": "<canonical runtime lock SHA256>",
  "pin": {},
  "policy": {},
  "max_pixels": 200000000
}
```

`runtime_lock` is a complete PerspectiveRuntimeLock with exact Python, machine, package versions and all component hashes from perspective_runtime_lock.component_paths. Empty objects above are explanatory placeholders, not valid configuration. `pin` uses the exact CPU qualification identity fields: policy_id, qualification_id, policy_sha, report, calibration, model_lock_sha, runtime_lock_sha, device_profile_sha, dataset_sha, issuer_id and backend=cpu. Both refs contain artifact_id and sha256, with distinct nonzero IDs. Runtime SHA must match the pin and installed runtime.

`policy` contains exactly the shared qualification policy keys, operation=optical.perspective, operation_version=1, backend=cpu, license_cleared=true. Identity fields must match the pin. Required checks and SHA arrays must be independently approved, unique and sorted before calculating policy_sha; fixture and calibration sets must not overlap. Original canonical report and qualified Calibration bytes remain mandatory at each admission. No default report, issuer, dataset, checks or evidence is fabricated.

The file must be a regular non-symlink file with one link and size 2..8MiB. Loader verifies its exact byte SHA, shape and runtime. Invalid installation stops startup. Factory additionally checks the SQL authority function exists; migrations 01026–01030 must be applied later through the normal migration process. Worker registration must match installed operations; CLI does not modify registrations, enable registry entries, import qualification or grant policy approval.

Per attempt, the dedicated SQL authority checks CPU worker/lease/fence/deadline/slot, Params SHA and declared closure, accepted project source/editor session, declared auto LineSegments and current imported qualification. Original report/calibration are loaded using exact version IDs, verified by size/SHA/canonical bytes, and SQL scope is re-read around object IO. Admission verifies report/policy/runtime/checks/license and measured resource peaks, then ICC. Backend producer/adopter and client remain required before this can serve a complete user workflow.

## Completed-task adopter

Set `PHOTO_ADOPTER_PERSPECTIVE_ENABLED=true` and supply `PHOTO_ADOPTER_PERSPECTIVE_CONFIG` / `PHOTO_ADOPTER_PERSPECTIVE_CONFIG_SHA` using the same operator configuration format as the worker. Set explicit positive safe-integer `PHOTO_ADOPTER_PERSPECTIVE_SCRATCH_MAX_BYTES` and `PHOTO_ADOPTER_PERSPECTIVE_RAM_MAX_BYTES`, an existing absolute real directory `PHOTO_ADOPTER_PERSPECTIVE_SCRATCH_ROOT`, and `PHOTO_ICC_ROOT`. These replay budgets include original/staged array materialization and independently prepared output; insufficient budgets reject adoption. No runtime/policy is inferred from a worker result.

The adopter re-reads completed-task SQL evidence, verifies original declared qualification bytes and ICC, independently prepares and compares pixel/alpha/mask output, then rechecks evidence before the dedicated SQL commit. At strength zero, exact original WorkingImage reuse is permitted only with pass metadata and a canonical NPY zero mask. Generic required output-image roles are waived only after that proof. Configuration is disabled by default; migrations and later end-to-end tests remain required.


### Review preview compiler (implementation, unverified)

`photo_worker.v2.perspective_derivative.compile_perspective_derivative` derives an existing `media.preview` plan from SHA-verified canonical perspective result/source/report/transform documents. It supports preview1600/preview2560, preserves alpha and delegates deployed sRGB profile hashing to the existing renderer. Its `authority_inputs` are provenance/retention roots, separate from the media pixel inputs, and must be checked by a dedicated durable scheduler/admission path. This helper does not grant producer authority, dispatch tasks, accept a correction or publish a URL. Scheduler/publication/receipt integration remains absent; no operator setting currently enables a completed perspective visual preview path. New tests have not been run.

### Original-source limits and detector draft recovery (authored, unverified)

Forward migrations 01136–01137 align original WorkingImage metadata at 1 MiB across source discovery, child locking, correction drafts and explicit-line publication. Draft envelopes are bounded at 5 MiB to accommodate embedded canonical source/lines strings; Params remain 64 KiB, LineSegments remain 1 MiB, NPY header allowances remain unchanged. This does not increase image pixel limits or GPU memory budgets.

Migration 01138 distinguishes a proven pre-admission expired detector draft. Current principal/source authorization remains mandatory. Only a cancelled, unsealed run without admission receipt, with a draft-expired event and no attempted/non-expiry tasks, receives `PERSPECTIVE_DETECTION_DRAFT_EXPIRED` (HTTP 410). Generic snapshot expiry and legacy admitted runs retain their existing failure behavior. The client repeats the original request/key before rotating a receipt-less attempt. A recovered admission is persisted and handed to editor polling immediately; generic errors never rotate the key.

These migrations have not been applied. The authored unit tests, current browser/native workflow and SQL concurrency behavior have not been executed. Detector registry remains disabled until the complete qualification/deployment process is performed.
