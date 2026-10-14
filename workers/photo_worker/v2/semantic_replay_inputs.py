"""Pinned document loader joining semantic intent authority to mask replay."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef,WorkingImage,ResolvedTransform
from photo_worker.contracts.semantic import SemanticTransferParams,EditIntent,TransferCandidate,bind_semantic_transfer_documents
from photo_worker.operations.session.contracts import Calibration
from .adoption import ArtifactRef,AdoptionInvalid,MAX_JSON_BYTES
from .semantic_adoption import verify_semantic_geometry
from .semantic_replay import verify_semantic_mask_replay


def verify_pinned_semantic_replay(reader,params_ref,root_ref,*,source_protection_ref,target_protection_ref,
        verify_authority,scratch_root,max_bytes,max_ram_bytes,check_active=lambda:None):
    """Protection refs are installer supplied, never inferred from the output.

    Authority callback must prove pinned model/runtime/ICC/scene planarity,
    snapshot membership, protection origin and live scoped access. Document
    content and replay do not establish those external facts themselves.
    """
    if not callable(verify_authority):raise AdoptionInvalid('SEMANTIC_REPLAY_AUTHORITY_REQUIRED')
    check_active()
    if not all(isinstance(value,ArtifactRef) for value in
            (params_ref,root_ref,source_protection_ref,target_protection_ref)):
        raise AdoptionInvalid('SEMANTIC_REPLAY_REFS_INVALID')
    params_ref,root_ref,source_protection_ref,target_protection_ref=deepcopy(
        (params_ref,root_ref,source_protection_ref,target_protection_ref))
    def ref(value):return ArtifactRef(value.artifact_id,value.sha256)
    def document(value,model,source):
        check_active();evidence=reader.evidence(value)
        if evidence.ref!=value or evidence.source!=source or evidence.media_type!='application/json':
            raise AdoptionInvalid('SEMANTIC_REPLAY_EVIDENCE_INVALID')
        raw=reader.read_json(value,MAX_JSON_BYTES);check_active()
        if (not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=value.sha256):raise AdoptionInvalid('SEMANTIC_REPLAY_BYTES_INVALID')
        try:return model.model_validate_json(raw)
        except ValueError as error:raise AdoptionInvalid('SEMANTIC_REPLAY_CONTRACT_INVALID') from error
    params=document(params_ref,SemanticTransferParams,'input')
    intent=document(ref(params.intent),EditIntent,'input');calibration=document(ref(params.calibration),Calibration,'input')
    try:params,intent,calibration=bind_semantic_transfer_documents(params,intent,calibration)
    except ValueError as error:raise AdoptionInvalid('SEMANTIC_REPLAY_DOCUMENT_BINDING_INVALID') from error
    candidate=document(root_ref,TransferCandidate,'output')
    mask=verify_semantic_geometry(reader,params_ref,root_ref,check_active=check_active)
    if mask is None:
        check_active();verify_authority(deepcopy(params),deepcopy(intent),deepcopy(calibration),None,[]);check_active()
        return None
    source=document(ref(params.reference.source),WorkingImage,'input')
    selected=document(ref(intent.reference_mask),MaskRef,'input')
    guards=[document(value,MaskRef,'input') for value in (source_protection_ref,target_protection_ref)]
    transform=document(ref(candidate.transform),ResolvedTransform,'output')
    if (selected.protected or selected.canvas!=source.canvas or selected.transform_chain!=source.transform_chain
            or canonical_hash(source.canvas)!=params.reference.canvas_hash):
        raise AdoptionInvalid('SEMANTIC_REPLAY_SOURCE_MASK_CHANGED')
    # Own all documents before external verifier code; it cannot change replay.
    check_active();verify_authority(deepcopy(params),deepcopy(intent),deepcopy(calibration),
        deepcopy(selected),deepcopy(guards));check_active()
    return verify_semantic_mask_replay(reader,selected,mask,*guards,transform,scratch_root=scratch_root,
        max_bytes=max_bytes,max_ram_bytes=max_ram_bytes,check_active=check_active)


def load_semantic_protection_refs(connection,tenant_id,task_id,attempt,fence,result_id):
    """Scheduler primary-DB read; exact pending result scope, no object IO."""
    if not connection.autocommit:raise ValueError('SEMANTIC_PROTECTION_AUTOCOMMIT_REQUIRED')
    rows=connection.execute('SELECT * FROM photo.v27_semantic_adoption_protection(%s,%s,%s,%s,%s)',
        (tenant_id,task_id,attempt,fence,result_id)).fetchall()
    if len(rows)!=1:raise AdoptionInvalid('SEMANTIC_PROTECTION_INPUTS_STALE')
    row=rows[0]
    try:return tuple(ArtifactRef.parse({'artifact_id':str(row[n]),'sha256':row[n+1]}) for n in (0,2))
    except (IndexError,TypeError,ValueError) as error:raise AdoptionInvalid('SEMANTIC_PROTECTION_INPUTS_INVALID') from error
