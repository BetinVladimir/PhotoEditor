"""Publish scoped transform files with target-bound metadata."""
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.mask_transform import MaskTransformParams,TransformedMask


@dataclass(frozen=True)
class PublishedTransform:
    result: TransformedMask
    outputs: tuple
    bytes_written: int


def stage_transform(artifacts,files,params,target,*,check_active=lambda:None):
    """Must run inside prepare_transform; no DB commit/adoption is performed."""
    params=MaskTransformParams.model_validate_json(canonical_bytes(params))
    target=WorkingImage.model_validate_json(canonical_bytes(target))
    chain=params.validate_target(target)
    radius=files.radius
    if ((params.dtype=='uint16' and radius is not None)
            or (params.dtype=='float32' and (type(radius) is not int or radius<1))):
        raise ValueError('MASK_TRANSFORM_RADIUS_INVALID')
    check_active();files.arrays.validate();files.pngs.validate()
    pixels=artifacts.stage(files.array,files.arrays,'transformed_mask','application/octet-stream')
    check_active()
    png=artifacts.stage(files.png,files.pngs,'transformed_mask_png','image/png')
    written=files.array.stat().st_size+files.png.stat().st_size
    mask=MaskRef(artifact=pixels,canvas=target.canvas,transform_chain=chain,object_id=params.mask.object_id,
        selector=params.mask.selector,confidence=params.mask.confidence,model_lock_id=params.mask.model_lock_id,
        protected=params.mask.protected)
    result=TransformedMask(params=params,mask=mask,png=png,dtype=params.dtype,guided_radius=radius)
    check_active()
    return PublishedTransform(result,(pixels,png),written+len(canonical_bytes(result)))


def finish_transform(artifacts,staged,*,check_active=lambda:None):
    if not isinstance(staged,PublishedTransform) or not 2<=len(staged.outputs)<=126:
        raise ValueError('MASK_TRANSFORM_PUBLICATION_INVALID')
    result=TransformedMask.model_validate_json(canonical_bytes(staged.result))
    expected=[result.mask.artifact,result.png]
    for region in result.regions:
        for ref in (region.mask.artifact,region.png):
            if ref not in expected:expected.append(ref)
    if len(staged.outputs)!=len(expected) or set((r.artifact_id,r.sha256) for r in staged.outputs)!=set((r.artifact_id,r.sha256) for r in expected):
        raise ValueError('MASK_TRANSFORM_OUTPUT_INVENTORY_INVALID')
    check_active();manifest=artifacts.stage_contract(result,'mask_transform_result');check_active()
    return PublishedTransform(staged.result,(*staged.outputs,manifest),staged.bytes_written)


def publish_transform(artifacts,files,params,target,*,check_active=lambda:None):
    return finish_transform(artifacts,stage_transform(artifacts,files,params,target,check_active=check_active),
        check_active=check_active)
