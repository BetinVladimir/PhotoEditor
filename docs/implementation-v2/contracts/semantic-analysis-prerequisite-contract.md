# Semantic scene/depth prerequisite compilation

Source candidate, 2026-10-17. No tests or qualification runs executed.

`PrepareSemanticAnalysisParams` supports only `scene.parse` and `depth.predict`. The exact source WorkingImage bytes must match the version binding SHA, canvas hash, float32 work Rec.2020 gamma24 color contract and supported dimensions. Inputs derive from that document, including RGB pixels, optional alpha and geometry references. Caller arrays do not define an alternative closure.

Scene Params use `segformer_b0_ade` and `segformer_ade512_official_v1`. Depth Params use `depth_anything_v2_small`, `depth_anything_v2_small_518_v1` and input size 518. Both freeze the deployed model lock digest. Their own Params input uses the existing generic `Params` transport contract; a consuming semantic transfer separately declares the specific SceneParseParams/DepthParams provenance contracts.

`CompileSemanticAnalysisTask` derives deterministic task and node identities, refuses Params/source aliasing, requires exact byte counts for every declared input and freezes the input digest. The CUDA execution profile carries model/runtime/device hashes and a bounded resource estimate. The estimate includes full-frame conversion and temporary buffers, output files, all input downloads and additional qualified model costs. Depth alpha/invalid-mask buffers are explicitly charged. Model costs are additive. Missing sizes, overflow, invalid pins, unsupported operation or insufficient capacity refuse compilation.

Frozen tasks expose DraftTask, BindPublishedTransport, VerifyInputInventory and ParamsByteCount. Publication builds the existing typed input manifest before binding its transport. Source/model/runtime qualification, canonical producer authority, tenant/project permission and transaction admission are mandatory separate gates. These pure compilers do not activate an operation.

Remaining integration: durable per-source prerequisite descriptors and child scheduling/recovery, installed producer qualification proof, restoration of adopted canonical outputs and embedding them in the final transfer profile. Existing accepted configurations are not rewritten.

Independent compiler review corrections: sizes for the physically available Params and WorkingImage JSON must exactly equal their bytes, rather than trusting smaller catalogue claims. Scene RAM additionally reserves 3 GiB for the maximum legal 150×1024×1024 logits and decoding temporaries, independently of source dimensions. This bounds the accepted decoder output envelope; installed model peaks remain additional. Authored scenarios cover understated Params bytes and maximum decoder cost for a tiny image. No execution or hardware qualification is claimed.

Fresh CUDA analysis prerequisites also refuse measured VRAM requirements above decimal 10,000,000,000 bytes, even when the operator-supplied capacity ceiling is larger. This prevents admitting a permanently unschedulable model under the MVP worker budget. The boundary is specific to this CUDA path; it does not add a cap to Metal. Boundary scenarios are authored, not executed.
