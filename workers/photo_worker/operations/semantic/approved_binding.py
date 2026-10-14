"""Byte/content binding only; SQL admission and object provenance are external."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.semantic import ApprovedSemanticEdit,TransferCandidate,EditIntent


def bind_approved_edit_documents(edit,candidate_raw,intent_raw):
    owned=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    def document(raw,ref,model):
        if not isinstance(raw,(bytes,bytearray)) or not 2<=len(raw)<=1_048_576:
            raise ValueError('SEMANTIC_APPROVED_DOCUMENT_INVALID')
        body=bytes(raw)
        if sha256(body).hexdigest()!=ref.sha256:
            raise ValueError('SEMANTIC_APPROVED_DOCUMENT_HASH')
        value=parse_json(body)
        if not isinstance(value,dict) or type(value.get('schema_version')) is not int or value['schema_version']!=1:
            raise ValueError('SEMANTIC_APPROVED_DOCUMENT_SCHEMA')
        return model.model_validate_json(canonical_bytes(value)),value
    candidate,_=document(candidate_raw,owned.candidate,TransferCandidate)
    intent,intent_fields=document(intent_raw,owned.intent,EditIntent)
    if (candidate.state!='proposed' or candidate.reason is not None
            or candidate.target!=owned.source or candidate.mask!=owned.mask
            or candidate.transform!=owned.transform):
        raise ValueError('SEMANTIC_APPROVED_CANDIDATE_BINDING')
    if (intent_fields.get('preserve_persons') is not True or intent.kind!=owned.kind
            or intent.strength!=owned.strength or intent.ev!=owned.ev
            or intent.prompt!=owned.prompt or not owned.preserve_persons):
        raise ValueError('SEMANTIC_APPROVED_INTENT_BINDING')
    return owned,candidate,intent
