"""Exact local-portrait document binding; physical pixel qualification is separate."""
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from photo_worker.operations.optical.contracts import ImageResult, ImageReport
from .adoption import ArtifactRef, AdoptionInvalid, MAX_JSON_BYTES, require_adoption_active


def verify_local_portrait_documents(reader, operation, params_ref, root_ref, *, check_active):
    models = {'portrait.hair': PortraitHairParams, 'portrait.clothing': PortraitClothingParams}
    if operation not in models or params_ref is None:
        raise AdoptionInvalid('LOCAL_PORTRAIT_CONTEXT_REQUIRED')

    def active():
        require_adoption_active(check_active, 'LOCAL_PORTRAIT_INACTIVE')

    def ref(value):
        return ArtifactRef(value.artifact_id, value.sha256)

    def document(reference, model, source, role=None):
        active()
        evidence = reader.evidence(reference)
        if (evidence.ref != reference or evidence.source != source
                or evidence.media_type != 'application/json'
                or role is not None and evidence.role != role):
            raise AdoptionInvalid('LOCAL_PORTRAIT_EVIDENCE_INVALID')
        raw = reader.read_json(reference, MAX_JSON_BYTES)
        active()
        if (not isinstance(raw, bytes) or len(raw) != evidence.bytes
                or len(raw) > MAX_JSON_BYTES or sha256(raw).hexdigest() != reference.sha256):
            raise AdoptionInvalid('LOCAL_PORTRAIT_BYTES_INVALID')
        try:
            return model.model_validate_json(canonical_bytes(parse_json(raw, max_bytes=MAX_JSON_BYTES)))
        except ValueError as error:
            raise AdoptionInvalid('LOCAL_PORTRAIT_CONTRACT_INVALID') from error

    params = document(params_ref, models[operation], 'input')
    source = document(ref(params.base.source), WorkingImage, 'input')
    root = document(root_ref, ImageResult, 'output', 'document')
    image = document(ref(root.working_image), WorkingImage, 'output', 'working_image')
    report = document(ref(root.report), ImageReport, 'output', 'report')
    target = document(ref(params.mask), MaskRef, 'input')
    protected = document(ref(params.protected), MaskRef, 'input')
    changed = document(ref(root.changed_mask), MaskRef, 'output', 'mask')
    for mask in (target, protected, changed):
        if (canonical_bytes(mask.canvas) != canonical_bytes(source.canvas)
                or canonical_bytes(mask.transform_chain) != canonical_bytes(source.transform_chain)):
            raise AdoptionInvalid('LOCAL_PORTRAIT_MASK_GEOMETRY_INVALID')
    if (target.protected or not protected.protected or changed.protected
            or changed.selector != 'changed' or changed.object_id is not None
            or changed.confidence != 1 or changed.model_lock_id is not None):
        raise AdoptionInvalid('LOCAL_PORTRAIT_MASK_SEMANTICS_INVALID')
    if (source.dtype != 'float32' or canonical_hash(source.canvas) != params.base.canvas_hash
            or root.generated_mask is not None
            or canonical_bytes(image.model_copy(update={'pixels': source.pixels})) != canonical_bytes(source)):
        raise AdoptionInvalid('LOCAL_PORTRAIT_IMAGE_CHANGED')
    pixels = reader.evidence(ref(image.pixels))
    if (pixels.ref != ref(image.pixels) or pixels.source != 'output'
            or pixels.role != 'pixels' or pixels.media_type != 'application/x-npy'):
        raise AdoptionInvalid('LOCAL_PORTRAIT_PIXELS_INVALID')
    # A valid JSON ref is not evidence of declared binary input membership.
    for value in (source.pixels, source.alpha, target.artifact, protected.artifact):
        if value is None:
            continue
        active()
        binary = reader.evidence(ref(value))
        if (binary.ref != ref(value) or binary.source != 'input'
                or binary.media_type != 'application/x-npy' or binary.bytes <= 0):
            raise AdoptionInvalid('LOCAL_PORTRAIT_INPUT_UNDECLARED')
    active()
    delta = reader.evidence(ref(changed.artifact))
    if (delta.ref != ref(changed.artifact) or delta.source != 'output'
            or delta.role != 'mask_pixels' or delta.media_type != 'application/x-npy'
            or delta.bytes <= 0):
        raise AdoptionInvalid('LOCAL_PORTRAIT_CHANGED_PIXELS_INVALID')
    review = operation == 'portrait.clothing' and params.strength > 0 and params.wrinkle_strength > 0
    if (report.operation != operation or report.status != ('review' if review else 'pass')
            or report.reason != ('wrinkle_correction' if review else None)
            or report.sources != [params.base] or report.skipped_count != 0
            or report.outside_mask_max_error != 0 or report.clipping_fraction != 0
            or report.identity is not None or report.transform is not None
            or report.registrations or report.parent_reports is not None):
        raise AdoptionInvalid('LOCAL_PORTRAIT_REPORT_INVALID')
    active()
    return params, source, root, image, report
