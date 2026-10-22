"""Exact native seeded SD1.5 stages, distinguished by server-proven Apply phase."""
from photo_worker.contracts.base import canonical_bytes
from contextlib import contextmanager,ExitStack
import cv2
import numpy as np
from photo_worker.contracts.restoration import GenerationParams
from photo_worker.operations.image.generate import _square_roi
from photo_worker.operations.image.inpaint import _required_frame_ram,_roi_pixel_budget
from photo_worker.operations.semantic.candidate_blend import blend_candidate_region
from photo_worker.operations.semantic.candidate_preservation import verify_candidate_preservation
from photo_worker.algorithms.model_delta import work_to_model,transfer_model_delta
from photo_worker.runtime.arrays import read_array


def generation_entry(effective,verified,phase,model_lock_sha):
    if phase not in ('background','generate'):raise ValueError('REGION_GENERATION_PHASE_INVALID')
    requested=effective.extensions.generation
    enabled=requested is not None and requested.enabled
    if phase=='background':enabled=enabled and effective.extensions.background is not None and effective.extensions.background.mode=='generate'
    entries=[entry for entry in verified if entry[0].operation=='image.generate' and entry[0].phase==phase]
    if len(entries)!=int(enabled):raise ValueError('REGION_GENERATION_STAGE_COVERAGE_MISMATCH')
    if not entries:return None
    _,raw,source,target,_=entries[0];params=GenerationParams.model_validate_json(canonical_bytes(raw))
    if (params.model_id!=requested.model or params.model_lock_id!=model_lock_sha
            or params.prompt!=requested.prompt or params.negative_prompt!=requested.negative_prompt
            or params.seed!=requested.seed or params.steps!=requested.steps or params.guidance_scale!=requested.guidance_scale
            or params.strength!=requested.strength or params.feather_pixels!=8
            or source.canvas!=target.canvas or source.transform_chain!=target.transform_chain or source.alpha!=target.alpha
            or any(mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain for mask in [params.mask,*params.protected])
            or phase=='background' and not any(mask.selector=='person' and mask.protected for mask in params.protected)):
        raise ValueError('REGION_GENERATION_POLICY_MISMATCH')
    return params,source


@contextmanager
def prepare_region_generation(artifacts,effective,verified,phase,rgb,alpha,*,pool,backend,model_lock_sha,profile,verify_runtime,check_active,private_input=None):
    entry=generation_entry(effective,verified,phase,model_lock_sha)
    if entry is None:yield rgb;return
    params,image=entry;h,w=image.canvas.height,image.canvas.width
    ram=profile.get('resource_estimate',{}).get('ram_bytes')
    if pool is None or pool.lock.lock_id!=model_lock_sha or profile.get('backend')!=backend:
        raise ValueError('REGION_GENERATION_INSTALLATION_REQUIRED')
    if type(ram) is not int or ram<max(2147483648,_required_frame_ram(h,w)):raise ValueError('REGION_GENERATION_RESOURCE_INVALID')
    verify_runtime();check_active()
    with ExitStack() as scopes:
        def mapped(ref,shape,dtype='float32'):
            artifacts.require_declared_input(ref);local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
            return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=dtype=='float32'))
        original=mapped(image.pixels,(h,w,3))
        from .private_sources import require_stage_source
        original_alpha=None if image.alpha is None else mapped(image.alpha,(h,w),'uint16')
        require_stage_source('image.generate',params.image,rgb,alpha,original,original_alpha,
            private_input=private_input,check_active=check_active)
        target=np.asarray(mapped(params.mask.artifact,(h,w)))>=.5;protected=np.zeros((h,w),bool)
        for mask in params.protected:
            values=mapped(mask.artifact,(h,w))
            for y in range(0,h,256):check_active();protected[y:y+256]|=values[y:y+256]>0
        effective_mask=target&~protected;x0,y0,x1,y1=_square_roi(effective_mask)
        if (x1-x0)*(y1-y0)>_roi_pixel_budget(ram,h,w):raise ValueError('REGION_GENERATION_RESOURCE_INVALID')
        roi=np.asarray(rgb[y0:y1,x0:x1],np.float32);mask=effective_mask[y0:y1,x0:x1]
        model_input,_=work_to_model(roi)
        input512=cv2.resize(model_input,(512,512),interpolation=cv2.INTER_AREA if max(roi.shape[:2])>512 else cv2.INTER_CUBIC)
        mask512=cv2.resize(mask.astype(np.uint8),(512,512),interpolation=cv2.INTER_NEAREST).astype(np.float32)
        engine=scopes.enter_context(pool.acquire(params.model_id,backend));check_active()
        generated=np.asarray(engine.generate(input512,mask512,prompt=params.prompt,negative_prompt=params.negative_prompt,
            seed=params.seed,steps=params.steps,guidance_scale=params.guidance_scale,scheduler='ddim_v1'),np.float32)
        check_active()
        if generated.shape!=(512,512,3) or not np.isfinite(generated).all() or np.any(generated<0) or np.any(generated>1):raise ValueError('REGION_GENERATION_MODEL_OUTPUT_INVALID')
        model_output=cv2.resize(generated,(x1-x0,y1-y0),interpolation=cv2.INTER_CUBIC).clip(0,1).astype(np.float32)
        restored,_=transfer_model_delta(roi,model_input,model_output,params.strength)
        distance=cv2.distanceTransform(mask.astype(np.uint8),cv2.DIST_L2,3)
        weight=np.minimum(distance/params.feather_pixels,1).astype(np.float32);weight[protected[y0:y1,x0:x1]]=0
        owner=scopes.enter_context(artifacts.scratch.reserve(h*w*12+16384));path=owner.path('region-generation','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=rgb.shape)
        try:
            for y in range(0,h,256):check_active();output[y:y+256]=rgb[y:y+256]
            blend_candidate_region(output,roi,restored,weight,x0,y0,check_active)
            verify_candidate_preservation(rgb,output,target,protected,max_workspace_bytes=8*1048576,check_active=check_active)
            output.flush();owner.validate_file(path);output.flags.writeable=False
            check_active();verify_runtime();yield output;check_active();verify_runtime()
        finally:output._mmap.close()
