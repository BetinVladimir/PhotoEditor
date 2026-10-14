from uuid import uuid4
from PIL import Image
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.metadata import MetadataDocument,MetadataParams
from photo_worker.operations.metadata.verify import verify_written_metadata


@pytest.mark.parametrize('mode',['valid','missing','changed','private','camera'])
def test_written_metadata_matches_actual_png(tmp_path,mode):
    path=tmp_path/'written.png'
    exif=Image.Exif();exif[274]=1
    if mode!='missing':exif[315]='Changed' if mode=='changed' else 'Author'
    if mode=='private':exif[305]='Private software'
    if mode=='camera':exif[272]='Unapproved camera'
    Image.new('RGB',(4,3)).save(path,exif=exif)
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=MetadataParams(encoded=ref,document=ref,policy='copyright_only')
    with Image.open(path) as image:
        if mode=='valid':verify_written_metadata(image,MetadataDocument(author='Author'),params)
        else:
            with pytest.raises(ValueError):verify_written_metadata(image,MetadataDocument(author='Author'),params)


def test_safe_camera_preserves_exact_allowed_camera(tmp_path):
    path=tmp_path/'written.png';exif=Image.Exif();exif[315]='Author';exif[272]='Camera'
    Image.new('RGB',(4,3)).save(path,exif=exif)
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=MetadataParams(encoded=ref,document=ref,policy='safe_camera',copy_gps=False)
    with Image.open(path) as image:
        verify_written_metadata(image,MetadataDocument(author='Author',camera_model='Camera'),params)


class ControlledExif(dict):
    def __init__(self,top,nested=None,gps=None):
        super().__init__(top);self.nested=nested or {};self.gps=gps or {}
    def get_ifd(self,tag):return self.nested if tag==34665 else self.gps


class ControlledImage:
    width=4; height=3; info={}; text={}
    def __init__(self,exif):self.exif=exif
    def getexif(self):return self.exif


@pytest.mark.parametrize('mode',['valid','wrong_lat_ref','wrong_alt_ref','private_gps','missing_gps','rounding','changed','opt_out'])
def test_written_gps_permission_signs_and_bounded_rounding(mode):
    from photo_worker.contracts.metadata import GPSMetadata
    gps={0:b'\x02\x03\x00\x00',1:'S',2:(42,30,0),3:'W',4:(23,15,0),5:1,6:20}
    if mode=='wrong_lat_ref':gps[1]='N'
    if mode=='wrong_alt_ref':gps[5]=0
    if mode=='private_gps':gps[27]=b'private processing method'
    if mode=='rounding':gps[2]=(42,30,0.0001)
    if mode=='changed':gps[2]=(42,30,0.01)
    top={} if mode=='missing_gps' else {34853:1}
    image=ControlledImage(ControlledExif(top,gps=gps))
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=MetadataParams(encoded=ref,document=ref,policy='safe_camera',copy_gps=mode!='opt_out')
    document=MetadataDocument(gps=GPSMetadata(latitude=-42.5,longitude=-23.25,altitude_m=-20))
    if mode in ('valid','rounding'):verify_written_metadata(image,document,params)
    else:
        with pytest.raises(ValueError):verify_written_metadata(image,document,params)


@pytest.mark.parametrize('top,nested',[
    ({274:7},{}),({256:5},{}),({257:True},{}),
    ({34665:1},{40962:5}),({34665:1},{40963:4}),
    ({34665:1},{37500:b'private maker note'}),
])
def test_written_metadata_rejects_orientation_dimensions_and_private_ifd(top,nested):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=MetadataParams(encoded=ref,document=ref,policy='safe_camera')
    with pytest.raises(ValueError):
        verify_written_metadata(ControlledImage(ControlledExif(top,nested)),MetadataDocument(),params)


def test_written_metadata_cancellation_precedes_header_access():
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        verify_written_metadata(None,None,None,check_active=cancelled)
