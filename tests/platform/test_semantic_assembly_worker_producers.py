from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from test_semantic_transfer_contracts import transfer_documents
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams,TransferCandidate
from photo_worker.operations.semantic.assembly_producers import sql_semantic_assembly_provenance
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('variant',['valid','revoke','lease','bucket','size'])
def test_producer_reader_exact_version_scope_and_cleanup(variant):
    transfer,intent,_=transfer_documents()
    transfer.intent=ArtifactRef(artifact_id=transfer.intent.artifact_id,sha256=canonical_hash(intent))
    raw=canonical_bytes(transfer)
    candidate=TransferCandidate(schema_version=1,target=transfer.target,mask=None,transform=None,confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(candidate))
    assembly=V2SemanticAssembleParams(schema_version=1,intent=transfer.intent,candidates=[ref],model_lock_sha=transfer.model_lock_sha,calibration=transfer.calibration)
    pin=dict(producer_task_id=str(uuid4()),producer_result_id=str(uuid4()),producer_attempt=1,producer_fence=2,params_artifact_id=str(uuid4()),params_sha=canonical_hash(transfer),bucket='owned',object_key='params',object_version='exact-old',bytes=len(raw))
    if variant=='bucket':pin['bucket']='foreign'
    if variant=='size':pin['bytes']=1048577
    events=[];revoked=False
    def authority(*args):return None if revoked else pin
    class Response(BytesIO):
        def close(self):events.append('close');super().close()
        def release_conn(self):events.append('release')
    def get(bucket,key,*,version_id):
        nonlocal revoked
        assert (bucket,key,version_id)==('owned','params','exact-old');events.append('open')
        if variant=='revoke':revoked=True
        return Response(raw)
    ctx=SimpleNamespace(lease=SimpleNamespace(fence=1),check_active=lambda:None)
    verify=sql_semantic_assembly_provenance(SimpleNamespace(semantic_assembly_producer=authority),SimpleNamespace(get_object=get),'owned',ctx)
    if variant=='lease':ctx.lease.fence=2
    if variant=='valid':verify(assembly,intent,None,[candidate])
    else:
        with pytest.raises(AdoptionInvalid):verify(assembly,intent,None,[candidate])
    assert events==(['open','close','release'] if variant in ('valid','revoke') else [])
