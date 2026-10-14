from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams,TransferCandidate
from photo_worker.operations.session.contracts import InputSnapshot,SnapshotItem
from photo_worker.operations.semantic.assemble_adapter import execute_assembly
from photo_worker.runtime.executor import OperationFailure
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','authority','output','scratch','mutate'])
def test_assembly_executor_provenance_review_and_early_budget(variant):
    transfer,intent,calibration=transfer_documents();docs={};staged=[];events=[]
    def ref(value):
        result=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value));docs[result.artifact_id]=value;return result
    candidate=TransferCandidate(schema_version=1,target=transfer.target,mask=None,transform=None,confidence=0,
        state='unsupported',reason='NO_PLANAR_CORRESPONDENCE')
    params=V2SemanticAssembleParams(schema_version=1,intent=ref(intent),candidates=[ref(candidate)],
        model_lock_sha=transfer.model_lock_sha,calibration=ref(calibration))
    snapshot=InputSnapshot(schema_version=1,snapshot_id=intent.target_snapshot_id,snapshot_digest='0'*64,
        items=[SnapshotItem(ordinal=0,binding=transfer.target,working_image=transfer.target.source,
            captured_at=None,camera_hash=None,recipe=params.intent)])
    snapshot.snapshot_digest=canonical_hash(snapshot.model_dump(mode='json',exclude={'snapshot_digest'}))
    def contract(value,model):events.append('read');return docs[value.artifact_id]
    def publish(plan,role):staged.append(plan);assert role=='document';return ref(plan)
    def verify(*values):
        events.append('authority')
        if variant=='authority':raise ValueError('SEMANTIC_PRODUCER_INVALID')
        if variant=='mutate':values[-1][0].reason='mutated'
    estimate=dict(ram_bytes=256*1024*1024,scratch_bytes=32*1024*1024,vram_bytes=0,output_bytes=8*1024*1024)
    if variant=='output':estimate['output_bytes']-=1
    if variant=='scratch':estimate['scratch_bytes']=16*1024*1024
    ctx=SimpleNamespace(check_active=lambda:None,progress=lambda *args:None,
        lease=SimpleNamespace(execution_profile={'backend':'cpu','renderer_version':'v2.2','resource_estimate':estimate}))
    artifacts=SimpleNamespace(params=lambda model:params,contract=contract,stage_contract=publish)
    def run():return execute_assembly(ctx,artifacts,load_snapshot=lambda *args:snapshot,verify_provenance=verify)
    if variant in ('output','scratch'):
        with pytest.raises(OperationFailure,match='OUTPUT_ADMISSION_EXCEEDED' if variant=='output' else 'SCRATCH_ADMISSION_EXCEEDED'):run()
        assert events==[] and staged==[]
    elif variant=='authority':
        with pytest.raises(ValueError,match='PRODUCER_INVALID'):run()
        assert staged==[]
    else:
        result=run();assert result.review_required and len(result.outputs)==1
        assert staged[0].candidates[0].reason=='NO_PLANAR_CORRESPONDENCE'
        assert result.bytes_written==len(canonical_bytes(staged[0]))
