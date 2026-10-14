"""Content binding of a candidate to its successful transfer producer.

Only authenticated DB/gateway records may be supplied. This function cannot
prove that the result was adopted, its fence is current, or tenant access live.
"""
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.semantic import V2SemanticAssembleParams,SemanticTransferParams,TransferCandidate,EditIntent
from photo_worker.contracts.wire import TaskCommand,TaskResult


def verify_candidate_producer(assembly,intent,candidate_ref,candidate,command,result,transfer,*,check_active=lambda:None):
    check_active()
    def owned(value,model):return model.model_validate_json(canonical_bytes(value))
    assembly=owned(assembly,V2SemanticAssembleParams);intent=owned(intent,EditIntent)
    candidate_ref=owned(candidate_ref,ArtifactRef);candidate=owned(candidate,TransferCandidate)
    command=owned(command,TaskCommand);result=owned(result,TaskResult);transfer=owned(transfer,SemanticTransferParams)
    verify_candidate_bindings(assembly,intent,candidate_ref,candidate,transfer,check_active=check_active)
    if (command.operation!='semantic.transfer' or command.operation_version!='1' or command.resource_class!='gpu'
            or command.params.sha256!=canonical_hash(transfer)):
        raise ValueError('SEMANTIC_PRODUCER_COMMAND_INVALID')
    if command.model is not None and command.model.lock_id!=transfer.model_lock_sha:
        raise ValueError('SEMANTIC_PRODUCER_BINDING_CHANGED')
    if (result.type!='task.succeeded' or result.error is not None
            or (result.tenant_id,result.run_id,result.task_id,result.attempt)!=
                (command.tenant_id,command.run_id,command.task_id,command.attempt)):
        raise ValueError('SEMANTIC_PRODUCER_RESULT_INVALID')
    outputs={(ref.artifact_id,ref.sha256) for ref in result.outputs}
    if len(outputs)!=len(result.outputs) or len({ref.artifact_id for ref in result.outputs})!=len(result.outputs):
        raise ValueError('SEMANTIC_PRODUCER_OUTPUT_INVALID')
    # V2 wire result publishes only the document root; mask/transform pixels
    # are independently verified through the adopted typed closure inventory.
    if result.outputs!=[candidate_ref]:raise ValueError('SEMANTIC_PRODUCER_OUTPUT_INVALID')
    check_active()


def verify_candidate_bindings(assembly,intent,candidate_ref,candidate,transfer,*,check_active=lambda:None):
    """Bind content after an independent DB producer/result/fence authority gate."""
    check_active()
    def owned(value,model):return model.model_validate_json(canonical_bytes(value))
    assembly=owned(assembly,V2SemanticAssembleParams);intent=owned(intent,EditIntent)
    candidate_ref=owned(candidate_ref,ArtifactRef);candidate=owned(candidate,TransferCandidate)
    transfer=owned(transfer,SemanticTransferParams)
    if (canonical_hash(intent)!=assembly.intent.sha256 or candidate_ref not in assembly.candidates
            or canonical_hash(candidate)!=candidate_ref.sha256):
        raise ValueError('SEMANTIC_PRODUCER_CONTENT_CHANGED')
    if (transfer.intent!=assembly.intent or transfer.reference!=intent.reference or transfer.target!=candidate.target
            or transfer.calibration!=assembly.calibration or transfer.model_lock_sha!=assembly.model_lock_sha):
        raise ValueError('SEMANTIC_PRODUCER_BINDING_CHANGED')
    check_active()
