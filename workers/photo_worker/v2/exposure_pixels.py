"""Numerical replay for already document-bound approved exposure inputs."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.operations.optical.color import to_working_linear
from photo_worker.operations.semantic.local_exposure import apply_local_exposure
from photo_worker.operations.semantic.exposure_encoding import restore_local_exposure_encoding
from .array_evidence import ArrayEvidenceSession
from .adoption import ArtifactRef, AdoptionInvalid


def verify_exposure_pixels(reader, edit, source, output, mask, protection, *,
        scratch_root, max_bytes, verify_color, check_active):
    """Caller must bind proposal geometry and canonical protection origin first.

    Memory is bounded by 256x256 replay tiles; scratch accounts all four NPYs.
    This gate alone does not authorize proposal/protection/runtime documents.
    """
    if not callable(verify_color) or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    source = WorkingImage.model_validate_json(canonical_bytes(source))
    output = WorkingImage.model_validate_json(canonical_bytes(output))
    mask = MaskRef.model_validate_json(canonical_bytes(mask))
    protection = MaskRef.model_validate_json(canonical_bytes(protection))
    if (edit.kind != 'local_exposure' or mask.protected or not protection.protected
            or any(value.canvas != source.canvas or value.transform_chain != source.transform_chain
                   for value in (output, mask, protection)) or output.color != source.color):
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_PIXEL_BINDING_INVALID')
    check_active()
    if verify_color(source.color.model_copy(deep=True)) is not True:
        raise AdoptionInvalid('SEMANTIC_EXPOSURE_COLOR_INVALID')
    check_active()
    shape = (source.canvas.height, source.canvas.width)
    def ref(value):return ArtifactRef(value.artifact_id, value.sha256)
    # Exposure preserves the entire source chain. Verify inherited field bytes
    # independently before allowing legacy input MIME in the output closure.
    # Release these mappings before pixel replay; both phases use the same bound.
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as fields:
        for step in source.transform_chain.steps:
            if step.kind == 'dense_warp':
                fields.array(ref(step.inverse_field),
                    (step.to_canvas.height, step.to_canvas.width, 2), source='input',
                    legacy_input_npy=True, inverse_field=True)
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        before = arrays.array(ref(source.pixels), (*shape, 3), source='input',legacy_input_npy=True)
        after = arrays.array(ref(output.pixels), (*shape, 3), source='output', role='pixels')
        weights = arrays.array(ref(mask.artifact), shape, source='input', unit=True,legacy_input_npy=True)
        protected = arrays.array(ref(protection.artifact), shape, source='input', unit=True,legacy_input_npy=True)
        for y in range(0, shape[0], 256):
            for x in range(0, shape[1], 256):
                check_active()
                tile = np.s_[y:y+256, x:x+256]
                try:
                    with np.errstate(under='ignore', divide='raise'):
                        linear = to_working_linear(before[tile], source.color)
                    changed = apply_local_exposure(linear, weights[tile], protected[tile],
                        ev=edit.ev, strength=edit.strength, max_bytes=96*256*256,
                        check_active=check_active)
                    expected = restore_local_exposure_encoding(before[tile], changed,
                        weights[tile], protected[tile], source.color, max_bytes=192*256*256,
                        check_active=check_active)
                except ValueError as error:
                    raise AdoptionInvalid('SEMANTIC_EXPOSURE_REPLAY_INVALID') from error
                if not np.array_equal(expected.view(np.uint32), after[tile].view(np.uint32)):
                    raise AdoptionInvalid('SEMANTIC_EXPOSURE_PIXELS_CHANGED')
        check_active()
