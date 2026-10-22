"""Native Big-LaMa Region replay uses the confirmed task's exact input mask."""
from photo_worker.contracts.base import canonical_bytes
from contextlib import contextmanager,ExitStack
import numpy as np
from photo_worker.contracts.restoration import InpaintParams
from photo_worker.runtime.arrays import read_array
from photo_worker.operations.image.inpaint import _required_frame_ram,_roi_pixel_budget
from photo_worker.operations.semantic.candidate_inpaint import compute_inpaint_region,inpaint_mask_roi
from photo_worker.operations.semantic.candidate_blend import blend_candidate_region
from photo_worker.operations.semantic.candidate_preservation import verify_candidate_preservation


def inpaint_entry(effective,verified,model_lock_sha):
    requested=effective.extensions.inpainting
    enabled=requested is not None and requested.enabled
    entries=[entry for entry in verified if entry[0].operation=='image.inpaint']
    if len(entries)!=int(enabled):raise ValueError('REGION_INPAINT_STAGE_COVERAGE_MISMATCH')
    if not entries:return None
    _,raw,source,target,_=entries[0];params=InpaintParams.model_validate_json(canonical_bytes(raw))
    if (params.model_id!=requested.model or params.model_lock_id!=model_lock_sha
            or source.canvas!=target.canvas or source.transform_chain!=target.transform_chain or source.alpha!=target.alpha
            or any(mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain for mask in [params.mask,*params.protected])):
        raise ValueError('REGION_INPAINT_POLICY_MISMATCH')
    return params,source


@contextmanager
def prepare_region_inpaint(artifacts,effective,verified,rgb,alpha,*,pool,backend,model_lock_sha,profile,verify_runtime,check_active,private_input=None):
    entry=inpaint_entry(effective,verified,model_lock_sha)
    if entry is None:yield rgb;return
    params,image=entry;h,w=image.canvas.height,image.canvas.width
    ram=profile.get('resource_estimate',{}).get('ram_bytes')
    if pool is None or pool.lock.lock_id!=model_lock_sha or profile.get('backend')!=backend:
        raise ValueError('REGION_INPAINT_INSTALLATION_REQUIRED')
    if type(ram) is not int or ram<max(1073741824,_required_frame_ram(h,w)):
        raise ValueError('REGION_INPAINT_RESOURCE_INVALID')
    verify_runtime();check_active()
    with ExitStack() as scopes:
        def mapped(ref,shape,dtype='float32'):
            artifacts.require_declared_input(ref)
            local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
            return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=dtype=='float32'))
        original=mapped(image.pixels,(h,w,3))
        from .private_sources import require_stage_source
        original_alpha=None if image.alpha is None else mapped(image.alpha,(h,w),'uint16')
        require_stage_source('image.inpaint',params.image,rgb,alpha,original,original_alpha,
            private_input=private_input,check_active=check_active)
        target=np.asarray(mapped(params.mask.artifact,(h,w)))>=.5
        protected=np.zeros((h,w),bool)
        for mask in params.protected:
            values=mapped(mask.artifact,(h,w))
            for y in range(0,h,256):check_active();protected[y:y+256]|=values[y:y+256]>0
        effective_mask=target&~protected
        x0,y0,x1,y1=inpaint_mask_roi(effective_mask,params.context_pixels)
        budget=_roi_pixel_budget(ram,h,w)
        if (x1-x0)*(y1-y0)>budget:raise ValueError('REGION_INPAINT_RESOURCE_INVALID')
        engine=scopes.enter_context(pool.acquire(params.model_id,backend))
        restored,weight,_=compute_inpaint_region(engine,rgb,effective_mask,protected,(x0,y0,x1,y1),
            feather_pixels=params.feather_pixels,max_padded_pixels=budget,check_active=check_active)
        owner=scopes.enter_context(artifacts.scratch.reserve(h*w*12+16384))
        path=owner.path('region-inpaint','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape)
        try:
            for y in range(0,h,256):check_active();output[y:y+256]=rgb[y:y+256]
            count=blend_candidate_region(output,rgb[y0:y1,x0:x1],restored,weight,x0,y0,check_active)
            proof=verify_candidate_preservation(rgb,output,target,protected,max_workspace_bytes=8*1048576,check_active=check_active)
            if count!=proof.changed_pixels:raise ValueError('REGION_INPAINT_PRESERVATION_INVALID')
            output.flush();owner.validate_file(path);output.flags.writeable=False
            check_active();verify_runtime();yield output;check_active();verify_runtime()
        finally:output._mmap.close()
