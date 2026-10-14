# Depth runtime installation

Status: implemented, NOT RUN. Unit tests exist but execution is deferred by the implementation-first plan.

A qualified preparation worker explicitly configures:

- `PHOTO_DEPTH_RUNTIME_ENABLED=true`
- `PHOTO_DEPTH_MODEL_LOCK_SHA`: independently approved installed model-lock hash
- `PHOTO_DEPTH_RUNTIME_LOCK_SHA`: approved hash from depth `measure_runtime()`
- `PHOTO_DEPTH_DEVICE_PROFILE_SHA`: approved actual device measurement hash

The worker must have resource class `gpu`, configured model root and model lock, and exactly the required `depth_anything_v2_small` model in the lock. The backend may be `cpu`, `mps` or `cuda`; GPU class is the existing queue/resource protocol for model operations, not an assertion that CPU backend uses VRAM. Current hardware verification is MPS with10GB budget; CUDA testing is deferred.

Startup measures installed model/runtime/device and refuses mismatch. Every pinned depth execution needs the installed verifier; passing expected identity strings in a task does not establish authority. The provider verifies weights, code/library fingerprints and device identity from trusted installed state. Qualification reports must be approved independently on the test corpus. Startup does not create reports, enable registry or register unsupported operations.

The depth source fingerprint includes the new authority and measurement code. Existing qualification hashes need recalculation and independent approval after implementation; do not copy a previous runtime SHA. Runtime measurement is not a performance/quality benchmark or proof of10GB peak use. Worker capacity policy and allocator limits must also be applied for the future hardware test phase.

Automatic preparation task admission is a separate outstanding implementation requirement. This configuration alone does not create depth tasks, publish source closure or produce editor results.
