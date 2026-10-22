"""Actual native detail + authored composition; analytic blend oracle, not photo quality."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from test_portrait_detail import adapter_fixture
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.operations.portrait.prepare_detail import detail_peak
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_composition import compose_render_pixels
from photo_worker.v2.render_detail import PinnedDetailProviderFactory
from photo_worker.v2.render_layer_params import LoadedRenderLayer
from photo_worker.v2.render_layers import RenderLayer
from photo_worker.v2.render_resources import render_resources


def independent_blend(source, proposal, mask, opacity, blend, protected):
    """Separate high precision formula, no production color/blending helpers."""
    s=source.astype(np.float64)**2.4;p=proposal.astype(np.float64)**2.4
    b=p if blend=='normal' else s*p if blend=='multiply' else 1-(1-s)*(1-p)
    weight=mask.astype(np.float64)*opacity;weight[protected>0]=0
    result=np.maximum(s+(b-s)*weight[...,None],0)**(1/2.4)
    result=result.astype(np.float32);result[weight==0]=source[weight==0]
    return result


@pytest.mark.parametrize('has_alpha',[False,True])
@pytest.mark.parametrize('kind',['teeth','sclera','shine','under_eye','cloth','glasses'])
@pytest.mark.parametrize('blend',['normal','multiply','screen'])
@pytest.mark.parametrize('tile',[256,512])
def test_actual_detail_composes_against_independent_oracle_and_preserves_ownership(tmp_path,kind,blend,tile,has_alpha):
    a,_=adapter_fixture(tmp_path);alpha=np.full(a.rgb.shape[:2],.375,np.float32) if has_alpha else None;a.image=a.image.model_copy(update={'alpha':None if alpha is None else a.array(tmp_path,'composition-alpha',alpha)});a.image_ref=a.add(a.image)
    from photo_worker.contracts.resolved import ResolvedStyle
    body=a.resolved.model_dump(exclude={'hash'});body['params']['image']=a.image_ref;a.resolved=ResolvedStyle.create(**body)
    protection=np.zeros(a.rgb.shape[:2],np.float32);protection[10:15,220:240]=np.float32(1e-6)
    ref=MaskRef(artifact=a.array(tmp_path,'composition-protection',protection),canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='manual',confidence=1,protected=True,model_lock_id=None)
    raw=dict(a.raw);raw.update(image=a.image_ref.model_dump(mode='json'),resolved=a.add(a.resolved).model_dump(mode='json'),protected=[ref.model_dump(mode='json')]);raw['regions'][0].update(kind=kind,confirmed=kind in ('cloth','glasses'))
    params=PortraitParams.model_validate_json(canonical_bytes(raw));layer=RenderLayer(id=uuid4(),enabled=True,opacity=.375,blend=blend,operation='portrait.detail',operation_version='1',params=a.add(params),mask={});item=LoadedRenderLayer(layer,params,'c'*64)
    h,w=a.rgb.shape[:2];peak=detail_peak(w,h);retained=(20+4*(len(params.regions)+len(params.protected)))*h*w+1048576;peak['ram_bytes']+=retained;peak['scratch_bytes']+=retained;peak.update(vram_bytes=0,output_bytes=0)
    profile=dict(backend='cpu',alpha_mode='require_float32',resource_estimate=render_resources(w,h,mask_refs=0,mask_nodes=0,provider_peak=peak))
    providers=PinnedDetailProviderFactory(runtime_sha256='c'*64,verify_runtime=lambda:None,validate_binding=lambda *args:None)(a,None,profile,lambda:None)
    source=a.rgb.copy();before=source.view(np.uint32).copy();mask=np.broadcast_to(np.linspace(0,1,w,dtype=np.float32),(h,w)).copy();mask[:,256]=0
    masks=SimpleNamespace(layer=lambda _,x,y,ww,hh:mask[y:y+hh,x:x+ww],protection=lambda x,y,ww,hh:protection[y:y+hh,x:x+ww]);root=tmp_path/'native-composition-scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,16<<20,min_free_bytes=0) as space:
        a.scratch=space;original=AdjustmentPixels(source,alpha)
        with providers[0].prepare(item,original) as output:proposal=np.array(output.pixels.rgb,copy=True)
        expected=independent_blend(source,proposal,mask,layer.opacity,blend,protection)
        @contextmanager
        def actual_proposal(loaded,current):
            with providers[0].prepare(loaded,current) as output:yield output
        with compose_render_pixels(original,original,(item,),masks,space,actual_proposal,color=a.image.color,
                max_tile_bytes=tile*tile*256,tile_size=tile) as result:
            np.testing.assert_allclose(result.pixels.rgb,expected,rtol=0,atol=2/65535)
            exact=(protection>0)|(mask==0)
            np.testing.assert_array_equal(result.pixels.rgb[exact].view(np.uint32),before[exact])
            assert result.pixels.generated is None
            if alpha is None:assert result.pixels.alpha is None
            else:np.testing.assert_array_equal(result.pixels.alpha.view(np.uint32),alpha.view(np.uint32))
            changed=np.any(result.pixels.rgb.view(np.uint32)!=before,axis=-1)
            assert result.changes.changed_pixels==np.count_nonzero(changed)
            np.testing.assert_array_equal(np.load(result.changes.path,allow_pickle=False),changed.astype(np.float32))
            assert len(result.pixels.effects)==1 and result.pixels.effects[0].operation=='portrait.detail'
        np.testing.assert_array_equal(source.view(np.uint32),before)
        assert not list(space.directory.iterdir())
