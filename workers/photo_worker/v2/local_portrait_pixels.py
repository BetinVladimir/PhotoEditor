"""Bounded physical invariants for document-bound local portrait results."""
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.images import MaskRef
from .adoption import ArtifactRef, AdoptionInvalid, require_adoption_active
from .array_evidence import ArrayEvidenceSession


def verify_local_portrait_tile(before, after, target, protected, changed, strength):
    untouched = (target == 0) | (protected > 0)
    if strength == 0:
        untouched = np.ones(target.shape, dtype=bool)
    if not np.array_equal(before.view(np.uint32)[untouched], after.view(np.uint32)[untouched]):
        raise AdoptionInvalid('LOCAL_PORTRAIT_PROTECTED_PIXELS_CHANGED')
    # Staging defines changed by numeric RGB comparison, including exact bypass.
    expected = np.any(before != after, axis=-1).astype(np.float32)
    if not np.array_equal(changed, expected):
        raise AdoptionInvalid('LOCAL_PORTRAIT_CHANGED_MASK_INVALID')


def verify_local_portrait_pixels(reader, params, source, root, image, *, scratch_root, max_bytes, check_active):
    def active(): require_adoption_active(check_active, 'LOCAL_PORTRAIT_INACTIVE')
    def ref(value): return ArtifactRef(value.artifact_id, value.sha256)
    def mask(value):
        active()
        raw = reader.read_json(ref(value), 8*1024*1024)
        active()
        if sha256(raw).hexdigest() != value.sha256:
            raise AdoptionInvalid('LOCAL_PORTRAIT_BYTES_INVALID')
        return MaskRef.model_validate_json(canonical_bytes(parse_json(raw, max_bytes=8*1024*1024)))
    target, protected, changed = mask(params.mask), mask(params.protected), mask(root.changed_mask)
    shape = (source.canvas.height, source.canvas.width)
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, active) as arrays:
        before = arrays.array(ref(source.pixels), (*shape, 3), source='input')
        after = arrays.array(ref(image.pixels), (*shape, 3), source='output', role='pixels')
        weights = arrays.array(ref(target.artifact), shape, source='input', unit=True)
        guards = arrays.array(ref(protected.artifact), shape, source='input', unit=True)
        delta = arrays.array(ref(changed.artifact), shape, source='output', role='mask_pixels', unit=True)
        if source.alpha is not None:
            arrays.array(ref(source.alpha), shape, source='input', unit=True, legacy_alpha=True)
        for step in source.transform_chain.steps:
            if step.kind == 'dense_warp':
                arrays.array(ref(step.inverse_field), (step.to_canvas.height, step.to_canvas.width, 2), source='input', inverse_field=True)
        for y in range(0, shape[0], 256):
            for x in range(0, shape[1], 256):
                active()
                tile = np.s_[y:y+256, x:x+256]
                verify_local_portrait_tile(before[tile], after[tile], weights[tile], guards[tile], delta[tile], params.strength)
        active()
