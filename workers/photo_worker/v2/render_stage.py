"""Fenced image/mask staging; quality report and root adoption are separate."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import WorkingImage,MaskRef


@dataclass(frozen=True)
class StagedRenderImage:
    working_image:ArtifactRef
    changed_mask:ArtifactRef
    generated_mask:ArtifactRef|None
    outputs:tuple[ArtifactRef,...]
    bytes_written:int


def stage_render_image(artifacts,source,binding,files,*,alpha_converted=False,check_active=lambda:None):
    """Stage only prepared same-canvas files under their owning reservations.

    No pass/review report is invented here. Caller must retain provider/report
    lineage, build the complete result closure and submit fenced adoption.
    """
    check_active();changes=files.changes
    if (not isinstance(source,WorkingImage) or source.dtype!='float32'
            or binding.canvas_hash!=canonical_hash(source.canvas)
            or type(alpha_converted) is not bool or alpha_converted and source.alpha is None
            or (files.rgb is not None)!=changes.rgb_changed
            or (files.alpha is not None)!=(changes.alpha_changed or alpha_converted)):
        raise ValueError('RENDER_STAGE_INPUT_INVALID')
    files.reservation.validate();changes.reservation.validate()
    outputs=[];written=0
    def stage(path,owner,role):
        nonlocal written
        check_active();owner.validate_file(path)
        ref=artifacts.stage(path,owner,role,'application/x-npy');check_active()
        outputs.append(ref);written+=path.stat().st_size;return ref
    def contract(value,role):
        nonlocal written
        check_active();ref=artifacts.stage_contract(value,role);check_active()
        outputs.append(ref);written+=len(canonical_bytes(value));return ref
    rgb=stage(files.rgb,files.reservation,'pixels') if files.rgb is not None else source.pixels
    alpha=stage(files.alpha,files.reservation,'alpha') if files.alpha is not None else source.alpha
    image=WorkingImage(pixels=rgb,alpha=alpha,dtype='float32',canvas=source.canvas,color=source.color,transform_chain=source.transform_chain)
    image_ref=contract(image,'working_image') if files.rgb is not None or files.alpha is not None else binding.source
    def mask(path,owner,selector):
        pixels=stage(path,owner,'mask_pixels')
        return contract(MaskRef(artifact=pixels,canvas=source.canvas,transform_chain=source.transform_chain,
            selector=selector,confidence=1,model_lock_id=None,protected=False),'mask')
    changed=mask(changes.path,changes.reservation,'changed')
    generated=mask(files.generated,files.reservation,'generated') if files.generated is not None else None
    return StagedRenderImage(image_ref,changed,generated,tuple(outputs),written)
