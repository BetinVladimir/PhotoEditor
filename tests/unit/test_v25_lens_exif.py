from types import SimpleNamespace as NS
import pytest
from photo_worker.operations.optical.exif import lens_metadata_from_header
from photo_worker.runtime.executor import OperationFailure


def test_exact_lens_header_numbers_and_no_35mm_inference():
    values={'exif-ifd0-Model':'Camera (Camera, ASCII)','exif-ifd2-LensModel':'Lens',
        'exif-ifd2-FocalLength':'50/1 (50 mm, Rational)','exif-ifd2-FNumber':'2/1 (f/2, Rational)'}
    class Header:
        def get_fields(self):return list(values)
        def get(self,key):return values[key]
    binding=NS(asset_version_id='version',source='source')
    evidence=lens_metadata_from_header(Header(),binding)
    assert (evidence.camera,evidence.lens,evidence.focal_mm,evidence.aperture)==('Camera','Lens',50,2)
    for bad in ['50/0','nan','50 unknown','0']:
        values['exif-ifd2-FocalLength']=bad
        with pytest.raises(OperationFailure):lens_metadata_from_header(Header(),binding)
    values.pop('exif-ifd2-FocalLength');values['exif-ifd2-FocalLengthIn35mmFilm']='50'
    with pytest.raises(OperationFailure):lens_metadata_from_header(Header(),binding)


def test_original_jpeg_exif_is_read_without_profile_fallback(tmp_path):
    from PIL import Image,TiffImagePlugin
    from photo_worker.operations.optical.exif import inspect_lens_metadata
    exif=Image.Exif();exif[272]='Camera'
    exif[34665]={42036:'Lens',37386:TiffImagePlugin.IFDRational(50,1),33437:TiffImagePlugin.IFDRational(2,1)}
    path=tmp_path/'original.jpg'
    Image.new('RGB',(4,3),'white').save(path,exif=exif)
    binding=NS(asset_version_id='version',source='source')
    evidence=inspect_lens_metadata(path,binding)
    assert (evidence.camera,evidence.lens,evidence.focal_mm,evidence.aperture)==('Camera','Lens',50,2)
    assert evidence.asset_version_id=='version' and evidence.source=='source'
