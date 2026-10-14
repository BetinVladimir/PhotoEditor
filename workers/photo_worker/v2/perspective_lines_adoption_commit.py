"""Dedicated final SQL statement selection for prepared detector adoption."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid
from .perspective_lines_adoption_prepare import PreparedPerspectiveLinesAdoption


def perspective_lines_commit_statement(tenant_id,task_id,context,inventory,digest,prepared):
    if not isinstance(prepared,PreparedPerspectiveLinesAdoption):
        raise AdoptionInvalid('PERSPECTIVE_LINES_COMMIT_INVALID')
    evidence=parse_json(prepared.evidence_jcs,262144)
    if (canonical_bytes(evidence)!=prepared.evidence_jcs or evidence['task_id']!=str(task_id)
            or evidence['result_id']!=str(context.result_id) or evidence['attempt']!=context.attempt
            or evidence['fence']!=context.fence
            or evidence['root']!={'artifact_id':str(context.root.artifact_id),'sha256':context.root.sha256}
            or sha256(prepared.result_bytes).hexdigest()!=context.root.sha256
            or sha256(prepared.params_bytes).hexdigest()!=evidence['params_ref']['sha256']):
        raise AdoptionInvalid('PERSPECTIVE_LINES_COMMIT_INVALID')
    return ('SELECT photo.adopt_v29_perspective_lines('+','.join(['%s']*10)+')',
        (tenant_id,task_id,context.attempt,context.fence,context.result_id,inventory,digest,
         prepared.evidence_jcs.decode('utf-8'),prepared.params_bytes.decode('utf-8'),prepared.result_bytes.decode('utf-8')))
