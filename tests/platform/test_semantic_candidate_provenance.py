from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams,TransferCandidate
from photo_worker.contracts.wire import TaskCommand,TaskResult,TaskMetrics
from photo_worker.operations.semantic.provenance import verify_candidate_producer
from test_semantic_transfer_contracts import transfer_documents


@pytest.mark.parametrize('variant',['valid','attempt','tenant','operation','target','intent','params','missing','duplicate'])
def test_candidate_exact_producer_command_result_binding(variant):
    transfer,intent,_=transfer_documents()
    def ref(value):return ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(value))
    transfer.intent=ref(intent)
    mask=ArtifactRef(artifact_id=uuid4(),sha256='b'*64);transform=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    candidate=TransferCandidate(schema_version=1,target=transfer.target,mask=mask,transform=transform,
        confidence=.8,state='proposed',reason=None)
    candidate_ref=ref(candidate)
    assembly=V2SemanticAssembleParams(schema_version=1,intent=transfer.intent,candidates=[candidate_ref],
        model_lock_sha=transfer.model_lock_sha,calibration=transfer.calibration)
    command=TaskCommand(schema_version=1,event_id=uuid4(),tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=2,
        operation='semantic.transfer',operation_version='1',resource_class='gpu',correlation_id=uuid4(),inputs=[],
        params=ref(transfer),model=None,timeout_seconds=1800,traceparent='')
    result=TaskResult(schema_version=1,event_id=uuid4(),type='task.succeeded',tenant_id=command.tenant_id,
        run_id=command.run_id,task_id=command.task_id,attempt=2,fencing_token=3,outputs=[candidate_ref],
        metrics=TaskMetrics(duration_ms=1,peak_memory_bytes=1),error=None)
    if variant=='attempt':result.attempt=1
    if variant=='tenant':result.tenant_id=uuid4()
    if variant=='operation':command.operation='image.inpaint'
    if variant=='target':transfer.target=transfer.target.model_copy(update={'canvas_hash':'f'*64});command.params=ref(transfer)
    if variant=='intent':transfer.intent=ArtifactRef(artifact_id=uuid4(),sha256='f'*64);command.params=ref(transfer)
    if variant=='params':command.params.sha256='f'*64
    if variant=='missing':result.outputs.clear()
    if variant=='duplicate':result.outputs.append(candidate_ref)
    def check():verify_candidate_producer(assembly,intent,candidate_ref,candidate,command,result,transfer)
    if variant=='valid':check()
    else:
        with pytest.raises(ValueError,match='SEMANTIC_PRODUCER_'):check()
