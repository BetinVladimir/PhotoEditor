from io import BytesIO
from hashlib import sha256
from uuid import uuid4
import pytest
from PIL import Image,ImageCms
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_legacy_preview_manifest import RestorePreviewManifestEvidence
from photo_worker.runtime import restore_legacy_preview_jpeg as m


@pytest.mark.parametrize('mode',['valid','hash','geometry','profile','exif','truncated','png','gray','activity_false','late_activity_false'])
def test_real_jpeg_decode_requires_exact_pixels_container_geometry_icc_and_no_metadata(mode):
    profile=ImageCms.ImageCmsProfile(ImageCms.createProfile('sRGB')).tobytes()
    image=Image.new('L' if mode=='gray' else 'RGB',(32,16),100 if mode=='gray' else (50,100,150))
    output=BytesIO();options={'icc_profile':profile}
    if mode=='exif':
        exif=Image.Exif();exif[270]='private description';options['exif']=exif
    image.save(output,format='PNG' if mode=='png' else 'JPEG',**options)
    raw=output.getvalue()
    if mode=='truncated':raw=raw[:-10]
    digest=sha256(raw).hexdigest()
    obj=RestoreObject('artifacts','tenants/fixture/preview','v1','f'*64 if mode=='hash' else digest,len(raw))
    manifest=RestorePreviewManifestEvidence(ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        ArtifactRef(artifact_id=uuid4(),sha256=digest),'b'*64,31 if mode=='geometry' else 32,16,'c'*64)
    checks=[]
    def active():
        checks.append(True)
        return False if mode=='activity_false' or mode=='late_activity_false' and len(checks)>=4 else None
    def invoke():return m.validate_legacy_preview_jpeg(raw,obj,manifest,
        srgb_profile=b'x'*128 if mode=='profile' else profile,check_active=active)
    if mode=='valid':assert invoke().icc_sha256==sha256(profile).hexdigest()
    else:
        with pytest.raises((ValueError,OSError,RuntimeError)):invoke()
