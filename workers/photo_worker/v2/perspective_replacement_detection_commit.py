"""Statement selection only; executor must retain current physical source locks."""
from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid
from .perspective_lines_adoption_commit import perspective_lines_commit_statement


def replacement_detection_commit_statement(tenant_id,task_id,context,inventory,digest,prepared,proof_jcs):
    # Reuse exact prepared result/evidence hash and attempt/fence/root checks.
    _, args = perspective_lines_commit_statement(tenant_id,task_id,context,inventory,digest,prepared)
    if not isinstance(proof_jcs,bytes) or not 2 <= len(proof_jcs) <= 4*1024*1024:
        raise AdoptionInvalid('REPLACEMENT_DETECTION_COMMIT_PROOF_INVALID')
    proof = parse_json(proof_jcs,4*1024*1024)
    try:
        if (not isinstance(proof,dict) or set(proof) != {'actor_id','auth_session_id','decision_hash','descriptor_jcs','original_jcs','plan','project_id','review_id','snapshot_id'}
                or canonical_bytes(proof) != proof_jcs):
            raise ValueError()
        descriptor = parse_json(proof['descriptor_jcs'].encode(),3*1024*1024)
        evidence = parse_json(prepared.evidence_jcs,262144)
        if (descriptor['tenant_id'] != str(tenant_id) or descriptor['task_id'] != str(task_id)
                or descriptor['run_id'] != evidence['run_id'] or descriptor['params_jcs'].encode() != prepared.params_bytes
                or descriptor['kind'] != 'v29_perspective_replacement_detection_v1'):
            raise ValueError()
    except (ValueError,TypeError,KeyError,AttributeError) as error:
        raise AdoptionInvalid('REPLACEMENT_DETECTION_COMMIT_PROOF_INVALID') from error
    return ('SELECT photo.adopt_v29_replacement_detector('+','.join(['%s']*11)+')',args+(proof_jcs.decode(),))
