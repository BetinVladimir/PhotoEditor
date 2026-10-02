"""Region result binding and pinned encoded-byte proof before SQL adoption."""
from photo_worker.operations.learning.contracts import FrozenRecipe
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.result import RegionDerivativeManifest, verify_region_encoded
from .adoption import AdoptionInvalid
from .lens_bypass import _document, _ref


def verify_region_result(reader, params_ref, root_ref, *, target_icc_sha256,
                         check_active=lambda: None):
    """Target ICC is trusted runtime evidence, never inferred from output JSON."""
    if (not isinstance(target_icc_sha256, str) or len(target_icc_sha256) != 64
            or any(c not in '0123456789abcdef' for c in target_icc_sha256)):
        raise AdoptionInvalid('REGION_ADOPTION_ICC_REQUIRED')
    check_active()
    params = _document(reader, params_ref, RegionPreviewParams, 'input')
    manifest = _document(reader, root_ref, RegionDerivativeManifest, 'output')
    recipe = _document(reader, _ref(params.recipe.ref), FrozenRecipe, 'input')
    if recipe != params.frozen_recipe:
        raise AdoptionInvalid('REGION_ADOPTION_RECIPE_INVALID')
    source_ref = _ref(params.source.ref)
    source_evidence = reader.evidence(source_ref)
    if (source_evidence.ref != source_ref or source_evidence.source != 'input'
            or source_evidence.media_type != 'application/json'
            or source_evidence.bytes != params.source.bytes):
        raise AdoptionInvalid('REGION_ADOPTION_SOURCE_INVALID')
    source_body = reader.read_json(source_ref, 1048576)
    try:
        source = params.validate_source(source_body)
        manifest.validate_for(params, target_icc_sha256, source.alpha is not None)
    except ValueError as exc:
        raise AdoptionInvalid('REGION_ADOPTION_BINDING_INVALID') from exc
    derivative_ref = _ref(manifest.derivative)
    evidence = reader.evidence(derivative_ref)
    if (evidence.ref != derivative_ref or evidence.source != 'output'
            or evidence.role != 'derivative' or evidence.media_type != manifest.media_type
            or evidence.bytes != manifest.bytes):
        raise AdoptionInvalid('REGION_ADOPTION_OUTPUT_INVALID')
    stream = getattr(reader, 'iter_bytes', None)
    if not callable(stream):
        raise AdoptionInvalid('REGION_ADOPTION_STREAM_REQUIRED')
    iterator = iter(stream(derivative_ref, manifest.bytes))
    body = bytearray()
    try:
        for block in iterator:
            check_active()
            if (not isinstance(block, bytes) or not block or len(block) > 65536
                    or len(body) + len(block) > manifest.bytes):
                raise AdoptionInvalid('REGION_ADOPTION_STREAM_INVALID')
            body.extend(block)
    finally:
        close = getattr(iterator, 'close', None)
        if close is not None:
            close()
    try:
        verify_region_encoded(manifest, params, bytes(body),
            target_sha256=target_icc_sha256, expected_alpha=source.alpha is not None)
    except (ValueError, OSError) as exc:
        raise AdoptionInvalid('REGION_ADOPTION_BYTES_INVALID') from exc
    check_active()
