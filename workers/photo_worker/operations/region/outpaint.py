"""Native outpaint replays only the accepted explicit composition proposal."""
from photo_worker.contracts.base import canonical_bytes
from contextlib import contextmanager,ExitStack
import cv2
import numpy as np
from photo_worker.contracts.restoration import OutpaintParams
from photo_worker.contracts.composition import CompositionPlan,CompositionParams
from photo_worker.contracts.images import Canvas,MatrixTransform,Rect,TransformChain
from photo_worker.algorithms.model_delta import work_to_model,transfer_model_delta
from photo_worker.operations.image.outpaint import _required_outpaint_ram
from photo_worker.operations.semantic.candidate_blend import blend_candidate_region
from photo_worker.runtime.arrays import read_array
from .geometry_native import GeometryPixels


def outpaint_policy(effective,entry,model_lock_sha):
    stage,raw,source,target,policy=entry;params=OutpaintParams.model_validate_json(canonical_bytes(raw))
    requested=effective.extensions.composition;plan=policy.get('plan');configured=policy.get('params')
    if (requested is None or not requested.enabled or requested.outpaint_policy!='review'
            or stage.phase!='composition_render' or not isinstance(plan,CompositionPlan) or not isinstance(configured,CompositionParams)
            or plan.image!=stage.source or plan.disposition!='outpaint' or plan.outpaint_side!=params.side or plan.outpaint_pixels!=params.pixels
            or plan.source_width!=source.canvas.width or plan.source_height!=source.canvas.height
            or configured.structure!=plan.structure or configured.aspect_width!=requested.aspect[0] or configured.aspect_height!=requested.aspect[1]
            or configured.headroom_target!=requested.headroom or configured.allow_rotation!=(requested.rotate_limit_deg>0)
            or configured.max_rotation_degrees!=requested.rotate_limit_deg
            or params.prompt!=requested.outpaint_prompt or params.negative_prompt!=requested.outpaint_negative_prompt
            or params.seed!=requested.outpaint_seed or params.guidance_scale!=requested.outpaint_guidance_scale
            or params.model_lock_id!=model_lock_sha or params.feather_pixels!=16):
        raise ValueError('REGION_OUTPAINT_POLICY_MISMATCH')
    h,w=source.canvas.height,source.canvas.width
    ow=w+params.pixels if params.side in ('left','right') else w;oh=h+params.pixels if params.side in ('top','bottom') else h
    dx=params.pixels if params.side=='left' else 0;dy=params.pixels if params.side=='top' else 0
    canvas=Canvas(width=ow,height=oh,orientation=1,space_id=f'out-{params.side}-{source.canvas.source_sha256[:12]}',source_sha256=source.canvas.source_sha256)
    step=MatrixTransform(kind='matrix',from_canvas=source.canvas,to_canvas=canvas,invertibility='partial',
        valid_region=Rect(x=dx,y=dy,width=w,height=h),forward_matrix=[1.,0.,float(dx),0.,1.,float(dy),0.,0.,1.])
    if (plan.output_width!=ow or plan.output_height!=oh or target.canvas!=canvas
            or target.transform_chain!=TransformChain.create([*source.transform_chain.steps,step])
            or (source.alpha is None)!=(target.alpha is None)):
        raise ValueError('REGION_OUTPAINT_TRANSFORM_MISMATCH')
    return params


@contextmanager
def prepare_region_outpaint(artifacts,params,source,target,rgb,alpha,*,pool,backend,profile,verify_runtime,check_active,private_input=None):
    h,w=source.canvas.height,source.canvas.width;oh,ow=target.canvas.height,target.canvas.width
    ram=profile.get('resource_estimate',{}).get('ram_bytes')
    if pool is None or pool.lock.lock_id!=params.model_lock_id or profile.get('backend')!=backend:
        raise ValueError('REGION_OUTPAINT_INSTALLATION_REQUIRED')
    if type(ram) is not int or ram<max(2147483648,_required_outpaint_ram(oh,ow)):raise ValueError('REGION_OUTPAINT_RESOURCE_INVALID')
    verify_runtime();check_active()
    with ExitStack() as scopes:
        def mapped(ref,shape,dtype='float32'):
            artifacts.require_declared_input(ref);local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
            return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=dtype=='float32'))
        original=mapped(source.pixels,(h,w,3))
        from .private_sources import require_stage_source
        original_alpha=None if source.alpha is None else mapped(source.alpha,(h,w),'uint16')
        require_stage_source('image.outpaint',params.image,rgb,alpha,original,original_alpha,
            private_input=private_input,check_active=check_active)
        dx=params.pixels if params.side=='left' else 0;dy=params.pixels if params.side=='top' else 0
        initial=np.empty((oh,ow,3),np.float32);initial[dy:dy+h,dx:dx+w]=rgb
        if params.side=='left':initial[:,:dx]=rgb[:,:1]
        elif params.side=='right':initial[:,w:]=rgb[:,-1:]
        elif params.side=='top':initial[:dy]=rgb[:1]
        else:initial[h:]=rgb[-1:]
        mask=np.ones((oh,ow),bool);mask[dy:dy+h,dx:dx+w]=False
        model_input,_=work_to_model(initial)
        input512=cv2.resize(model_input,(512,512),interpolation=cv2.INTER_AREA if max(oh,ow)>512 else cv2.INTER_CUBIC)
        mask512=cv2.resize(mask.astype(np.uint8),(512,512),interpolation=cv2.INTER_NEAREST).astype(np.float32)
        engine=scopes.enter_context(pool.acquire(params.model_id,backend));check_active()
        generated=np.asarray(engine.generate(input512,mask512,prompt=params.prompt,negative_prompt=params.negative_prompt,
            seed=params.seed,steps=25,guidance_scale=params.guidance_scale,scheduler='ddim_v1'),np.float32)
        check_active()
        if generated.shape!=(512,512,3) or not np.isfinite(generated).all() or np.any(generated<0) or np.any(generated>1):raise ValueError('REGION_OUTPAINT_MODEL_OUTPUT_INVALID')
        model_output=cv2.resize(generated,(ow,oh),interpolation=cv2.INTER_CUBIC).clip(0,1).astype(np.float32)
        restored,_=transfer_model_delta(initial,model_input,model_output,1.)
        distance=cv2.distanceTransform(mask.astype(np.uint8),cv2.DIST_L2,3);weight=np.minimum(distance/params.feather_pixels,1).astype(np.float32)
        owner=scopes.enter_context(artifacts.scratch.reserve(oh*ow*16+16384));path=owner.path('region-outpaint','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(oh,ow,3))
        try:
            for y in range(0,oh,256):check_active();output[y:y+256]=initial[y:y+256]
            blend_candidate_region(output,initial,restored,weight,0,0,check_active)
            for y in range(0,h,256):
                check_active()
                if not np.array_equal(output[dy+y:dy+min(y+256,h),dx:dx+w].view(np.uint32),rgb[y:y+256].view(np.uint32)):
                    raise ValueError('REGION_OUTPAINT_PRESERVATION_INVALID')
            output.flush();owner.validate_file(path);output.flags.writeable=False
            output_alpha=None
            if alpha is not None:
                alpha_path=owner.path('region-outpaint-alpha','.npy')
                output_alpha=np.lib.format.open_memmap(alpha_path,mode='w+',dtype='<f4',shape=(oh,ow))
                for y in range(0,oh,256):check_active();output_alpha[y:y+256]=1
                for y in range(0,h,256):check_active();output_alpha[dy+y:dy+min(y+256,h),dx:dx+w]=alpha[y:y+256]
                output_alpha.flush();owner.validate_file(alpha_path);output_alpha.flags.writeable=False
            try:
                check_active();verify_runtime();yield GeometryPixels(output,output_alpha,target.canvas,target.transform_chain);check_active();verify_runtime()
            finally:
                if output_alpha is not None:output_alpha._mmap.close()
        finally:output._mmap.close()
