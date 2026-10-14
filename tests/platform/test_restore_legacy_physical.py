from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace as NS
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_legacy_physical as m


@pytest.mark.parametrize('mode',['valid','noncanonical','hash','changed','zero_pixels','alias','scope_false','activity_false'])
def test_legacy_physical_root_uses_real_working_image_codec_and_rechecks_catalogue(monkeypatch,mode):
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    if mode=='zero_pixels':ref=ArtifactRef(artifact_id=UUID(int=0),sha256='a'*64)
    image=WorkingImage(pixels=ref,alpha=ref if mode=='alias' else None,dtype='float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image)
    if mode=='noncanonical':raw=b' '+raw
    obj=RestoreObject('photos','tenants/fixture/output','v1','f'*64 if mode=='hash' else sha256(raw).hexdigest(),len(raw))
    result=NS(object=obj,artifact_id=uuid4());calls=[];opens=[]
    def observe(*a,**k):
        calls.append(True)
        return NS(object=obj,artifact_id=uuid4()) if mode=='changed' and len(calls)>1 else result
    monkeypatch.setattr(m,'observe_fixture_legacy_apply_result',observe)
    monkeypatch.setattr(m,'verify_fixture_legacy_array',lambda *a,**k:'array-verified')
    monkeypatch.setattr(m,'verify_fixture_legacy_quality',lambda *a,**k:'quality-verified')
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,value):
            opens.append(True)
            assert value==obj
            yield BytesIO(raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    def run():return m.verify_fixture_legacy_working_image(object(),object(),('photos',),NS(tenant_id=uuid4()),object(),object(),
        expected_database='restore',
        assert_fixture_scope=lambda *a:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().pixels==ref and len(calls)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('scope_false','activity_false'):assert opens==[]
