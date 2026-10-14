import json
from hashlib import sha256
from types import SimpleNamespace as NS
import pytest
from PIL import ImageCms
from photo_worker.runtime import restore_preview_icc as m


@pytest.mark.parametrize('mode',['valid','source_sha','target_bytes','corrupt_file','lock_sha','missing','activity_false'])
def test_installed_real_icc_registry_refuses_substitution(tmp_path,mode):
    data=ImageCms.ImageCmsProfile(ImageCms.createProfile('sRGB')).tobytes();digest=sha256(data).hexdigest()
    (tmp_path/'working.icc').write_bytes(b'broken' if mode=='corrupt_file' else data)
    (tmp_path/'srgb.icc').write_bytes(data)
    entries=[{'id':'linear_srgb_v1','path':'working.icc','bytes':len(data),'sha256':'f'*64 if mode=='lock_sha' else digest},
        {'id':'srgb_v1','path':'srgb.icc','bytes':len(data),'sha256':digest}]
    if mode=='missing':entries=entries[1:]
    (tmp_path/'profiles.lock.json').write_text(json.dumps({'profiles':entries}))
    registry=m.ICCRegistry(tmp_path)
    source=NS(profile_id='linear_srgb_v1',icc_sha256='a'*64 if mode=='source_sha' else digest)
    def invoke():return m.verify_preview_icc(registry,source,b'x'*128 if mode=='target_bytes' else data,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke()==(digest,digest)
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
