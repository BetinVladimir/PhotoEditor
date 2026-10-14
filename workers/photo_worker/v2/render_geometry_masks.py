"""Scoped explicit mask geometry, preserving existing guided refinement."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from copy import deepcopy
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,TransformChain,ResizeTransform
from photo_worker.operations.mask.transform_prepare import prepare_transform
from photo_worker.algorithms.mask_transform import transform_tiles
from photo_worker.runtime.arrays import read_array


@dataclass(frozen=True)
class GeometryMasks:
    masks: tuple
    generated: object
    transform_chain: TransformChain
    files: tuple = ()
    generated_path: object = None
    generated_owner: object = None


@contextmanager
def prepare_resize_masks(entries,generated,step,source_chain,guide,scratch,*,execution_profile,tile=512,
                         max_work_pixels=4194304,check_active=lambda:None):
    """Caller admits RAM/scratch and proves mask/guide/source authority.

    entries are (MaskRef, readonly pixels) tuples. Probability masks retain
    existing target-guide refinement; generated lineage is bilinear geometry
    only, never inferred/refined from output image content.
    """
    execution_profile=deepcopy(execution_profile)
    if not isinstance(step,ResizeTransform):raise ValueError('RENDER_MASK_RESIZE_REQUIRED')
    step=ResizeTransform.model_validate_json(canonical_bytes(step))
    source_chain=TransformChain.model_validate_json(canonical_bytes(source_chain))
    if not isinstance(entries,tuple) or len(entries)>257:raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    owned=[]
    for entry in entries:
        if not isinstance(entry,tuple) or len(entry)!=2:raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
        reference,pixels=entry
        if not isinstance(reference,MaskRef):raise ValueError('RENDER_GEOMETRY_MASK_INVALID')
        reference=MaskRef.model_validate_json(canonical_bytes(reference))
        if reference.canvas!=step.from_canvas or reference.transform_chain!=source_chain:
            raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
        owned.append((reference,pixels))
    if len({(reference.artifact.artifact_id,reference.artifact.sha256) for reference,_ in owned})!=len(owned):
        raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    h,w=step.to_canvas.height,step.to_canvas.width
    if not isinstance(guide,np.ndarray) or guide.dtype!=np.float32 or guide.shape!=(h,w,3):
        raise ValueError('RENDER_GEOMETRY_GUIDE_INVALID')
    from .render_resources import admit_render
    peak=resize_masks_peak(step,len(owned),has_generated=generated is not None,tile=tile,max_work_pixels=max_work_pixels)
    admit_render(execution_profile,w,h,mask_refs=0,mask_nodes=0,provider_peak=peak,tile_size=tile)
    chain=TransformChain.create([*source_chain.steps,step])
    with ExitStack() as stack:
        result=[];owners=[]
        for reference,pixels in owned:
            check_active()
            files=stack.enter_context(prepare_transform(pixels,step,scratch,guide=guide,tile=tile,
                max_work_pixels=max_work_pixels,check_active=check_active))
            output=stack.enter_context(read_array(files.array,(h,w),'float32',check_active,unit_interval=True))
            result.append((reference,output,files.radius));owners.append(files)
        lineage=None;lineage_path=None;lineage_owner=None
        if generated is not None:
            reservation=stack.enter_context(scratch.reserve(h*w*4+8192))
            path=reservation.path('resized-generated','.npy')
            lineage_path=path;lineage_owner=reservation
            mapping=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w))
            try:
                for part in transform_tiles(generated,step,tile_size=tile,check_active=check_active):
                    th,tw=part.values.shape;mapping[part.y:part.y+th,part.x:part.x+tw]=part.values
                mapping.flush();reservation.validate()
            finally:mapping._mmap.close()
            lineage=stack.enter_context(read_array(path,(h,w),'float32',check_active,unit_interval=True))
        check_active();yield GeometryMasks(tuple(result),lineage,chain,tuple(owners),lineage_path,lineage_owner);check_active()


def publish_resize_masks(artifacts,prepared,target_ref,target,step,*,check_active=lambda:None):
    """Publish explicit masks within their live resize context; lineage separate."""
    from photo_worker.contracts.mask_transform import MaskTransformParams
    from photo_worker.contracts.images import WorkingImage
    from copy import deepcopy
    target=WorkingImage.model_validate_json(canonical_bytes(target))
    target_ref=deepcopy(target_ref)
    from photo_worker.operations.mask.transform_publish import publish_transform
    if not isinstance(prepared,GeometryMasks) or len(prepared.files)!=len(prepared.masks):
        raise ValueError('RENDER_GEOMETRY_MASK_INVENTORY_INVALID')
    if not isinstance(step,ResizeTransform) or target.canvas!=step.to_canvas or target.transform_chain!=prepared.transform_chain:
        raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
    # Validate the complete target inventory before staging the first output.
    params=[]
    for (source,_,radius),files in zip(prepared.masks,prepared.files):
        item=MaskTransformParams(mask=source,target=target_ref,step=step,dtype='float32')
        if item.validate_target(target)!=prepared.transform_chain or files.radius!=radius:
            raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
        params.append(item)
    return tuple(publish_transform(artifacts,files,item,target,check_active=check_active)
        for files,item in zip(prepared.files,params))


def resize_masks_peak(step,count,*,has_generated,tile=512,max_work_pixels=4194304):
    """All retained mask/output/PNG owners and one complete guided halo."""
    from photo_worker.algorithms.mask_png import png16_bound
    if (not isinstance(step,ResizeTransform) or type(count) is not int or not 0<=count<=257
            or type(has_generated) is not bool or type(tile) is not int or tile not in (256,512,1024)
            or type(max_work_pixels) is not int or not 1<=max_work_pixels<=16777216):
        raise ValueError('RENDER_GEOMETRY_MASK_BUDGET_INVALID')
    h,w=step.to_canvas.height,step.to_canvas.width
    sh,sw=step.from_canvas.height,step.from_canvas.width
    if max(h*w,sh*sw)>200000000:raise ValueError('RENDER_GEOMETRY_MASK_BUDGET_INVALID')
    radius=max(1,round(8*max(h,w)/1024))
    work=min(h,tile+4*radius)*min(w,tile+4*radius)
    if count and work>max_work_pixels:raise ValueError('GUIDED_WORK_BUDGET_EXCEEDED')
    png=png16_bound(w,h)
    scratch=count*(8*h*w+8192+png)+(4*h*w+8192 if has_generated else 0)
    ram=count*(4*sh*sw+8*h*w+16384)+12*h*w+512*work+1048576
    if has_generated:ram+=4*(sh*sw+h*w)+16384
    return dict(ram_bytes=ram,scratch_bytes=scratch,vram_bytes=0,
        output_bytes=count*(4*h*w+8192+png)+(4*h*w+8192 if has_generated else 0))


@dataclass(frozen=True)
class PublishedGeometryLineage:
    mask: object
    reference: object
    outputs: tuple
    bytes_written: int


def publish_resize_generated(artifacts,prepared,target,step,*,check_active=lambda:None):
    """Stage geometric generated coverage only; origin report lineage remains required."""
    from photo_worker.contracts.images import WorkingImage
    target=WorkingImage.model_validate_json(canonical_bytes(target))
    if (not isinstance(prepared,GeometryMasks) or not isinstance(step,ResizeTransform)
            or target.canvas!=step.to_canvas or target.transform_chain!=prepared.transform_chain
            or not target.transform_chain.steps or target.transform_chain.steps[-1]!=step):
        raise ValueError('RENDER_GEOMETRY_MASK_BINDING_CHANGED')
    if prepared.generated is None:
        if prepared.generated_path is not None or prepared.generated_owner is not None:
            raise ValueError('RENDER_GEOMETRY_LINEAGE_INVALID')
        return None
    if (prepared.generated_path is None or prepared.generated_owner is None
            or not isinstance(prepared.generated,np.ndarray) or prepared.generated.dtype!=np.float32
            or prepared.generated.shape!=(target.canvas.height,target.canvas.width)):
        raise ValueError('RENDER_GEOMETRY_LINEAGE_INVALID')
    check_active();prepared.generated_owner.validate_file(prepared.generated_path)
    pixels=artifacts.stage(prepared.generated_path,prepared.generated_owner,'mask_pixels','application/x-npy')
    check_active()
    mask=MaskRef(artifact=pixels,canvas=target.canvas,transform_chain=target.transform_chain,
        selector='generated',confidence=1.,model_lock_id=None,protected=False)
    reference=artifacts.stage_contract(mask,'mask');check_active()
    return PublishedGeometryLineage(mask,reference,(pixels,reference),
        prepared.generated_path.stat().st_size+len(canonical_bytes(mask)))
