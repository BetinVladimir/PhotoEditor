"""Stage V28 prepared result closure; adoption and authority remain caller-owned."""
from dataclasses import dataclass

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from photo_worker.operations.optical.contracts import ImageReport, ImageResult


@dataclass(frozen=True)
class StagedLocalPortrait:
    document: object
    outputs: tuple
    review_required: bool
    bytes_written: int


def stage_local_result(artifacts, params, source, files):
    if not isinstance(params,(PortraitHairParams,PortraitClothingParams)):
        raise ValueError('PORTRAIT_PARAMS_INVALID')
    if source.dtype!='float32' or canonical_hash(source.canvas)!=params.base.canvas_hash:
        raise ValueError('PORTRAIT_SOURCE_MISMATCH')
    review=isinstance(params,PortraitClothingParams) and params.strength>0 and params.wrinkle_strength>0
    if type(files.review_required) is not bool or files.review_required != review:
        raise ValueError("PORTRAIT_REVIEW_MISMATCH")
    files.reservation.validate()
    outputs=[]
    written=0
    def stage(path,role):
        nonlocal written
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size
        return ref
    def contract(value,role):
        nonlocal written
        ref=artifacts.stage_contract(value,role)
        outputs.append(ref);written+=len(canonical_bytes(value))
        return ref
    # V28 requires pixels and working_image output roles even for exact bypass.
    # prepare_local writes original bits at strength zero; geometry/alpha persist.
    pixels=stage(files.rgb,'pixels')
    image=source.model_copy(update={'pixels':pixels})
    image_ref=contract(image,'working_image')
    changed_pixels=stage(files.changed_mask,'mask_pixels')
    changed=MaskRef(artifact=changed_pixels,canvas=image.canvas,
        transform_chain=image.transform_chain,object_id=None,selector='changed',
        confidence=1.,model_lock_id=None,protected=False)
    changed_ref=contract(changed,'mask')
    operation='portrait.hair' if isinstance(params,PortraitHairParams) else 'portrait.clothing'
    report=ImageReport(schema_version=1,status='review' if review else 'pass',
        operation=operation,skipped_count=0,reason='wrinkle_correction' if review else None,
        outside_mask_max_error=0.,clipping_fraction=0.,identity=None,transform=None,
        sources=[params.base],registrations=[])
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,
        report=report_ref,changed_mask=changed_ref,generated_mask=None),'document')
    return StagedLocalPortrait(document,tuple(outputs),review,written)
