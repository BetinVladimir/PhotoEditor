"""V28 donor glare ImageResult closure with explicit no-generation lineage."""

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.v2.portrait_donor_images import verify_donor_working_image

from .donor_files import DonorPortraitFiles
from .local_result import StagedLocalPortrait


def stage_glare_donor_result(artifacts, params, source_raw, files, *,
                             color_warning, check_active):
    """Stage an always-reviewed donor repair with original alpha and canvas."""
    if (not isinstance(params, PortraitGlareParams) or params.mode != 'donor'
            or params.donor is None or not isinstance(files, DonorPortraitFiles)
            or files.review_required is not True or not callable(check_active)
            or color_warning not in (None, 'COLOR_MATCH_UNAVAILABLE')):
        raise ValueError('GLARE_RESULT_INVALID')
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
    changed_pixels = stage(files.changed_mask, 'mask_pixels')
    changed = MaskRef(artifact=changed_pixels, canvas=source.canvas,
        transform_chain=source.transform_chain, object_id=None,
        selector='changed', confidence=1., model_lock_id=None, protected=False)
    changed_ref = document(changed, 'mask')
    report = ImageReport(schema_version=1, status='review',
        operation='portrait.glare', skipped_count=0,
        reason=color_warning or 'donor_review', outside_mask_max_error=0.,
        clipping_fraction=0., identity=None, transform=None,
        sources=[owned.base, owned.donor], registrations=[])
    report_ref = document(report, 'report')
    result = ImageResult(schema_version=1, working_image=image_ref,
        report=report_ref, changed_mask=changed_ref, generated_mask=None)
    result_ref = document(result, 'document')
    check_active()
    return StagedLocalPortrait(result_ref, tuple(outputs), True, written)
