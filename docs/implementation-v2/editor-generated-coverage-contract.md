# Editor: generated coverage and executed-base seal

Coverage describes where generated pixels or their downstream influence may be present. It is binary provenance support, not a probability mask and not a statement that every selected pixel numerically changed. It never changes the RGB processing algorithm. A request involving generation retains review even if a later crop removes every generated pixel.

`image.generate` and `image.inpaint` publish their actual operation selection: target >= 0.5, excluding protection >= 0.5. `image.outpaint` publishes the actual generated region outside the restored source rectangle. Their typed reports reference `generated_mask`, which references immutable float32 binary `generated_pixels`. Both artifacts belong to the same canonical producer result; old reports without the field retain their previous serialized bytes, but cannot qualify a newly required coverage trace.

Seal Params contain a bounded `coverage_trace` of canonical Apply image-producing tasks, in order. Every entry includes task/result/attempt/fence, operation, source WorkingImage, output WorkingImage and the generation/inpaint report when applicable. SQL checks canonical producers, immutable Apply Params.image, declared source, output roles and the continuous initial-effect-source to final-processed-source chain. `trace_sha` is SHA-256 of the canonical JSON list. The coordinator must declare every metadata, mask and inverse-field dependency; missing dependencies fail input admission.

The worker carries initial precompose generated support, transforms prior support and unions every actual generation report mask. It admits the maximum intermediate canvas before physical IO, keeps at most two writable support canvases and releases each previous scratch owner after transformation.

Supported geometry semantics:

- Crop: exact source rectangle support.
- Body: all nonzero clamped bilinear contributors from the actual inverse field, matching `body_warp.bilinear`; no nearest-only approximation.
- Outpaint: exact integer source translation, followed by union of the operation-computed generated region.
- Derivative downsampling: actual AREA source contributor rectangles, with integer boundary arithmetic excluding zero-area neighbours.
- Derivative Lanczos enlargement: conservative eight-tap support envelope, including possible floating kernel tails and replicated borders.
- Neural upscale: if any input has generated support, the entire output is conservatively marked as potentially influenced. The production upscale runs a learned model and transfers its delta, with overlapping tiles; a binary nearest/bilinear resize would falsely assert absence of learned contextual influence. Full support is intentionally broader than exact synthesized-location coverage, but cannot discard a genuine generated contribution. No-AI input remains zero support.
- Mixed-axis AREA resize and image effects with unspecified contextual receptive fields use a conservative full support envelope when prior support exists. Unsupported geometric step types fail; they never produce a blank mask.

The support code is separate from `mask.transform` probability resampling and guided refinement. Guided masks must not be substituted because they change support and do not preserve the binary provenance contract.

The seal is an identity operation over the admitted processed WorkingImage. It independently checks finite values and measures clipping, converts actual QualityReport status, retains generation and inherited review, and publishes ImageResult plus executed-base receipt. Zero outside-mask error describes the identity seal only; it does not describe original-to-processed differences.

Final compose requires a live primary database receipt binding and a declared canonical sealed ImageResult parent. Worker and independent replay verify the same original recipe, processed source, executed Style, report and receipt references. Already executed overrides, correction deltas, learning, session corrections, layer stack and protection masks cannot execute again. Authored layers run once before full Style/geometry. Final base rendering is identity only after receipt qualification.

Implementation is authored, not executed. Remaining assembly obligations are the durable coordinator that writes the canonical trace and its dependency closure, seal output adoption/quota handoff, and final-run freeze/admission. Missing assembly remains a coding gap; tests or historical evidence do not close it.
