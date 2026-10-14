"""Typed donor result staging; caller owns task admission and pixel proof."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.optical.contracts import ImageReport,ImageResult
from photo_worker.v2.portrait_donor_images import verify_donor_working_image
from .local_result import StagedLocalPortrait
from .donor_files import DonorPortraitFiles


def stage_donor_result(artifacts,params,source_raw,files,*,color_warning,check_active):
    """Stage six declared output roles with original alpha/ICC/geometry.

    Files must come from admitted donor processing and write_donor_patch_files.
    File writer proves outside-mask preservation against supplied masks;
    staging does not prove physical mask lineage or current consent.
    Donor edits always require review; no generated mask or identity assertion.
    """
    if not isinstance(params,PortraitOpenEyesParams) or not callable(check_active):
        raise ValueError('DONOR_RESULT_CONTEXT')
    if (not isinstance(files,DonorPortraitFiles) or
            color_warning not in (None,'COLOR_MATCH_UNAVAILABLE') or files.review_required is not True):
        raise ValueError('DONOR_RESULT_REVIEW')
    owned=PortraitOpenEyesParams.model_validate_json(canonical_bytes(params))
    source=verify_donor_working_image(bytes(source_raw),owned.base)
    files.reservation.validate();check_active()
    outputs=[];written=0
    def stage(path,role):
        nonlocal written
        check_active()
        size=path.stat().st_size
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=size
        return ref
    def contract(value,role):
        nonlocal written
        check_active()
        size=len(canonical_bytes(value))
        ref=artifacts.stage_contract(value,role)
        outputs.append(ref);written+=size
        return ref
    pixels=stage(files.rgb,'pixels')
    image=source.model_copy(update={'pixels':pixels},deep=True)
    image_ref=contract(image,'working_image')
    mask_pixels=stage(files.changed_mask,'mask_pixels')
    mask=MaskRef(artifact=mask_pixels,canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    mask_ref=contract(mask,'mask')
    report=ImageReport(schema_version=1,status='review',operation='portrait.open_eyes',
        skipped_count=0,reason=color_warning or 'donor_review',outside_mask_max_error=0.,
        clipping_fraction=0.,identity=None,transform=None,sources=[owned.base,owned.donor],registrations=[])
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,report=report_ref,
        changed_mask=mask_ref,generated_mask=None),'document')
    check_active()
    return StagedLocalPortrait(document,tuple(outputs),True,written)
