"""Independent evidence-scoped same-canvas full-render output validation."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.operations.optical.contracts import ImageResult, ImageReport
from .adoption import AdoptionInvalid
from .lens_bypass import _document, _ref
from .render_recipe import FrozenRenderRecipe, decode_frozen_render_recipe
from .render_reports import collect_render_parent_reports


def verify_render_metadata(reader, frozen_ref, root_ref, binding, parent_roots, *, check_active=lambda: None):
    """Frozen ref, source binding and parent inventory must be task-authoritative.

    Reader proves exact immutable task/attempt evidence. This validates same-
    canvas metadata and report lineage, not pixel equality or provider metrics.
    Geometry requires a separate proof and must not use this validator.
    """
    check_active()
    _document(reader, frozen_ref, FrozenRenderRecipe, 'input')
    try:
        raw = reader.read_json(frozen_ref, 1048576)
        if not isinstance(raw, bytes) or len(raw) > 1048576 or sha256(raw).hexdigest() != frozen_ref.sha256:
            raise ValueError("recipe bytes changed")
        frozen = decode_frozen_render_recipe(raw)
    except ValueError as error:
        raise AdoptionInvalid('RENDER_ADOPTION_RECIPE_INVALID') from error
    if frozen.recipe.source != binding:
        raise AdoptionInvalid('RENDER_ADOPTION_SOURCE_INVALID')
    source = _document(reader, _ref(binding.source), WorkingImage, 'input')
    if source.dtype != 'float32' or canonical_hash(source.canvas) != binding.canvas_hash:
        raise AdoptionInvalid('RENDER_ADOPTION_SOURCE_INVALID')
    result = _document(reader, root_ref, ImageResult, 'output')
    output_ref = _ref(result.working_image)
    output_scope = reader.evidence(output_ref).source
    if output_scope not in ('input', 'output'):
        raise AdoptionInvalid('RENDER_ADOPTION_EVIDENCE_INVALID')
    output = _document(reader, output_ref, WorkingImage, output_scope)
    if (output.dtype != source.dtype or output.canvas != source.canvas
            or output.color != source.color or output.transform_chain != source.transform_chain):
        raise AdoptionInvalid('RENDER_ADOPTION_GEOMETRY_INVALID')
    for ref, selector in ((result.changed_mask, 'changed'), (result.generated_mask, 'generated')):
        if ref is None:
            continue
        check_active()
        mask = _document(reader, _ref(ref), MaskRef, 'output')
        if (mask.canvas != output.canvas or mask.transform_chain != output.transform_chain
                or mask.selector != selector or mask.protected or mask.object_id is not None
                or mask.confidence != 1 or mask.model_lock_id is not None):
            raise AdoptionInvalid('RENDER_ADOPTION_MASK_INVALID')
    parents = tuple(_document(reader, ref, ImageResult, 'input') for ref in parent_roots)
    class Artifacts:
        def contract(self, ref, model):
            check_active()
            return _document(reader, _ref(ref), model, 'input')
    try:
        evidence = collect_render_parent_reports(Artifacts(), parents, binding, check_active=check_active)
    except ValueError as error:
        raise AdoptionInvalid('RENDER_ADOPTION_PARENT_INVALID') from error
    report = _document(reader, _ref(result.report), ImageReport, 'output')
    if (report.operation != 'render.compose' or report.status not in ('pass', 'review')
            or report.sources != [binding] or report.registrations
            or tuple(report.parent_reports or ()) != evidence.reports
            or evidence.review_required and report.status != 'review'):
        raise AdoptionInvalid('RENDER_ADOPTION_REPORT_INVALID')
    check_active()
    return frozen, result, output


def verify_render_output_arrays(reader, frozen_ref, root_ref, binding, parent_roots, *, check_active=lambda: None):
    """Verify staged canonical NPY bytes and restrict source input reuse.

    Finite RGB, unit alpha and binary changed/generated masks are checked via
    bounded streams. This does not prove exact render recomposition or changes.
    """
    from .lens_adoption import verify_lens_array
    _, result, output = verify_render_metadata(reader, frozen_ref, root_ref,
        binding, parent_roots, check_active=check_active)
    shape = (output.canvas.height, output.canvas.width)
    if shape[0] * shape[1] > 200000000:
        raise AdoptionInvalid('RENDER_ADOPTION_PIXEL_LIMIT')
    source = _document(reader, _ref(binding.source), WorkingImage, 'input')
    for reference, original, dimensions, role, unit in (
            (output.pixels, source.pixels, (*shape, 3), 'pixels', False),
            (output.alpha, source.alpha, shape, 'alpha', True)):
        check_active()
        if reference is None:
            continue
        evidence = reader.evidence(_ref(reference))
        if evidence.source == 'output':
            verify_lens_array(reader, _ref(reference), dimensions, role,
                unit=unit, check_active=check_active)
        elif evidence.source != 'input' or reference != original:
            raise AdoptionInvalid('RENDER_ADOPTION_INPUT_REUSE_INVALID')
    for reference in (result.changed_mask, result.generated_mask):
        check_active()
        if reference is None:
            continue
        mask = _document(reader, _ref(reference), MaskRef, 'output')
        verify_lens_array(reader, _ref(mask.artifact), shape, 'mask_pixels',
            unit=True, binary=True, check_active=check_active)
    check_active()


def verify_render_changes(reader, frozen_ref, root_ref, binding, parent_roots, *,
                          scratch_root, max_bytes, max_tile_bytes, tile_size=512,
                          alpha_mode='require_float32', check_active=lambda: None):
    """Independently prove exact final change coverage and protected pixels.

    Does not recompute base style/layer effects or establish generated provenance.
    """
    import numpy as np
    from .array_evidence import ArrayEvidenceSession
    if (type(tile_size) is not int or tile_size not in (256, 512, 1024)
            or alpha_mode not in ('require_float32', 'normalize_uint16')):
        raise AdoptionInvalid('RENDER_ADOPTION_CHANGE_POLICY_INVALID')
    frozen, result, output = verify_render_metadata(reader, frozen_ref, root_ref,
        binding, parent_roots, check_active=check_active)
    shape = (output.canvas.height, output.canvas.width)
    if (shape[0]*shape[1] > 200000000 or type(max_tile_bytes) is not int
            or max_tile_bytes < min(tile_size,shape[0])*min(tile_size,shape[1])*64):
        raise AdoptionInvalid('RENDER_ADOPTION_CHANGE_BUDGET_INVALID')
    verify_render_output_arrays(reader, frozen_ref, root_ref, binding, parent_roots,
        check_active=check_active)
    source = _document(reader, _ref(binding.source), WorkingImage, 'input')
    changed = _document(reader, _ref(result.changed_mask), MaskRef, 'output')
    protection = None
    if frozen.recipe.protected_mask is not None:
        protection = _document(reader, _ref(frozen.recipe.protected_mask), MaskRef, 'input')
        if (not protection.protected or protection.canvas != source.canvas
                or protection.transform_chain != source.transform_chain):
            raise AdoptionInvalid('RENDER_ADOPTION_PROTECTION_INVALID')
    with ArrayEvidenceSession(reader, scratch_root, max_bytes, check_active) as arrays:
        before = arrays.array(_ref(source.pixels), (*shape,3), source='input')
        after = arrays.array(_ref(output.pixels), (*shape,3),
            source=reader.evidence(_ref(output.pixels)).source)
        before_alpha = arrays.alpha(_ref(source.alpha) if source.alpha is not None else None,
            shape, mode=alpha_mode)
        if output.alpha is None:
            after_alpha = None
        elif output.alpha == source.alpha:
            # A float32 WorkingImage must not retain legacy uint16 alpha, even
            # when source comparison explicitly permits normalization.
            raw_alpha = arrays.array(_ref(source.alpha), shape, source="input", unit=True,
                legacy_alpha=alpha_mode == "normalize_uint16")
            if raw_alpha.dtype != np.float32:
                raise AdoptionInvalid("RENDER_ADOPTION_ALPHA_REUSE_INVALID")
            after_alpha = before_alpha
        else:
            after_alpha = arrays.array(_ref(output.alpha), shape, source='output', role='alpha', unit=True)
        mask = arrays.array(_ref(changed.artifact), shape, source='output', role='mask_pixels', unit=True)
        protected = (arrays.array(_ref(protection.artifact), shape, source='input', unit=True)
            if protection is not None else None)
        for y in range(0,shape[0],tile_size):
            for x in range(0,shape[1],tile_size):
                check_active()
                part=np.s_[y:y+tile_size,x:x+tile_size]
                rgb=np.any(before[part].view(np.uint32)!=after[part].view(np.uint32),axis=-1)
                alphas=[np.ones(rgb.shape,np.float32) if value is None else value[part]
                    for value in (before_alpha,after_alpha)]
                expected=rgb | (alphas[0].view(np.uint32)!=alphas[1].view(np.uint32))
                if not np.array_equal(mask[part],expected.astype(np.float32)):
                    raise AdoptionInvalid('RENDER_ADOPTION_CHANGED_MASK_INVALID')
                if protected is not None and np.any(expected & (protected[part]>0)):
                    raise AdoptionInvalid('RENDER_ADOPTION_PROTECTED_PIXEL_CHANGED')
    check_active()
