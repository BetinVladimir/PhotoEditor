from contextlib import contextmanager
from io import BytesIO
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.runtime import scratch as scratch_module
from photo_worker.runtime.scratch import DiskQuota
from PIL import Image,ImageCms
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_export_physical as m


@pytest.mark.parametrize('mode',['valid','hash','changed','geometry','profile','container','scratch','inactive'])
def test_real_encoded_jpeg_production_verifier_and_catalogue_recheck(tmp_path,monkeypatch,mode):
    profile=ImageCms.ImageCmsProfile(ImageCms.createProfile('sRGB')).tobytes()
    output=BytesIO();Image.new('RGB',(32,16),(80,100,120)).save(output,format='PNG' if mode=='container' else 'JPEG',icc_profile=profile)
    raw=output.getvalue();obj=RestoreObject('photos','tenants/fixture/export','v1','f'*64 if mode=='hash' else sha256(raw).hexdigest(),len(raw))
    result=NS(object=obj,media_type='image/jpeg');calls=[]
    def observe(*a,**k):
        calls.append(True)
        return NS(object=obj,media_type='changed') if mode=='changed' and len(calls)>1 else result
    monkeypatch.setattr(m,'observe_fixture_encoded_export',observe)
    monkeypatch.setattr(m,'verify_fixture_export_plan',lambda *a,**k:'plan-sha')
    monkeypatch.setattr(m,'verify_fixture_export_params',lambda *a,**k:'plan-sha')
    monkeypatch.setattr(m,'verify_export_geometry',lambda *a,**k:None)
    monkeypatch.setattr(m,'verify_fixture_export_memory',lambda *a,**k:'memory-sha')
    if mode=='scratch':
        monkeypatch.setattr(scratch_module.os,'fstatvfs',lambda _:NS(f_bavail=0,f_frsize=4096))
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,value):
            assert mode!='scratch'
            yield BytesIO(raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    params=m.EncodeParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),format='jpeg',icc_id='srgb_v1',
        icc_sha256='b'*64 if mode=='profile' else sha256(profile).hexdigest(),quality=90,alpha='matte',matte_rgb=[1.,1.,1.],metadata_policy='strip')
    def invoke():return m.verify_fixture_encoded_export(None,None,['photos'],NS(tenant_id=uuid4()),None,None,None,uuid4(),expected_database='restore',
        params=params,source=None,width=31 if mode=='geometry' else 32,height=16,alpha=False,scratch_root=tmp_path,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().bytes_verified==len(raw)
    else:
        with pytest.raises((ValueError,RuntimeError,DiskQuota)):invoke()
    if mode=='inactive':assert not calls
    assert not list(tmp_path.iterdir())
