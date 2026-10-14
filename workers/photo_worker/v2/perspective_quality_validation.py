"""Validate a scoped master quality report; never authorize version Apply.

The reader must pin current tenant/task/result/attempt/fence and immutable object
versions before and after IO. This verifier validates worker measurement evidence,
not independent pixel recomputation or perceptual quality.
"""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.quality import QualityParams, QualityReport
from photo_worker.operations.quality.decision import _decision
from .adoption import AdoptionInvalid, ArtifactRef


def verify_perspective_quality_result(reader, params_ref, report_ref, *,
                                      expected_target, check_active=lambda: None):
    if not all(isinstance(ref, ArtifactRef) for ref in
               (params_ref, report_ref, expected_target)):
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_BINDING_INVALID')
    if len({ref.artifact_id for ref in (params_ref, report_ref, expected_target)}) != 3:
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_BINDING_INVALID')

    def document(ref, model, source, role, limit):
        check_active()
        meta = reader.evidence(ref)
        if (meta.ref != ref or meta.source != source or meta.role != role or
                meta.media_type != 'application/json' or
                type(meta.bytes) is not int or not 2 <= meta.bytes <= limit):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_EVIDENCE_INVALID')
        raw = reader.read_json(ref, limit)
        check_active()
        if (not isinstance(raw, bytes) or len(raw) != meta.bytes or
                sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_BYTES_INVALID')
        try:
            # Reject duplicate fields before strict JSON-mode UUID parsing.
            parse_json(raw, max_bytes=limit)
            value = model.model_validate_json(raw)
            if canonical_bytes(value) != raw:
                raise ValueError('noncanonical quality document')
        except ValueError as exc:
            raise AdoptionInvalid('PERSPECTIVE_QUALITY_CONTRACT_INVALID') from exc
        return value

    params = document(params_ref, QualityParams, 'input', 'task_params', 65536)
    report = document(report_ref, QualityReport, 'output', 'quality_report', 1048576)
    target = {'artifact_id': str(expected_target.artifact_id), 'sha256': expected_target.sha256}
    if (params.target.model_dump(mode='json') != target or
            params.target_kind != 'working' or params.stage != 'master' or
            params.threshold_profile_id != 'master_v1' or params.baseline is not None or
            params.protected or params.seams):
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_BINDING_INVALID')
    if (report.target != params.target or report.target_kind != params.target_kind or
            report.stage != params.stage or report.threshold_profile_id != params.threshold_profile_id or
            report.reference is not None or report.technical.seam_p99_delta != 0):
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_BINDING_INVALID')
    status, reasons = _decision(report.technical, None)
    if report.status != status or report.reasons != reasons:
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_DECISION_INVALID')
    if reader.staged_outputs() != {report_ref}:
        raise AdoptionInvalid('PERSPECTIVE_QUALITY_OUTPUTS_INVALID')
    check_active()
    return report
