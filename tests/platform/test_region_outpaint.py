from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_image_inpaint import Artifacts,Pool,LOCK,context
from photo_worker.contracts.restoration import OutpaintParams
from photo_worker.operations.image.outpaint import execute
from photo_worker.operations.region.outpaint import prepare_region_outpaint
from photo_worker.runtime.scratch import ScratchSpace


def policy_fixture(tmp_path,side='left'):
    from photo_worker.contracts.composition import CompositionPlan,CompositionParams
    from photo_worker.contracts.images import Canvas,MatrixTransform,Rect,TransformChain
    a=Artifacts(tmp_path);source=a.contract(a.image_ref,None)
    params=OutpaintParams(image=a.image_ref,side=side,pixels=8,prompt='extend',seed=9,model_lock_id=LOCK,feather_pixels=16)
    h,w=source.canvas.height,source.canvas.width
    dx=8 if side=='left' else 0;dy=8 if side=='top' else 0
    ow=w+8 if side in ('left','right') else w;oh=h+8 if side in ('top','bottom') else h
    canvas=Canvas(width=ow,height=oh,orientation=1,space_id=f'out-{side}-{source.canvas.source_sha256[:12]}',source_sha256=source.canvas.source_sha256)
    step=MatrixTransform(kind='matrix',from_canvas=source.canvas,to_canvas=canvas,invertibility='partial',valid_region=Rect(x=dx,y=dy,width=w,height=h),forward_matrix=[1.,0.,float(dx),0.,1.,float(dy),0.,0.,1.])
    target=source.model_copy(update=dict(canvas=canvas,transform_chain=TransformChain.create([*source.transform_chain.steps,step])))
    configured=CompositionParams(structure=a.image_ref,aspect_width=4,aspect_height=3,headroom_target=.12,allow_rotation=True,max_rotation_degrees=5.)
    plan=CompositionPlan(image=a.image_ref,structure=a.image_ref,source_width=w,source_height=h,output_width=ow,output_height=oh,
        forward_matrix=step.forward_matrix,source_quad=[0.,0.,float(w),0.,float(w),float(h),0.,float(h)],rotation_degrees=0.,perspective_area_change=0.,retained_subject_fraction=1.,center_offset=0.,protected_points_preserved=True,
        disposition='outpaint',reasons=['no_valid_crop'],outpaint_side=side,outpaint_pixels=8)
    requested=SimpleNamespace(enabled=True,outpaint_policy='review',aspect=[4,3],headroom=.12,rotate_limit_deg=5.,outpaint_prompt='extend',outpaint_negative_prompt='',outpaint_seed=9,outpaint_guidance_scale=7.5)
    effective=SimpleNamespace(extensions=SimpleNamespace(composition=requested))
    stage=SimpleNamespace(operation='image.outpaint',phase='composition_render',source=a.image_ref)
    return effective,(stage,params.model_dump(mode='json'),source,target,dict(plan=plan,params=configured))


@pytest.mark.parametrize('side',['left','right','top','bottom'])
def test_outpaint_policy_accepts_only_exact_actual_extension(tmp_path,side):
    from photo_worker.operations.region.outpaint import outpaint_policy
    effective,entry=policy_fixture(tmp_path,side)
    assert outpaint_policy(effective,entry,LOCK).side==side


@pytest.mark.parametrize('change',['phase','plan_source','side','pixels','prompt','seed','lock','target'])
def test_outpaint_policy_refuses_relabelled_plan_or_reconfigured_execution(tmp_path,change):
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.operations.region.outpaint import outpaint_policy
    effective,entry=policy_fixture(tmp_path);stage,raw,source,target,evidence=entry
    if change=='phase':stage.phase='generate'
    elif change=='plan_source':evidence['plan']=evidence['plan'].model_copy(update={'image':ArtifactRef(artifact_id=uuid4(),sha256='f'*64)})
    elif change=='target':target=source
    elif change=='lock':raw['model_lock_id']='f'*64
    elif change=='side':raw['side']='right'
    elif change=='pixels':raw['pixels']=9
    elif change=='prompt':raw['prompt']='different'
    else:raw['seed']+=1
    with pytest.raises(ValueError):outpaint_policy(effective,(stage,raw,source,target,evidence),LOCK)


@pytest.mark.parametrize('side',['left','right','top','bottom'])
@pytest.mark.parametrize('backend',['cuda','mps'])
def test_native_outpaint_matches_original_and_keeps_source_bits(tmp_path,side,backend):
    a=Artifacts(tmp_path);a.require_declared_input=lambda ref:None
    params=OutpaintParams(image=a.image_ref,side=side,pixels=8,prompt='extend',seed=9,model_lock_id=LOCK,feather_pixels=16)
    a.raw=params.model_dump(mode='json');source=a.contract(a.image_ref,None)
    def generated(rgb,mask,**kwargs):
        assert kwargs==dict(prompt='extend',negative_prompt='',seed=9,steps=25,guidance_scale=7.5,scheduler='ddim_v1')
        return np.where(mask[...,None]>0,.7,rgb).astype(np.float32)
    class NativePool(Pool):
        @contextmanager
        def acquire(self,model,selected_backend):
            assert model=='sd15_inpaint' and selected_backend==backend;yield self.engine
    baseline=Pool(model='sd15_inpaint');baseline.engine.generate=generated
    native=NativePool(model='sd15_inpaint');native.engine.generate=generated
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,1_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch;execute(context(a),a,baseline,'cuda')
        expected=a.output.copy();target=next(value for role,value in a.staged if role=='working_image')
        with prepare_region_outpaint(a,params,source,target,a.source,None,pool=native,backend=backend,
                profile={'backend':backend,'resource_estimate':{'ram_bytes':2<<30}},verify_runtime=lambda:None,check_active=lambda:None) as actual:
            np.testing.assert_array_equal(actual.rgb.view(np.uint32),expected.view(np.uint32))
            assert actual.canvas==target.canvas and actual.transform_chain==target.transform_chain
        assert not list(scratch.directory.iterdir())
