from contextlib import contextmanager
from dataclasses import replace
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef as WireRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.delivery_encoded_verify import VerifiedEncodedRaster
from photo_worker.v2.delivery_metadata_result import ValidatedDeliveryMetadataResult
from photo_worker.v2.delivery_metadata_files import VerifiedDeliveryMetadataFiles
from photo_worker.v2.delivery_metadata_prepare_result import PreparedDeliveryMetadataResult
from photo_worker.v2.delivery_metadata_reader import MetadataResultScope
from photo_worker.v2.delivery_metadata_service import PreparedCurrentDeliveryMetadata,record_prepared_delivery_metadata


def fixture():
    scope=MetadataResultScope(uuid4(),uuid4(),1,2,uuid4())
    raster=ArtifactRef(uuid4(),'a'*64)
    manifest=EncodedImage(artifact=WireRef(artifact_id=raster.artifact_id,sha256=raster.sha256),format='jpeg',
        width=4,height=3,icc_id='srgb_v1',icc_sha256='b'*64,alpha=False,metadata_policy='copyright_only')
    docs=ValidatedDeliveryMetadataResult(b'{}',b'{}',canonical_bytes(manifest),b'{}',b'{}')
    def ref(body):return ArtifactRef(uuid4(),sha256(body).hexdigest())
    result=PreparedDeliveryMetadataResult(VerifiedDeliveryMetadataFiles(docs,VerifiedEncodedRaster(raster.sha256,10,4,3,False)),
        ref(docs.params_jcs),ref(docs.after_jcs),ref(docs.report_jcs),raster)
    class Connection:
        autocommit=True
        calls=[]
        committed=False
        rolled_back=False
        row=(True,)
        @contextmanager
        def transaction(self):
            try:yield
            except BaseException:
                self.rolled_back=True
                raise
            else:self.committed=True
        def execute(self,query,args):
            self.calls.append((query,args))
            return SimpleNamespace(fetchone=lambda:self.row)
    return Connection(),scope,PreparedCurrentDeliveryMetadata(scope,result)


def test_metadata_verification_scoped_transaction():
    connection,scope,prepared=fixture()
    assert record_prepared_delivery_metadata(connection,scope=scope,prepared=prepared)
    assert connection.committed and not connection.rolled_back
    assert connection.calls[0][1]==(str(scope.tenant_id),)
    assert connection.calls[1][1][:5]==(scope.tenant_id,scope.task_id,1,2,scope.result_id)
    assert connection.calls[1][1][-1]==10


@pytest.mark.parametrize('mode',['scope','root','raster','refused','late-cancel'])
def test_metadata_verification_refusal_or_cancellation(mode):
    connection,scope,prepared=fixture()
    active=lambda:None
    if mode=='scope':scope=replace(scope,tenant_id=uuid4())
    if mode=='root':prepared=replace(prepared,result=replace(prepared.result,root=ArtifactRef(uuid4(),'c'*64)))
    if mode=='raster':prepared=replace(prepared,result=replace(prepared.result,raster=ArtifactRef(uuid4(),'a'*64)))
    if mode=='refused':connection.row=(False,)
    if mode=='late-cancel':
        def active():
            if len(connection.calls)==2:raise RuntimeError('lease lost')
    with pytest.raises((AdoptionInvalid,RuntimeError)):
        record_prepared_delivery_metadata(connection,scope=scope,prepared=prepared,check_active=active)
    assert not connection.committed
    if mode in ('refused','late-cancel'):assert connection.rolled_back
    else:assert connection.calls==[]


@pytest.mark.parametrize('accepted',[True,False])
def test_metadata_adoption_uses_dedicated_atomic_boundary(accepted):
    from photo_worker.v2.delivery_metadata_service import adopt_prepared_delivery_metadata
    connection,scope,prepared=fixture()
    connection.row=(accepted,)
    if accepted:
        assert adopt_prepared_delivery_metadata(connection,scope=scope,prepared=prepared)
        assert connection.committed
    else:
        with pytest.raises(AdoptionInvalid):
            adopt_prepared_delivery_metadata(connection,scope=scope,prepared=prepared)
        assert connection.rolled_back
    assert 'photo.adopt_v30_delivery_metadata(' in connection.calls[1][0]


def test_metadata_adoption_late_cancellation_rolls_back():
    from photo_worker.v2.delivery_metadata_service import adopt_prepared_delivery_metadata
    connection,scope,prepared=fixture()
    def active():
        if len(connection.calls)==2:raise RuntimeError('lease lost')
    with pytest.raises(RuntimeError,match='lease lost'):
        adopt_prepared_delivery_metadata(connection,scope=scope,prepared=prepared,check_active=active)
    assert connection.rolled_back and not connection.committed
