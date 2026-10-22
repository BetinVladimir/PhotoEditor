from pathlib import Path
import numpy as np
from PIL import Image
import pytest
import pyvips
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.asset.verify import AssetError,verify_decode
from photo_worker.operations.asset.decode import icc_to_work
from photo_worker.operations.asset.inspect import _pixels

ROOT=Path(__file__).resolve().parents[2]

@pytest.fixture
def registry():
    return ICCRegistry(ROOT/'resources/icc')

def test_missing_profile_requires_explicit_assumption(tmp_path,registry):
    path=tmp_path/'unprofiled.png'
    Image.new('RGB',(12,10),(128,128,128)).save(path)
    source=verify_decode(path)
    with pytest.raises(AssetError,match='INPUT_PROFILE_REQUIRED'):
        icc_to_work(source,tmp_path/'no.npy',registry)
    result=icc_to_work(source,tmp_path/'working.npy',registry,assume_profile='srgb_v1')
    rgb=np.load(result.rgb_path)
    assert rgb.dtype==np.float32 and rgb.shape==(10,12,3)
    assert result.profile_id=='work_rec2020_gamma24_v1'
    # sRGB-encoded 128 does not equal Rec2020 gamma-2.4 encoded 128.
    assert abs(float(rgb[0,0,0])-128/255)>.01

def test_orientation_once(tmp_path,registry):
    path=tmp_path/'oriented.jpg'
    image=Image.new('RGB',(20,10),(100,120,150))
    exif=Image.Exif();exif[274]=6
    image.save(path,icc_profile=registry.resolve('srgb_v1').data,exif=exif)
    source=verify_decode(path)
    result=icc_to_work(source,tmp_path/'oriented.npy',registry)
    assert (result.width,result.height)==(10,20)
    assert np.load(result.rgb_path).shape==(20,10,3)

def test_png16_keeps_precision_and_tile_equivalence(tmp_path,registry):
    pixels=np.random.default_rng(3).integers(0,65536,size=(37,601,3),dtype=np.uint16)
    image=pyvips.Image.new_from_memory(pixels.data,601,37,3,'ushort').copy(interpretation='rgb16')
    image.set_type(pyvips.GValue.blob_type,'icc-profile-data',registry.resolve('work_rec2020_gamma24_v1').data)
    path=tmp_path/'precision.png';image.pngsave(str(path),bitdepth=16)
    source=verify_decode(path)
    a=icc_to_work(source,tmp_path/'a.npy',registry,tile=256)
    b=icc_to_work(source,tmp_path/'b.npy',registry,tile=512)
    one,two=np.load(a.rgb_path),np.load(b.rgb_path)
    assert np.array_equal(one,two)
    assert np.array_equal(np.round(one*65535).astype(np.uint16),pixels)
    with pytest.raises(AssetError,match='OUTPUT_ALREADY_EXISTS'):
        icc_to_work(source,tmp_path/'a.npy',registry)

def test_alpha_preserved_and_analyze_rejected(tmp_path,registry):
    path=tmp_path/'alpha.png'
    pixels=np.zeros((12,14,4),dtype=np.uint8);pixels[...,:3]=100;pixels[...,3]=np.arange(14)*18
    Image.fromarray(pixels).save(path,icc_profile=registry.resolve('srgb_v1').data)
    source=verify_decode(path)
    with pytest.raises(AssetError,match='ANALYZE_ALPHA_UNSUPPORTED'):
        icc_to_work(source,tmp_path/'no.npy',registry,analyze=True)
    result=icc_to_work(source,tmp_path/'alpha.npy',registry)
    assert np.array_equal(np.load(result.alpha_path),pixels[...,3].astype(np.uint16)*257)


def test_profiled_png16_roundtrip_keeps_hidden_rgb_and_all_alpha_codes(tmp_path,registry):
    # The baseline must preserve straight RGB even at alpha zero. Cover every
    # uint16 alpha code rather than a few opaque/transparent endpoints.
    codes=np.arange(65536,dtype=np.uint16).reshape(256,256)
    pixels=np.stack((codes,np.flip(codes,axis=1),np.full_like(codes,12345),codes),axis=-1)
    raster=pyvips.Image.new_from_memory(pixels.data,256,256,4,'ushort').copy(interpretation='rgb16')
    raster.set_type(pyvips.GValue.blob_type,'icc-profile-data',registry.resolve('work_rec2020_gamma24_v1').data)
    path=tmp_path/'straight.png';raster.pngsave(str(path),bitdepth=16)
    decoded=icc_to_work(verify_decode(path),tmp_path/'rgb.npy',registry)
    assert np.array_equal(np.load(decoded.alpha_path),codes)
    assert np.array_equal(np.round(np.load(decoded.rgb_path)*65535).astype(np.uint16),pixels[...,:3])

def test_reject_cmyk_multipage_and_spoof(tmp_path):
    path=tmp_path/'cmyk.jpg';Image.new('CMYK',(10,10)).save(path)
    with pytest.raises(AssetError,match='RGB_8_OR_16_REQUIRED'):verify_decode(path)
    path=tmp_path/'multi.tiff';Image.new('RGB',(10,10)).save(path,save_all=True,append_images=[Image.new('RGB',(10,10))])
    with pytest.raises(AssetError,match='MULTIPAGE_UNSUPPORTED'):verify_decode(path)
    path=tmp_path/'fake.jpg';path.write_text('<svg/>')
    with pytest.raises(AssetError,match='FORMAT_UNSUPPORTED'):verify_decode(path)


def test_reject_magic_suffix_mismatch_and_pixel_bomb_before_decode(tmp_path):
    jpeg_as_png=tmp_path/'forged.png'
    Image.new('RGB',(8,8)).save(jpeg_as_png,format='JPEG')
    with pytest.raises(AssetError,match='FORMAT_SUFFIX_MISMATCH'):
        verify_decode(jpeg_as_png)
    png_as_jpeg=tmp_path/'forged.jpg'
    Image.new('RGB',(8,8)).save(png_as_jpeg,format='PNG')
    with pytest.raises(AssetError,match='FORMAT_SUFFIX_MISMATCH'):
        verify_decode(png_as_jpeg)
    for width,height in ((200000001,1),(20000,20000)):
        with pytest.raises(AssetError,match='PIXEL_LIMIT_EXCEEDED'):
            _pixels(width,height)


def test_cancel_between_tiles_removes_unpublished_buffers(tmp_path,registry):
    path=tmp_path/'cancel.png'
    Image.new('RGBA',(600,600),(100,120,140,80)).save(path,icc_profile=registry.resolve('srgb_v1').data)
    source=verify_decode(path)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        icc_to_work(source,tmp_path/'result.npy',registry,tile=256,check_active=active)
    assert list(tmp_path.iterdir())==[path]


@pytest.mark.parametrize('dtype',[np.uint8,np.uint16])
def test_tiff_explicit_straight_alpha_is_preserved_without_unpremultiplication(tmp_path,registry,dtype):
    import tifffile
    from photo_worker.color.icc import LittleCMS
    maximum=np.iinfo(dtype).max
    rgba=np.zeros((5,7,4),dtype=dtype)
    rgba[...,:3]=np.array([maximum//3,maximum//2,maximum],dtype=dtype)
    rgba[...,3]=np.resize(np.array([0,1,maximum//4,maximum//2,maximum],dtype=dtype),(5,7))
    path=tmp_path/'straight.tiff'
    tifffile.imwrite(path,rgba,photometric='rgb',extrasamples='unassalpha')
    result=icc_to_work(verify_decode(path),tmp_path/'working.npy',registry,assume_profile='srgb_v1')
    samples=rgba.astype(np.uint16)*(257 if dtype==np.uint8 else 1)
    expected=LittleCMS().convert(samples[...,:3],registry.resolve('srgb_v1').data,registry.resolve('work_rec2020_gamma24_v1').data).astype(np.float32)/65535
    assert np.array_equal(np.load(result.alpha_path),samples[...,3])
    assert np.array_equal(np.load(result.rgb_path),expected)
    # Hidden RGB is still color transformed even where alpha is exactly zero.
    assert np.array_equal(np.load(result.rgb_path)[rgba[...,3]==0],expected[rgba[...,3]==0])

@pytest.mark.parametrize('extra',['assocalpha','unspecified'])
def test_tiff_ambiguous_or_associated_alpha_refused_before_publication(tmp_path,registry,extra):
    import tifffile
    rgba=np.full((4,6,4),128,dtype=np.uint8);rgba[0,0,3]=0
    path=tmp_path/'unsupported-alpha.tiff'
    tifffile.imwrite(path,rgba,photometric='rgb',extrasamples=extra)
    output=tmp_path/'working.npy'
    with pytest.raises(AssetError,match='TIFF_ALPHA_SEMANTICS_UNSUPPORTED'):
        icc_to_work(verify_decode(path),output,registry,assume_profile='srgb_v1')
    assert not output.exists()
    assert not output.with_name('working.alpha.npy').exists()
