"""Native portrait layer execution with explicit legacy alpha policy."""
from uuid import uuid4
import numpy as np
import pytest
from test_portrait_detail import adapter_fixture
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.v2.render_detail import PinnedDetailProviderFactory
from photo_worker.operations.portrait.prepare_detail import detail_peak
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_layers import RenderLayer
from photo_worker.v2.render_layer_params import LoadedRenderLayer
from photo_worker.v2.render_resources import render_resources


@pytest.mark.parametrize('alpha_kind', ['float32', 'uint16'])
def test_detail_provider_preserves_alpha_and_protection_and_refuses_changed_source(tmp_path,alpha_kind):
    a,_=adapter_fixture(tmp_path)
    alpha=np.full(a.rgb.shape[:2],.5,np.float32) if alpha_kind=='float32' else np.full(a.rgb.shape[:2],10000,np.uint16)
    a.image=a.image.model_copy(update={'alpha':a.array(tmp_path,'render-alpha',alpha)})
    a.image_ref=a.add(a.image)
    values=a.resolved.model_dump(exclude={'hash'});values['params']['image']=a.image_ref
    from photo_worker.contracts.resolved import ResolvedStyle
    a.resolved=ResolvedStyle.create(**values)
    raw=dict(a.raw);raw.update(image=a.image_ref.model_dump(mode='json'),resolved=a.add(a.resolved).model_dump(mode='json'))
    protection=np.zeros(a.rgb.shape[:2],np.float32);protection[10:15,220:240]=1e-6
    protected=MaskRef(artifact=a.array(tmp_path,'render-protection',protection),canvas=a.image.canvas,
                      transform_chain=a.image.transform_chain,selector='manual',confidence=1,protected=True,model_lock_id=None)
    raw['protected']=[protected.model_dump(mode='json')]
    params=PortraitParams.model_validate_json(canonical_bytes(raw))
    layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='portrait.detail',operation_version='1',params=a.add(params),mask={})
    h,w=a.rgb.shape[:2];peak=detail_peak(w,h);retained=(20+4*(len(params.regions)+len(params.protected)))*h*w+1048576
    peak['ram_bytes']+=retained;peak['scratch_bytes']+=retained;peak.update(vram_bytes=0,output_bytes=0)
    profile=dict(backend='cpu',alpha_mode='normalize_uint16' if alpha_kind=='uint16' else 'require_float32',
                 resource_estimate=render_resources(w,h,mask_refs=0,mask_nodes=0,provider_peak=peak,alpha_mode='normalize_uint16' if alpha_kind=='uint16' else 'require_float32'))
    providers=PinnedDetailProviderFactory(runtime_sha256='c'*64,verify_runtime=lambda:None,validate_binding=lambda *args:None)(a,None,profile,lambda:None)
    current_alpha=alpha.astype(np.float32)/np.float32(65535) if alpha_kind=='uint16' else alpha
    current=AdjustmentPixels(a.rgb,current_alpha)
    item=LoadedRenderLayer(layer,params,'c'*64)
    with providers[0].prepare(item,current) as result:
        assert not np.array_equal(result.pixels.rgb,a.rgb)
        np.testing.assert_array_equal(result.pixels.rgb[protection>0],a.rgb[protection>0])
        np.testing.assert_array_equal(result.pixels.alpha.view(np.uint32),current_alpha.view(np.uint32))
    changed=a.rgb.copy();changed[0,0,0]+=np.float32(.01)
    with pytest.raises(ValueError,match='RENDER_DETAIL_CURRENT_PIXELS_CHANGED'):
        with providers[0].prepare(item,AdjustmentPixels(changed,current_alpha)):pytest.fail('changed source accepted')

    changed_alpha=current_alpha.copy();changed_alpha[0,0]=np.nextafter(changed_alpha[0,0],np.float32(1))
    with pytest.raises(ValueError,match='RENDER_DETAIL_CURRENT_PIXELS_CHANGED'):
        with providers[0].prepare(item,AdjustmentPixels(a.rgb,changed_alpha)):pytest.fail('changed alpha accepted')
    if alpha_kind=='uint16':
        strict=dict(profile,alpha_mode='require_float32')
        strict_provider=PinnedDetailProviderFactory(runtime_sha256='c'*64,verify_runtime=lambda:None,
            validate_binding=lambda *args:None)(a,None,strict,lambda:None)[0]
        with pytest.raises(ValueError,match='ARRAY_LAYOUT_INVALID'):
            with strict_provider.prepare(item,current):pytest.fail('implicit alpha conversion accepted')
