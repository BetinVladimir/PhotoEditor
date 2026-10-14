"""Replacement-scoped detector replay; authoritative evidence is caller supplied.

The evidence loader must retain current mapped-source authority using the
replacement SQL/Go boundary. This module never substitutes ordinary source scope.
"""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.perspective_line_detection import PerspectiveLineDetectionParams
from photo_worker.operations.optical.perspective_lines_installation import execution_policy
from .adoption import AdoptionInvalid
from .perspective_lines_adoption_evidence import validate_perspective_lines_adoption_evidence
from .perspective_lines_adoption_prepare import PreparedPerspectiveLinesAdoption, validate_perspective_lines_adoption_installation
from .perspective_lines_replay import verify_pinned_perspective_lines_replay


def prepare_replacement_detection_replay(reader, task_id, context, params_ref, descriptor_jcs, *,
        installation, load_current_evidence, check_active):
    if not callable(load_current_evidence) or not callable(check_active):
        raise ValueError('REPLACEMENT_DETECTION_REPLAY_CONFIGURATION')
    check_active()
    owned = validate_perspective_lines_adoption_installation(installation)
    ctx, ref = deepcopy(context), deepcopy(params_ref)
    if not isinstance(descriptor_jcs, bytes) or not 2 <= len(descriptor_jcs) <= 3 * 1024 * 1024:
        raise AdoptionInvalid('REPLACEMENT_DETECTION_DESCRIPTOR_INVALID')
    descriptor = parse_json(descriptor_jcs, 3 * 1024 * 1024)
    if (not isinstance(descriptor,dict) or set(descriptor) != {'schema_version','kind','tenant_id','project_id','review_id','decision_hash','actor_id','auth_session_id','replacement','run_id','task_id','base','params_jcs','source_jcs','profile_jcs'}
            or canonical_bytes(descriptor) != descriptor_jcs
            or descriptor.get('kind') != 'v29_perspective_replacement_detection_v1'
            or type(descriptor.get('schema_version')) is not int or descriptor['schema_version'] != 1
            or descriptor.get('task_id') != str(task_id)):
        raise AdoptionInvalid('REPLACEMENT_DETECTION_DESCRIPTOR_INVALID')
    try:
        if UUID(descriptor['run_id']).int == 0:
            raise ValueError()
        raw = reader.read_json(ref,65536)
        params = PerspectiveLineDetectionParams.model_validate_json(raw)
        if (canonical_bytes(params) != raw or descriptor['params_jcs'].encode() != raw
                or descriptor['base'] != parse_json(canonical_bytes(params.base),65536)):
            raise ValueError()
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        raise AdoptionInvalid('REPLACEMENT_DETECTION_PARAMS_INVALID') from error
    def current():
        check_active()
        evidence = validate_perspective_lines_adoption_evidence(load_current_evidence(),task_id,ctx,raw)
        if (evidence['run_id'] != descriptor['run_id']
                or evidence['params_ref'] != {'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}
                or evidence['policy_jcs'] != canonical_bytes(execution_policy(owned.runtime.working_icc_sha,owned.runtime.runtime_sha)).decode()
                or evidence['source']['sha256'] != sha256(descriptor['source_jcs'].encode()).hexdigest()):
            raise AdoptionInvalid('REPLACEMENT_DETECTION_EVIDENCE_MISMATCH')
        check_active()
        return canonical_bytes(evidence)
    evidence_jcs = current()
    def checkpoint():
        if current() != evidence_jcs:
            raise AdoptionInvalid('REPLACEMENT_DETECTION_EVIDENCE_CHANGED')
    checkpoint()
    verified = verify_pinned_perspective_lines_replay(reader,ref,ctx.root,expected_base=params.base,
        working_icc_sha=owned.runtime.working_icc_sha,runtime_lock=owned.runtime.runtime_lock,
        runtime_sha=owned.runtime.runtime_sha,scratch_root=owned.scratch_root,
        max_bytes=owned.max_bytes,ram_max_bytes=owned.ram_max_bytes,check_active=check_active)
    checkpoint()
    check_active()
    return PreparedPerspectiveLinesAdoption(evidence_jcs,bytes(raw),verified.result_bytes,
        verified.lines_bytes,verified.lines_sha256)
