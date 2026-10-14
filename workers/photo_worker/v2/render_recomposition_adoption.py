"""Compare staged bytes with an independently reconstructed full composition."""
import numpy as np
from .adoption import AdoptionInvalid
from .array_evidence import ArrayEvidenceSession
from .lens_bypass import _document, _ref
from .render_adjustments import AdjustmentPixels
from .render_adoption import verify_render_metadata, verify_render_output_arrays
from photo_worker.contracts.images import MaskRef


def verify_render_recomposition(reader, frozen_ref, root_ref, binding, parent_roots,
                                expected, *, scratch_root, max_bytes,
                                max_tile_bytes, tile_size=512,
                                check_active=lambda: None):
    """Caller retains independently prepared, qualified pixel owners throughout.

    Expected pixels must come from replay of the complete task-authoritative
    recipe/provider inventory, never from the submitted output. This comparison
    proves byte equivalence and generated coverage; it does not itself qualify
    providers or prove inventory completeness. Exact changed/protected coverage
    remains a separate verify_render_changes gate. No SQL writes occur here.
    """
    check_active()
    if (not isinstance(expected, AdjustmentPixels) or type(tile_size) is not int
            or tile_size not in (256, 512, 1024) or type(max_tile_bytes) is not int):
        raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_POLICY_INVALID')
    _, result, output = verify_render_metadata(reader, frozen_ref, root_ref,
        binding, parent_roots, check_active=check_active)
    shape = (output.canvas.height, output.canvas.width)
    if (shape[0]*shape[1] > 200000000
            or max_tile_bytes < min(tile_size, shape[0])*min(tile_size, shape[1])*64):
        raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_BUDGET_INVALID')
    for array, dimensions in ((expected.rgb, (*shape, 3)),
                              (expected.alpha, shape), (expected.generated, shape)):
        if array is None and dimensions == shape:
            continue
        if (not isinstance(array, np.ndarray) or array.dtype != np.float32
                or array.shape != dimensions or array.flags.writeable):
            raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_EXPECTED_INVALID')
    verify_render_output_arrays(reader, frozen_ref, root_ref, binding, parent_roots,
        check_active=check_active)
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        rgb_ref = _ref(output.pixels)
        actual_rgb = arrays.array(rgb_ref, (*shape, 3), source=reader.evidence(rgb_ref).source)
        actual_alpha = None
        if output.alpha is not None:
            ref = _ref(output.alpha)
            actual_alpha = arrays.array(ref, shape, source=reader.evidence(ref).source, unit=True)
        actual_generated = None
        if result.generated_mask is not None:
            mask = _document(reader, _ref(result.generated_mask), MaskRef, 'output')
            actual_generated = arrays.array(_ref(mask.artifact), shape,
                source='output', role='mask_pixels', unit=True)
        for y in range(0, shape[0], tile_size):
            for x in range(0, shape[1], tile_size):
                check_active()
                part = np.s_[y:y+tile_size, x:x+tile_size]
                wanted_rgb = expected.rgb[part]
                if not np.isfinite(wanted_rgb).all():
                    raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_EXPECTED_INVALID')
                if not np.array_equal(wanted_rgb.view(np.uint32), actual_rgb[part].view(np.uint32)):
                    raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_RGB_MISMATCH')
                tile_shape = wanted_rgb.shape[:2]
                for wanted, actual, name, default in (
                        (expected.alpha, actual_alpha, 'ALPHA', 1),
                        (expected.generated, actual_generated, 'GENERATED', 0)):
                    expected_tile = (np.full(tile_shape, default, np.float32)
                        if wanted is None else wanted[part])
                    actual_tile = (np.full(tile_shape, default, np.float32)
                        if actual is None else actual[part])
                    if (not np.isfinite(expected_tile).all() or np.any(expected_tile < 0)
                            or np.any(expected_tile > 1)
                            or name == 'GENERATED' and np.any((expected_tile != 0) & (expected_tile != 1))):
                        raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_EXPECTED_INVALID')
                    if not np.array_equal(expected_tile.view(np.uint32), actual_tile.view(np.uint32)):
                        raise AdoptionInvalid('RENDER_ADOPTION_RECOMPOSITION_'+name+'_MISMATCH')
    check_active()
