"""Reassemble a SemanticPlan from authenticated inputs before adoption.

Snapshot and producer authority callbacks must use scoped database evidence;
content equality alone cannot establish tenant access or adopted producers.
"""
from copy import deepcopy
from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import V2SemanticAssembleParams, EditIntent, TransferCandidate, SemanticPlan
from photo_worker.operations.session.contracts import Calibration, InputSnapshot
from photo_worker.operations.semantic.assemble import assemble_semantic_plan
from .adoption import ArtifactRef, AdoptionInvalid, MAX_JSON_BYTES


def _load_validated_snapshot(load_snapshot, reader, intent):
    snapshot_value = load_snapshot(reader, deepcopy(intent))
    if not isinstance(snapshot_value, InputSnapshot):
        raise AdoptionInvalid('SEMANTIC_PLAN_SNAPSHOT_INVALID')
    try:
        snapshot_bytes = canonical_bytes(snapshot_value)
    except Exception as error:
        # A model instance can still be mutated after validation or built with
        # model_construct; an invalid value must not escape as a worker crash.
        raise AdoptionInvalid('SEMANTIC_PLAN_SNAPSHOT_INVALID') from error
    if len(snapshot_bytes) > MAX_JSON_BYTES:
        raise AdoptionInvalid('SEMANTIC_PLAN_SNAPSHOT_LIMIT')
    try:
        return InputSnapshot.model_validate_json(snapshot_bytes)
    except ValueError as error:
        raise AdoptionInvalid('SEMANTIC_PLAN_SNAPSHOT_INVALID') from error


def verify_semantic_plan(reader, params_ref, root_ref, *, load_snapshot, verify_provenance,
                         check_active=lambda: None):
    if not callable(load_snapshot) or not callable(verify_provenance):
        raise AdoptionInvalid('SEMANTIC_PLAN_AUTHORITY_REQUIRED')
    def ref(value):
        return ArtifactRef(value.artifact_id, value.sha256)
    def document(value, model, source):
        check_active()
        evidence = reader.evidence(value)
        if (evidence.ref != value or evidence.source != source
                or evidence.media_type != 'application/json'
                or (source == 'output' and evidence.role != 'document')):
            raise AdoptionInvalid('SEMANTIC_PLAN_EVIDENCE_INVALID')
        raw = reader.read_json(value, MAX_JSON_BYTES)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) > MAX_JSON_BYTES
                or len(raw) != evidence.bytes or sha256(raw).hexdigest() != value.sha256):
            raise AdoptionInvalid('SEMANTIC_PLAN_BYTES_INVALID')
        try:
            return model.model_validate_json(raw)
        except ValueError as error:
            raise AdoptionInvalid('SEMANTIC_PLAN_CONTRACT_INVALID') from error
    params = document(params_ref, V2SemanticAssembleParams, 'input')
    actual = document(root_ref, SemanticPlan, 'output')
    intent = document(ref(params.intent), EditIntent, 'input')
    calibration = document(ref(params.calibration), Calibration, 'input')
    snapshot = _load_validated_snapshot(load_snapshot, reader, intent)
    candidates = [document(ref(value), TransferCandidate, 'input') for value in params.candidates]
    try:
        expected = assemble_semantic_plan(params, intent, snapshot, calibration, candidates,
                                          check_active=check_active)
    except ValueError as error:
        raise AdoptionInvalid('SEMANTIC_PLAN_INPUTS_INVALID') from error
    if canonical_bytes(actual) != canonical_bytes(expected):
        raise AdoptionInvalid('SEMANTIC_PLAN_REASSEMBLY_MISMATCH')
    check_active()
    if verify_provenance(deepcopy(params), deepcopy(intent), deepcopy(snapshot), deepcopy(candidates)) is False:
        raise AdoptionInvalid('SEMANTIC_PLAN_PROVENANCE_DENIED')
    check_active()
    return expected
