# V28 reflection worker installation

`image.reflection` is unavailable by default. A GPU-class worker using the `cpu` or `cuda` backend may install it only with all of these operator-owned values:

The operator can verify the installed runtime lock offline with `photo-worker verify-reflection-runtime PATH EXPECTED_SHA cpu|cuda`. The command reports byte identity only (`qualification: not_measured`); it does not import a qualification or enable the operation.

- `PHOTO_MODEL_ROOT` and `PHOTO_MODEL_LOCK`: the existing canonical ModelLock installation, containing one approved `reflection_rgb` entry, one `.onnx` file, exact byte hashes, and `passed` evidence for the selected backend.
- `PHOTO_REFLECTION_ENABLED=true` and `PHOTO_REFLECTION_MODEL_LOCK_SHA`: the exact `lock_id` of that ModelLock.
- `PHOTO_REFLECTION_RUNTIME_LOCK` and `PHOTO_REFLECTION_RUNTIME_SHA`: an absolute path to a canonical, regular, single-link V28 runtime lock and its SHA-256. The lock binds CPython version, machine, CPU/CUDA backend, package versions, and the reflection source inventory. Any code or package change requires a new lock and qualification.

Startup verifies both locks, all model bytes, the selected ONNX provider and exact tensor interface before registering the operation. It also requires worker-only, `photo_fn_owner` SQL gates `v28_restoration_lease_ready` and `v28_restoration_qualification_ready`. Each task rechecks the exact lease and qualification before model I/O and before output handoff. ModelPool independently requires `approved_for_install`, a `passed` backend, and matching local model bytes. Missing configuration leaves the operation absent; inconsistent configuration fails startup.

The lock entries and SQL qualification must refer to real, licensed, externally tested model bytes and the same model/runtime/device policy. A shape-compatible ONNX file or a locally generated lock is not qualification. No such weights, report, or deployment evidence is included in the source repository. The operation is not claimed available until those external inputs, migrations, current-build tests, photographic corpus evaluation, and end-to-end acceptance are complete.
