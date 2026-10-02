"""Prepare source-bound capture alert evidence before the commit transaction."""

from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.capture.quality import technical_quality
from photo_worker.operations.capture.contracts import CaptureAssessParams, CaptureContext, QAReport
from .adoption import AdoptionInvalid, ArtifactRef, EvidenceReader, MAX_JSON_BYTES


@dataclass(frozen=True)
class CaptureAdoption:
    session_id: UUID
    asset_version_id: UUID
    report_ref: ArtifactRef
    severity: str
    params_jcs: bytes
    context_jcs: bytes
    report_jcs: bytes


def report_severity(report: QAReport) -> str:
    statuses = {check.status for check in report.checks}
    if 'critical' in statuses:
        return 'critical'
    if statuses & {'warning', 'unavailable'}:
        return 'warning'
    return 'info'


def prepare_capture_adoption(reader: EvidenceReader, params_ref: ArtifactRef,
                             report_ref: ArtifactRef) -> CaptureAdoption:
    def document(ref, contract, source):
        evidence = reader.evidence(ref)
        if (evidence.ref != ref or evidence.source != source or evidence.media_type != 'application/json'
            or source == 'output' and evidence.role != 'document'):
            raise AdoptionInvalid('CAPTURE_ADOPTION_SOURCE_INVALID')
        raw = reader.read_json(ref, MAX_JSON_BYTES)
        if (not isinstance(raw, bytes) or len(raw) > MAX_JSON_BYTES or len(raw) != evidence.bytes
            or sha256(raw).hexdigest() != ref.sha256):
            raise AdoptionInvalid('CAPTURE_ADOPTION_BYTES_INVALID')
        try:
            value = contract.model_validate_json(raw)
        except ValueError as error:
            raise AdoptionInvalid('CAPTURE_ADOPTION_CONTRACT_INVALID') from error
        if canonical_bytes(value) != raw:
            raise AdoptionInvalid('CAPTURE_ADOPTION_NONCANONICAL')
        return value, raw
    params, params_jcs = document(params_ref, CaptureAssessParams, 'input')
    context_ref = ArtifactRef(params.context.artifact_id, params.context.sha256)
    context, context_jcs = document(context_ref, CaptureContext, 'input')
    report, report_jcs = document(report_ref, QAReport, 'output')
    if report.binding != params.binding:
        raise AdoptionInvalid('CAPTURE_ADOPTION_BINDING_MISMATCH')
    requested = set(params.policy.checks)
    actual = {check.code for check in report.checks}
    if actual != requested:
        raise AdoptionInvalid('CAPTURE_ADOPTION_CHECK_COVERAGE')
    for check in report.checks:
        if check.status != 'unavailable' and check.score is None:
            raise AdoptionInvalid('CAPTURE_ADOPTION_SCORE_MISSING')
    try:
        technical_quality(report, params.policy)
    except ValueError as error:
        raise AdoptionInvalid('CAPTURE_ADOPTION_QUALITY_INVALID') from error
    return CaptureAdoption(context.session_id, params.binding.asset_version_id,
        report_ref, report_severity(report), params_jcs, context_jcs, report_jcs)
