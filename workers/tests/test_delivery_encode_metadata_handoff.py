from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.delivery_encoded_prepare import PreparedDeliveryEncode
from photo_worker.v2.delivery_encoded_verify import VerifiedEncodedRaster
from photo_worker.v2.delivery_encoded_service import (
    DeliveryEncodeContext,PreparedCurrentDeliveryEncode,adopt_prepared_delivery_encode,
)


class Row:
    def __init__(self,value):self.value=value
    def fetchone(self):return self.value


class Connection:
    def __init__(self,accepted=True):self.calls=[];self.accepted=accepted
    def execute(self,statement,args):
        self.calls.append((statement,args))
        return Row((True,) if 'set_config' in statement else (self.accepted,))


def prepared_fixture():
    tenant,task=uuid4(),uuid4()
    root=ArtifactRef(uuid4(),'a'*64);params=ArtifactRef(uuid4(),'b'*64)
    raster=ArtifactRef(uuid4(),'c'*64)
    evidence=PreparedDeliveryEncode(b'{}',b'{}',b'{}',root,raster,
        VerifiedEncodedRaster(raster.sha256,100,1,1,False))
    return tenant,task,PreparedCurrentDeliveryEncode(
        DeliveryEncodeContext(1,7,uuid4(),root,params),evidence,tenant,task)


def test_encode_handoff_sets_tenant_before_single_sql_adoption_abi():
    tenant,task,prepared=prepared_fixture();connection=Connection()
    adopt_prepared_delivery_encode(connection,tenant_id=tenant,task_id=task,prepared=prepared)
    assert len(connection.calls)==2
    assert connection.calls[0]==("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
    statement,args=connection.calls[1]
    assert 'photo.adopt_v30_delivery_encode(' in statement
    assert args[:5]==(tenant,task,1,7,prepared.context.result_id)
    assert args[5:]==('{}','{}','{}',100)


def test_encode_handoff_scope_and_sql_refusal():
    tenant,task,prepared=prepared_fixture();connection=Connection(False)
    with pytest.raises(AdoptionInvalid):
        adopt_prepared_delivery_encode(connection,tenant_id=uuid4(),task_id=task,prepared=prepared)
    assert not connection.calls
    with pytest.raises(AdoptionInvalid):
        adopt_prepared_delivery_encode(connection,tenant_id=tenant,task_id=task,prepared=prepared)
