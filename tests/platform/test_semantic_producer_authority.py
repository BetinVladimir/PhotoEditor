from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams,TransferCandidate
from photo_worker.v2.adoption import AdoptionInvalid,ArtifactEvidence
from photo_worker.v2 import semantic_producer_authority as authority
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','missing','ambiguous','intent','model','target','calibration',
                                     'bytes','source','mutate','rotate','cancel','transaction'])
def test_adopted_semantic_producer_scoped_params_and_content(monkeypatch,variant):
    transfer,intent,_=transfer_documents()
    transfer.intent=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(intent))
    candidate=TransferCandidate(schema_version=1,target=transfer.target,mask=None,transform=None,
        confidence=0,state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    candidate_ref=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(candidate))
    assembly=V2SemanticAssembleParams(schema_version=1,intent=transfer.intent,candidates=[candidate_ref],
        model_lock_sha=transfer.model_lock_sha,calibration=transfer.calibration)
    if variant=='intent':transfer.intent=transfer.intent.model_copy(update={'artifact_id':uuid4()})
    if variant=='model':transfer.model_lock_sha='e'*64
    if variant=='target':transfer.target=transfer.target.model_copy(update={'asset_version_id':uuid4()})
    if variant=='calibration':transfer.calibration=transfer.calibration.model_copy(update={'artifact_id':uuid4()})
    body=canonical_bytes(transfer);params_id=uuid4();producer=uuid4();producer_result=uuid4()
    row=(producer,2,3,producer_result,params_id,sha256(body).hexdigest())
    tenant,task=uuid4(),uuid4();context=SimpleNamespace(attempt=4,fence=5,result_id=uuid4())
    events=[]
    class Read:
        autocommit=variant!='transaction'
        def execute(self,query,args):
            events.append('query')
            assert 'v27_semantic_assembly_producer' in query
            assert args==(tenant,task,4,5,context.result_id,candidate_ref.artifact_id,candidate_ref.sha256)
            if variant=='mutate':
                assembly.model_lock_sha='f'*64
                candidate.reason='MUTATED'
            rows=[] if variant=='missing' else [row,row] if variant=='ambiguous' else [row]
            if variant=='rotate' and events.count('query')==2:
                rows=[(producer,2,4,producer_result,params_id,sha256(body).hexdigest())]
            return SimpleNamespace(fetchall=lambda:rows)
    class Reader:
        def __init__(self,connection,objects,**kwargs):
            assert kwargs==dict(tenant_id=tenant,task_id=producer,attempt=2,fence=3,allowed_bucket='bucket')
        def evidence(self,ref):
            events.append('evidence')
            assert ref.artifact_id==params_id and ref.sha256==sha256(body).hexdigest()
            return ArtifactEvidence(ref,'application/json',len(body),None,'output' if variant=='source' else 'input')
        def read_json(self,ref,max_bytes):
            events.append('object')
            return body+b' ' if variant=='bytes' else body
    monkeypatch.setattr(authority,'PostgresObjectEvidenceReader',Reader)
    def active():
        if variant=='cancel':raise InterruptedError('cancelled')
    def run():
        return authority.verify_adopted_semantic_producers(Read(),object(),'bucket',tenant,task,context,
            assembly,intent,None,[candidate],check_active=active)
    if variant=='transaction':
        with pytest.raises(ValueError,match='autocommit'):run()
        assert events==[]
    elif variant=='cancel':
        with pytest.raises(InterruptedError):run()
        assert events==[]
    elif variant in ('valid','mutate'):
        run();assert events==['query','evidence','object','query']
    else:
        with pytest.raises(AdoptionInvalid,match='SEMANTIC_PRODUCER_'):run()
        if variant in ('missing','ambiguous'):assert events==['query']
        if variant=='source':assert events==['query','evidence']
        if variant=='rotate':assert events==['query','evidence','object','query']


def test_semantic_producer_authority_factory_preserves_coordinator_context(monkeypatch):
    connection=SimpleNamespace(autocommit=True);objects=object();calls=[]
    def verify(*args,**kwargs):calls.append((args,kwargs))
    monkeypatch.setattr(authority,'verify_adopted_semantic_producers',verify)
    active=lambda:None
    installed=authority.adopted_semantic_producer_authority(connection,objects,'bucket',check_active=active)
    context=object();documents=[object() for _ in range(4)];tenant,task=uuid4(),uuid4()
    installed(tenant,task,context,*documents)
    assert calls==[((connection,objects,'bucket',tenant,task,context,*documents),{'check_active':active})]
    with pytest.raises(ValueError,match='autocommit'):
        authority.adopted_semantic_producer_authority(SimpleNamespace(autocommit=False),objects,'bucket')
