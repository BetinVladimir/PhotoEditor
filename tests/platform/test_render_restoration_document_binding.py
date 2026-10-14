"""The authored decision and executed native Params must be one snapshot."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.style import Restoration
from photo_worker.contracts.dust import DustRepairParams
from photo_worker.contracts.restoration import DenoiseParams,UpscaleParams
from photo_worker.v2.render_dust import prepare_authored_dust
from photo_worker.v2.render_denoise import prepare_authored_denoise
from photo_worker.v2.render_upscale import prepare_authored_upscale


@pytest.mark.parametrize('operation',['dust','denoise','upscale'])
def test_authored_restoration_rejects_document_substitution_before_image_io(operation):
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    other=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    reference=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    style=Restoration(denoise='scunet_color_real_psnr',denoise_strength=.5,
        upscale='2x',sensor_dust='review')
    if operation=='dust':params=DustRepairParams(image=source)
    elif operation=='denoise':params=DenoiseParams(image=source,strength=.5,model_lock_id='d'*64)
    else:params=UpscaleParams(image=source,scale=2,model_id='realesrgan_x2',model_lock_id='d'*64)
    calls=[]
    def contract(ref,model):
        assert ref==reference and model is type(params), 'Read source before detecting changed Params'
        calls.append(ref)
        return params if len(calls)==1 else params.model_copy(update={'image':other})
    def forbidden(*args,**kwargs):raise AssertionError('Pixel/model IO before detecting changed Params')
    artifacts=SimpleNamespace(contract=contract,input=forbidden)
    options=dict(execution_profile={'backend':'cpu'},verify_runtime=lambda:None)
    if operation=='dust':context=prepare_authored_dust(artifacts,source,style,reference,None,**options)
    else:
        options.update(backend='cpu',provider_peak={})
        pool=SimpleNamespace(acquire=forbidden)
        if operation=='denoise':context=prepare_authored_denoise(artifacts,source,style,reference,None,pool,**options)
        else:context=prepare_authored_upscale(artifacts,source,style,reference,None,pool,mask_refs=(),**options)
    with pytest.raises(ValueError,match='DOCUMENT_CHANGED'):
        with context:raise AssertionError('Unexpected stage execution')
    assert len(calls)==2


@pytest.mark.parametrize('operation',['dust','denoise','detail'])
def test_layer_native_boundary_rejects_changed_params(operation):
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.v2.render_detail import PinnedDetailLayerProposal
    from photo_worker.v2.render_dust import PinnedDustLayerProposal
    from photo_worker.v2.render_denoise import PinnedDenoiseLayerProposal
    from photo_worker.v2.render_layers import RenderLayer
    from photo_worker.v2.render_layer_params import LoadedRenderLayer
    source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    other=ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
    reference=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    if operation=='dust':params=DustRepairParams(image=source);name='sensor.dust_repair'
    elif operation=='denoise':
        params=DenoiseParams(image=source,strength=.5,model_lock_id='d'*64);name='image.denoise'
    else:params=PortraitParams(image=source,resolved=other,regions=[]);name='portrait.detail'
    calls=[]
    def contract(ref,model):
        assert ref==reference and model is type(params)
        calls.append(ref)
        return params if len(calls)==1 else params.model_copy(update={'image':other})
    def forbidden(*args,**kwargs):raise AssertionError('Premature pixel/model IO')
    artifacts=SimpleNamespace(contract=contract,input=forbidden)
    options=dict(execution_profile={'backend':'cpu'},runtime_sha256='e'*64)
    if operation=='dust':callback=PinnedDustLayerProposal(artifacts,verify_runtime=lambda:None,**options)
    elif operation=='detail':callback=PinnedDetailLayerProposal(artifacts,**options)
    else:callback=PinnedDenoiseLayerProposal(artifacts,SimpleNamespace(acquire=forbidden),
        backend='cpu',provider_peak={},verify_runtime=lambda:None,**options)
    layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation=name,
        operation_version='1',params=reference,mask={'type':'all'})
    item=LoadedRenderLayer(layer,params,'e'*64)
    with pytest.raises(ValueError,match='DOCUMENT_CHANGED'):
        with callback(item,None):raise AssertionError('Unexpected layer execution')
    assert len(calls)==2
