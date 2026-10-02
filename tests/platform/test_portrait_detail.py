import numpy as np
import pytest
from uuid import uuid4
from photo_worker.algorithms.portrait_detail import apply_region
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import PortraitDetail
from photo_worker.operations.portrait.detail import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_image_color_adapter import Artifacts


def fixture():
    y,x=np.mgrid[:64,:64];rgb=np.stack([.35+x/256,.32+y/320,.3+x/512],axis=-1).astype(np.float32);mask=np.zeros((64,64),np.float32);mask[16:48,16:48]=1;protected=np.zeros_like(mask);return rgb,mask,protected


def test_teeth_bounds_and_outside_exact():
    rgb,mask,protected=fixture();out,count,ev,chroma,reason=apply_region(rgb,mask,'teeth',1,protected)
    assert reason is None and count==32*32 and ev==.2 and chroma==.25
    assert np.array_equal(out[mask==0],rgb[mask==0]) and np.mean(out[mask==1])>np.mean(rgb[mask==1])


def test_protected_pixels_remain_exact_and_glasses_is_review():
    rgb,mask,protected=fixture();protected[24:40,24:40]=1;out,*_=apply_region(rgb,mask,'shine',1,protected)
    assert np.array_equal(out[protected==1],rgb[protected==1])
    out,count,ev,chroma,reason=apply_region(rgb,mask,'glasses',1,protected)
    assert reason=='review_required' and count==0 and np.array_equal(out,rgb)


def adapter_fixture(root):
    a=Artifacts(root,False);body=a.resolved.model_dump(exclude={'hash'});body['strength']=1.;extensions=a.resolved.modules.extensions.model_copy(update={'portrait_detail':PortraitDetail(enabled=True,teeth_strength=.8,shine_strength=.5,under_eye_strength=.4,glasses_policy='review')});body['modules']['extensions']=extensions;a.resolved=ResolvedStyle.create(**body);resolved=a.add(a.resolved)
    values=np.zeros(a.rgb.shape[:2],np.float32);values[8:30,200:400]=1;mask_ref=a.array(root,'teeth-mask',values);mask=MaskRef(artifact=mask_ref,canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=False)
    a.raw={'image':a.image_ref.model_dump(mode='json'),'resolved':resolved.model_dump(mode='json'),'regions':[{'region_id':'teeth-1','kind':'teeth','mask':mask.model_dump(mode='json'),'confirmed':False,'strength':1}],'protected':[]}
    ctx=context_for(a.image_ref,'portrait.detail');ctx.lease.command.inputs=[a.image_ref,a.image.pixels,a.image.alpha,resolved,mask_ref];ctx.lease.execution_profile.update(resource_estimate={'ram_bytes':1073741824},tile_size=512)
    return a,ctx


def test_portrait_detail_adapter_uses_declared_explicit_region(tmp_path):
    a,ctx=adapter_fixture(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==3 and a.staged[1][1].regions[0].applied
    assert not np.array_equal(a.output,a.rgb)


def test_portrait_detail_adapter_rejects_undeclared_region(tmp_path):
    a,ctx=adapter_fixture(tmp_path);ctx.lease.command.inputs.pop()
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.uploads
