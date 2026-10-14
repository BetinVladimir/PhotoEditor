from io import BytesIO
from types import SimpleNamespace
from hashlib import sha256
from uuid import uuid4
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.semantic_presentation import read_exposure_presentation_result
from photo_worker.v2.adoption import AdoptionInvalid


def test_presentation_rechecks_source_after_versioned_read():
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    import json
    body=json.dumps(dict(schema_version=1,working_image=ref,report=ref,changed_mask=ref,generated_mask=None)).encode()
    row=(uuid4(),uuid4(),uuid4(),uuid4(),1,1,uuid4(),sha256(body).hexdigest(),'bucket','key','version',len(body),'application/json')
    class Connection:
        autocommit=True
        info=SimpleNamespace(transaction_status=TransactionStatus.IDLE)
        calls=0
        def execute(self,*args):
            self.calls+=1
            return SimpleNamespace(fetchall=lambda:[row] if self.calls==1 else [])
    class Objects:
        def get_object(self,bucket,key,*,version_id):
            assert (bucket,key,version_id)==('bucket','key','version')
            self.response=BytesIO(body)
            return self.response
    connection,objects=Connection(),Objects()
    with pytest.raises(AdoptionInvalid,match='SOURCE_STALE'):
        read_exposure_presentation_result(connection,objects,tenant_id=uuid4(),target_id=uuid4(),allowed_bucket='bucket')
    assert connection.calls==2 and objects.response.closed


def test_presentation_rejects_untrusted_icc_before_pixel_io(monkeypatch,tmp_path):
    import photo_worker.v2.semantic_presentation as presentation
    working=SimpleNamespace(color=SimpleNamespace(profile_id='work_rec2020_gamma24_v1',icc_sha256='a'*64))
    monkeypatch.setattr(presentation,'read_exposure_presentation_working_image',lambda *args,**kwargs:working)
    registry=SimpleNamespace(resolve=lambda profile:SimpleNamespace(sha256='b'*64))
    class Objects:
        def get_object(self,*args,**kwargs):
            raise AssertionError('Pixel IO must not precede ICC verification')
    with pytest.raises(AdoptionInvalid,match='ICC_MISMATCH'):
        with presentation.exposure_presentation_pixels(None,Objects(),tenant_id=uuid4(),target_id=uuid4(),allowed_bucket='bucket',scratch_root=tmp_path,max_bytes=1024,icc_registry=registry):
            raise AssertionError('Untrusted profile accepted')
    assert list(tmp_path.iterdir())==[]


def test_combined_budget_rejects_when_each_array_fits(monkeypatch,tmp_path):
    import photo_worker.v2.semantic_presentation as presentation
    rgb=SimpleNamespace(artifact_id=uuid4(),sha256='a'*64)
    alpha=SimpleNamespace(artifact_id=uuid4(),sha256='b'*64)
    monkeypatch.setattr(presentation,'read_exposure_presentation_working_image',lambda *a,**k:SimpleNamespace(pixels=rgb,alpha=alpha))
    class Connection:
        info=SimpleNamespace(transaction_status=TransactionStatus.IDLE)
        def execute(self,query,args):
            ref=rgb if args[2]==rgb.artifact_id else alpha
            row=('bucket','key','version',ref.sha256,70 if ref is rgb else 40,'application/x-npy')
            return SimpleNamespace(fetchall=lambda:[row])
    class Objects:
        def get_object(self,*a,**k):raise AssertionError('Over-budget IO')
    with pytest.raises(AdoptionInvalid,match='SCRATCH_EXCEEDED'):
        with presentation.exposure_presentation_image(Connection(),Objects(),tenant_id=uuid4(),target_id=uuid4(),allowed_bucket='bucket',scratch_root=tmp_path,max_bytes=100,icc_registry=None):
            raise AssertionError('Combined budget bypass')
    assert list(tmp_path.iterdir())==[]
