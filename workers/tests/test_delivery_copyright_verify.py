from PIL import Image
import pytest
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.v2.delivery_copyright_verify import verify_delivery_copyright_image


@pytest.mark.parametrize('mode',['valid','missing','changed','camera','orientation','dimensions','comment'])
def test_delivery_copyright_verifies_actual_encoded_exif(tmp_path,mode):
    source=MetadataDocument(author='Author',copyright='Copyright',camera_model='private camera')
    exif=Image.Exif();exif[274]=1;exif[315]='Author';exif[33432]='Copyright'
    if mode=='missing':del exif[315]
    if mode=='changed':exif[33432]='substitution'
    if mode=='camera':exif[272]='private camera'
    if mode=='orientation':exif[274]=6
    if mode=='dimensions':exif[256]=999
    path=tmp_path/'copyright.png'
    with Image.new('RGB',(8,8)) as image:image.save(path,exif=exif)
    with Image.open(path) as image:
        if mode=='comment':image.info['comment']=b'unapproved'
        if mode=='valid':assert verify_delivery_copyright_image(image,source) is True
        else:
            with pytest.raises(ValueError):verify_delivery_copyright_image(image,source)
