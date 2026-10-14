from contextlib import contextmanager
from dataclasses import replace
from hashlib import sha256
from io import BytesIO
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import WorkingImage,Canvas,ColorSpace,TransformChain
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime import restore_preview_source as m


@pytest.mark.parametrize('mode',['valid','noncanonical','hash','truncated','changed','dtype','zero_pixels','root_alias','alpha_alias','oversize','activity_false'])
def test_source_real_codec_and_exact_version_rechecks(monkeypatch,mode):
    root=uuid4();ref=ArtifactRef(artifact_id=UUID(int=0) if mode=='zero_pixels' else root if mode=='root_alias' else uuid4(),sha256='a'*64)
    image=WorkingImage(pixels=ref,alpha=ref if mode=='alpha_alias' else None,dtype='uint16' if mode=='dtype' else 'float32',
        canvas=Canvas(width=2,height=1,orientation=1,space_id='source',source_sha256='b'*64),
        color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
    raw=canonical_bytes(image)
    if mode=='noncanonical':raw=b' '+raw
    digest=sha256(raw).hexdigest()
    binding=m.RestoreLegacyApplyBinding(uuid4(),uuid4(),uuid4(),root,digest)
    obj=RestoreObject('photos','tenants/fixture/source','v1',digest,len(raw))
    if mode=='oversize':obj=replace(obj,size=1048577)
    if mode=='hash':raw=raw.replace(b'linear',b'LINEAR',1)
    if mode=='truncated':raw=raw[:-1]
    calls=[];opens=[]
    def observe(*a,**k):
        calls.append(True)
        return replace(obj,version='v2') if mode=='changed' and len(calls)>1 else obj
    monkeypatch.setattr(m,'read_preview_source_object',observe)
    monkeypatch.setattr(m,'verify_fixture_preview_array',lambda *a,**k:'verified-array')
    class Store:
        def __init__(self,*a,**k):pass
        @contextmanager
        def open_version(self,value):
            opens.append(True)
            assert value==obj
            if mode=='oversize':raise AssertionError('unbounded read')
            yield BytesIO(raw)
    monkeypatch.setattr(m,'MinioRestoreObjects',Store)
    def invoke():return m.verify_fixture_preview_source(None,None,['photos'],uuid4(),binding,
        expected_database='restore',check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke().width==2 and len(calls)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='activity_false':assert opens==[]
