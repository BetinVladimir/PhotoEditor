"""Read adopted transfer producer parameters through scheduler-only SQL scope.

Object reads use the producer's exact task/attempt/fence, outside transactions.
Final adoption must independently recheck live scope; this provider does not
replace snapshot authority, model qualification or scene/protection replay.
"""
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams, EditIntent, TransferCandidate, SemanticTransferParams
from photo_worker.operations.semantic.provenance import verify_candidate_bindings
from .adoption import ArtifactRef, AdoptionInvalid, MAX_JSON_BYTES
from .adoption_store import PostgresObjectEvidenceReader


def verify_adopted_semantic_producers(connection,objects,allowed_bucket,tenant_id,task_id,context,
                                     assembly,intent,snapshot,candidates,*,check_active=lambda:None):
    if not connection.autocommit:
        raise ValueError('Semantic producer preparation requires autocommit')
    check_active()
    assembly=V2SemanticAssembleParams.model_validate_json(canonical_bytes(assembly))
    intent=EditIntent.model_validate_json(canonical_bytes(intent))
    if not isinstance(candidates,(list,tuple)) or len(candidates)!=len(assembly.candidates):
        raise AdoptionInvalid('SEMANTIC_PRODUCER_COVERAGE_INVALID')
    # Own the complete list before callbacks/IO can mutate installer arguments.
    candidates=[TransferCandidate.model_validate_json(canonical_bytes(value)) for value in candidates]
    for candidate_ref,candidate in zip(assembly.candidates,candidates):
        check_active()
        rows=connection.execute(
            'SELECT * FROM photo.v27_semantic_assembly_producer(%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id,
             candidate_ref.artifact_id,candidate_ref.sha256)).fetchall()
        if len(rows)!=1:
            raise AdoptionInvalid('SEMANTIC_PRODUCER_SCOPE_STALE')
        producer,attempt,fence,result_id,params_id,params_sha=rows[0]
        if (not isinstance(producer,UUID) or not isinstance(result_id,UUID)
                or type(attempt) is not int or attempt<1 or type(fence) is not int or fence<1):
            raise AdoptionInvalid('SEMANTIC_PRODUCER_EVIDENCE_INVALID')
        params_ref=ArtifactRef.parse({'artifact_id':str(params_id),'sha256':params_sha})
        reader=PostgresObjectEvidenceReader(connection,objects,tenant_id=tenant_id,task_id=producer,
            attempt=attempt,fence=fence,allowed_bucket=allowed_bucket)
        evidence=reader.evidence(params_ref)
        if evidence.ref!=params_ref or evidence.source!='input' or evidence.media_type!='application/json':
            raise AdoptionInvalid('SEMANTIC_PRODUCER_EVIDENCE_INVALID')
        raw=reader.read_json(params_ref,MAX_JSON_BYTES)
        check_active()
        if (not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=params_ref.sha256):
            raise AdoptionInvalid('SEMANTIC_PRODUCER_BYTES_INVALID')
        try:
            transfer=SemanticTransferParams.model_validate_json(raw)
            verify_candidate_bindings(assembly,intent,candidate_ref,candidate,transfer,check_active=check_active)
        except ValueError as error:
            raise AdoptionInvalid('SEMANTIC_PRODUCER_BINDING_INVALID') from error
    check_active()


def adopted_semantic_producer_authority(connection,objects,allowed_bucket,*,check_active=lambda:None):
    """Install the DB-backed callback expected by AdoptionCoordinator."""
    if not connection.autocommit or not isinstance(allowed_bucket,str) or not allowed_bucket:
        raise ValueError('Semantic producer authority requires autocommit and bucket')
    if not callable(check_active):
        raise ValueError('Semantic producer activity check must be callable')
    def verify(tenant_id,task_id,context,assembly,intent,snapshot,candidates):
        return verify_adopted_semantic_producers(connection,objects,allowed_bucket,tenant_id,task_id,context,
            assembly,intent,snapshot,candidates,check_active=check_active)
    return verify
