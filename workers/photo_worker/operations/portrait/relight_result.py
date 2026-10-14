"""Stage image.relight output closure; lease/adoption authority is caller-owned."""
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.optical.contracts import ImageReport,ImageResult


@dataclass(frozen=True)
class StagedRelight:
    document:object
    outputs:tuple
    review_required:bool
    bytes_written:int


def stage_relight_result(artifacts,params,source,files):
    if not isinstance(params,ImageRelightParams) or not isinstance(source,WorkingImage):raise ValueError('RELIGHT_PARAMS_INVALID')
    if (source.dtype!='float32' or canonical_hash(source.canvas)!=params.base.canvas_hash
            or canonical_hash(source)!=params.base.source.sha256):raise ValueError('RELIGHT_SOURCE_MISMATCH')
    if files.review_required is not True:raise ValueError('RELIGHT_REVIEW_MISMATCH')
    files.reservation.validate()
    outputs=[];written=0
    def stage(path,role):
        nonlocal written
        ref=artifacts.stage(path,files.reservation,role,'application/x-npy')
        outputs.append(ref);written+=path.stat().st_size;return ref
    def contract(value,role):
        nonlocal written
        ref=artifacts.stage_contract(value,role)
        outputs.append(ref);written+=len(canonical_bytes(value));return ref
    pixels=stage(files.rgb,'pixels')
    image=source.model_copy(update={'pixels':pixels})
    image_ref=contract(image,'working_image')
    changed_pixels=stage(files.changed_mask,'mask_pixels')
    changed=MaskRef(artifact=changed_pixels,canvas=source.canvas,transform_chain=source.transform_chain,
        object_id=None,selector='changed',confidence=1.,model_lock_id=None,protected=False)
    changed_ref=contract(changed,'mask')
    report=ImageReport(schema_version=1,status='review',operation='image.relight',skipped_count=0,
        reason='relative_depth_exposure',outside_mask_max_error=0.,clipping_fraction=0.,
        identity=None,transform=None,sources=[params.base],registrations=[])
    report_ref=contract(report,'report')
    document=contract(ImageResult(schema_version=1,working_image=image_ref,report=report_ref,
        changed_mask=changed_ref,generated_mask=None),'document')
    return StagedRelight(document,tuple(outputs),True,written)
