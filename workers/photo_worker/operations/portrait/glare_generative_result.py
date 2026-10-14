"""V28 generative glare result closure with explicit synthetic lineage."""

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.v2.portrait_donor_images import verify_donor_working_image

from .glare_generative_files import GenerativeGlareFiles
from .local_result import StagedLocalPortrait


def stage_generative_glare_result(artifacts, params, source_raw, files, *,
                                   check_active):
    """Publish reviewed candidate with mandatory generated-mask evidence."""
    if (not isinstance(params, PortraitGlareParams)
            or params.mode != 'generative' or params.donor is not None
            or params.donor_faces is not None
            or not isinstance(files, GenerativeGlareFiles)
            or not callable(check_active)
            or not 0 <= files.clipping_fraction <= 1):
        raise ValueError('GLARE_GENERATIVE_RESULT_INVALID')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    source = verify_donor_working_image(bytes(source_raw), owned.base)
    files.reservation.validate()
    check_active()
    outputs = []
    written = 0

    def stage(path, role):
        nonlocal written
        check_active()
        ref = artifacts.stage(path, files.reservation, role, 'application/x-npy')
        outputs.append(ref)
        written += path.stat().st_size
        return ref

    def document(value, role):
        nonlocal written
        check_active()
        ref = artifacts.stage_contract(value, role)
        outputs.append(ref)
        written += len(canonical_bytes(value))
        return ref

    pixels_ref = stage(files.rgb, 'pixels')
    image = source.model_copy(update={'pixels': pixels_ref}, deep=True)
    image_ref = document(image, 'working_image')
    changed_pixels = stage(files.changed, 'mask_pixels')
    generated_pixels = stage(files.generated, 'mask_pixels')
    changed = MaskRef(artifact=changed_pixels, canvas=source.canvas,
        transform_chain=source.transform_chain, object_id=None,
        selector='changed', confidence=1., model_lock_id=None,
        protected=False)
    generated = MaskRef(artifact=generated_pixels, canvas=source.canvas,
        transform_chain=source.transform_chain, object_id=None,
        selector='generated', confidence=1.,
        model_lock_id=owned.model_lock_sha, protected=False)
    changed_ref = document(changed, 'mask')
    generated_ref = document(generated, 'mask')
    report = ImageReport(schema_version=1, status='review',
        operation='portrait.glare', skipped_count=0,
        reason='synthetic_glare_proposal', outside_mask_max_error=0.,
        clipping_fraction=files.clipping_fraction, identity=None,
        transform=None, sources=[owned.base], registrations=[])
    report_ref = document(report, 'report')
    result = ImageResult(schema_version=1, working_image=image_ref,
        report=report_ref, changed_mask=changed_ref,
        generated_mask=generated_ref)
    result_ref = document(result, 'document')
    check_active()
    return StagedLocalPortrait(result_ref, tuple(outputs), True, written)
