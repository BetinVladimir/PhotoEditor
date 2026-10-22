"""Real working→linear premult resample→ICC→WebP adapter path; authored only."""
from contextlib import contextmanager
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.color.icc import ICCRegistry,LittleCMS
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.images import Canvas,ColorSpace,WorkingImage,TransformChain
from photo_worker.contracts.media import MediaParams
from photo_worker.operations.media import derivative
from photo_worker.operations.export.encode import _resize_rgba
from photo_worker.runtime.scratch import ScratchSpace


@pytest.mark.parametrize('encoding,profile_id',[('linear','linear_srgb_v1'),('gamma24','work_rec2020_gamma24_v1')])
@pytest.mark.parametrize('opaque',[False,True])
@pytest.mark.parametrize('legacy',[False,True])
def test_media_adapter_uses_declared_working_encoding_for_translucency(tmp_path,monkeypatch,encoding,profile_id,opaque,legacy):
    registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')
    source_profile=registry.resolve(profile_id);target=registry.resolve('srgb_v1')
    def ref(raw):return ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    # Two source contributors per output pixel; fractional alpha makes a
    # gamma24 assumption observably different for the valid linear source.
    rgb=np.empty((2,640,3),np.float32);rgb[:,::2]=.1;rgb[:,1::2]=.9
    alpha=np.empty((2,640),np.float32);alpha[:,::2]=.25;alpha[:,1::2]=.75
    if opaque:alpha.fill(1)
    paths={};refs=[]
    for name,value in [('rgb',rgb),('alpha',alpha)]:
        path=tmp_path/(name+'.npy');np.save(path,value,allow_pickle=False)
        pointer=ref(path.read_bytes());refs.append(pointer);paths[pointer.artifact_id]=path
    image_ref=ref(b'working');image=WorkingImage(pixels=refs[0],alpha=refs[1],
        canvas=Canvas(width=640,height=2,orientation=1,space_id='source',source_sha256='c'*64),
        color=ColorSpace(profile_id=profile_id,icc_sha256=source_profile.sha256,encoding=encoding),
        dtype='float32',transform_chain=TransformChain.create([]))
    params=MediaParams(image=image_ref,variant='thumb320',profile_hash=derivative.media_profile_hash('thumb320',target.sha256,legacy=legacy))
    observed={};original_write=derivative._write
    def write(path,pixels,opacity,*args,**kwargs):
        observed.update(rgb=pixels.copy(),alpha=opacity.copy())
        return original_write(path,pixels,opacity,*args,**kwargs)
    monkeypatch.setattr(derivative,'_write',write)
    class Artifacts:
        def params(self,_):return params
        def contract(self,*_):return image
        @contextmanager
        def input(self,pointer,**_):yield SimpleNamespace(path=paths[pointer.artifact_id])
        def stage(self,path,reservation,role,media_type):
            assert role=='derivative' and media_type=='image/webp'
            return ref(path.read_bytes())
        def stage_contract(self,value,role):
            observed['manifest']=value
            return ref(b'manifest')
    artifacts=Artifacts()
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cpu'},
        command=SimpleNamespace(inputs=[image_ref,*refs])),check_active=lambda:None,progress=lambda *_:None)
    (tmp_path/'scratch').mkdir()
    with ScratchSpace(tmp_path/'scratch',uuid4(),uuid4(),1,8*1024**2,min_free_bytes=0) as scratch:
        artifacts.scratch=scratch
        result=derivative.execute(ctx,artifacts,registry,'media.thumbnail')
    resized,opacity=_resize_rgba(rgb,alpha,320,1,encoding='gamma24' if legacy else encoding)
    expected=LittleCMS().convert(np.round(resized*65535).astype(np.uint16),source_profile.data,target.data,preserve_identity=not legacy).astype(np.float32)/65535
    assert np.array_equal(observed['rgb'],expected)
    assert np.array_equal(observed['alpha'],opacity)
    assert observed['manifest'].alpha and len(result.outputs)==2


@pytest.mark.parametrize('alpha',[False,True])
@pytest.mark.parametrize('corruption',['none','icc','exif','xmp','layout'])
def test_physical_derivative_requires_exact_icc_and_stripped_metadata(tmp_path,alpha,corruption):
    from PIL import Image
    profile=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1')
    path=tmp_path/('preview.webp' if alpha else 'preview.jpg')
    mode='RGBA' if alpha else 'RGB'
    if corruption=='layout':mode='RGB' if alpha else 'L'
    image=Image.new(mode,(8,6))
    options={'icc_profile':profile.data}
    if corruption=='icc':options['icc_profile']=b'wrong-profile'
    if corruption=='exif':
        exif=Image.Exif();exif[0x010f]='private camera metadata';options['exif']=exif
    if corruption=='xmp':options['xmp']=b'<private/>'
    # JPEG XMP writer support is not consistent between Pillow versions;
    # use WebP for the physical XMP negative rather than mocking metadata.
    if corruption=='xmp' and not alpha:pytest.skip('Pillow JPEG writer has no portable XMP option; WebP physical case covers XMP')
    image.save(path,**options)
    if corruption=='none':
        derivative._verify_derivative(path,8,6,alpha,profile.sha256,
            expected_alpha=np.zeros((6,8),np.float32) if alpha else None)
    else:
        with pytest.raises(ValueError,match='DERIVATIVE_VERIFY_FAILED'):
            derivative._verify_derivative(path,8,6,alpha,profile.sha256,
                expected_alpha=np.zeros((6,8),np.float32) if alpha else None)


@pytest.mark.parametrize('value',[1.,.9999,0.,.75,np.nextafter(np.float32(1),np.float32(0)),float('nan')])
def test_rgb_webp_alpha_omission_requires_quantized_allopaque_support(tmp_path,value):
    from PIL import Image
    profile=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1')
    path=tmp_path/'rgb.webp'
    Image.new('RGB',(513,257),(20,40,60)).save(path,icc_profile=profile.data)
    alpha=np.ones((257,513),np.float32);alpha[-1,-1]=value
    if np.isfinite(value) and np.round(value*255)==255:
        derivative._verify_derivative(path,513,257,True,profile.sha256,expected_alpha=alpha)
    else:
        with pytest.raises(ValueError,match='DERIVATIVE_VERIFY_FAILED'):
            derivative._verify_derivative(path,513,257,True,profile.sha256,expected_alpha=alpha)


def test_opaque_support_checks_cancel_between_tiles():
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        derivative._presentation_alpha_matches(np.ones((257,513),np.float32),513,257,active)


@pytest.mark.parametrize('corrupt',[False,True])
def test_actual_rgba_webp_alpha_must_match_written_quantization(tmp_path,corrupt):
    from PIL import Image
    profile=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1')
    alpha=np.tile(np.linspace(0,1,513,dtype=np.float32),(257,1))
    pixels=np.zeros((257,513,4),np.uint8);pixels[...,:3]=80
    pixels[...,3]=derivative._alpha8(alpha)
    if corrupt:pixels[-1,-1,3]=100
    path=tmp_path/'rgba.webp';Image.fromarray(pixels).save(path,lossless=True,icc_profile=profile.data)
    if corrupt:
        with pytest.raises(ValueError,match='DERIVATIVE_VERIFY_FAILED'):
            derivative._verify_derivative(path,513,257,True,profile.sha256,expected_alpha=alpha)
    else:
        derivative._verify_derivative(path,513,257,True,profile.sha256,expected_alpha=alpha)


@pytest.mark.parametrize('alpha',[False,True])
def test_truncated_real_presentation_payload_refused(tmp_path,alpha):
    from PIL import Image
    profile=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1')
    path=tmp_path/('truncated.webp' if alpha else 'truncated.jpg')
    pixels=np.random.default_rng(21).integers(0,256,(96,128,4 if alpha else 3),dtype=np.uint8)
    Image.fromarray(pixels).save(path,icc_profile=profile.data)
    raw=path.read_bytes();path.write_bytes(raw[:len(raw)//2])
    with pytest.raises((OSError,ValueError)):
        derivative._verify_derivative(path,128,96,alpha,profile.sha256)
