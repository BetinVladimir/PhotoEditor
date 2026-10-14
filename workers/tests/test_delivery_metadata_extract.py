from types import SimpleNamespace
from PIL import Image
import pytest
from photo_worker.v2.delivery_metadata_extract import extract_delivery_metadata


def test_delivery_extract_actual_original_exif_allowlist(tmp_path):
    exif=Image.Exif();exif[315]='Author';exif[33432]='Copyright';exif[271]='Make';exif[272]='Model';exif[305]='private software'
    path=tmp_path/'original.jpg'
    Image.new('RGB',(4,3)).save(path,exif=exif)
    with Image.open(path) as image:document=extract_delivery_metadata(image)
    assert document.author=='Author' and document.camera_model=='Model' and document.gps is None
    assert 'private software' not in document.model_dump_json()


@pytest.mark.parametrize('mode',['valid','south-west','invalid-minutes','invalid-ref','nan','overflow','altitude'])
def test_delivery_extract_gps_is_opt_in_and_bounded(mode):
    gps={1:'N',2:(42,30,0),3:'E',4:(23,15,0),5:0,6:100}
    if mode=='south-west':gps[1]='S';gps[3]='W'
    if mode=='invalid-minutes':gps[2]=(42,60,0)
    if mode=='invalid-ref':gps[1]='unknown'
    if mode=='nan':gps[4]=(float('nan'),0,0)
    if mode=='overflow':gps[2]=(90,1,0)
    if mode=='altitude':gps[6]=100001
    class Exif(dict):
        def get_ifd(self,tag):return gps
    image=SimpleNamespace(getexif=lambda:Exif({34853:1}))
    assert extract_delivery_metadata(image).gps is None
    if mode in ('valid','south-west'):
        result=extract_delivery_metadata(image,include_gps=True)
        assert result.gps.latitude==(-42.5 if mode=='south-west' else 42.5)
        assert result.gps.longitude==(-23.25 if mode=='south-west' else 23.25)
    else:
        with pytest.raises(ValueError):extract_delivery_metadata(image,include_gps=True)
