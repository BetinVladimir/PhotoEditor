"""Validate actual legacy master quality reports against production thresholds."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.quality import QualityReport
from photo_worker.operations.quality.decision import _decision
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreLegacyQualityEvidence:
    target: ArtifactRef
    report_sha256: str
    threshold_profile_id: str
    status: str


def validate_legacy_master_quality(raw,target,*,check_active):
    """Pass baseline only; reviewed/failing fixtures require review evidence.

    Caller proves physical report SHA and canonical task producer separately.
    Stored status alone is insufficient: production technical thresholds are
    recomputed. This does not establish perceptual quality or replace human QA.
    """
    if (not isinstance(raw,bytes) or not 2<=len(raw)<=1048576
            or not isinstance(target,ArtifactRef) or target.artifact_id.int==0 or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_QUALITY_INSTALLATION')
    require_restore_active(check_active);report=QualityReport.model_validate_json(raw)
    if canonical_bytes(parse_json(raw))!=raw or canonical_bytes(report)!=raw:
        raise ValueError('RESTORE_LEGACY_QUALITY_CANONICAL')
    if (report.target!=target or report.target_kind!='working' or report.stage!='master'
            or report.threshold_profile_id!='master_v1' or report.status!='pass' or report.reasons):
        raise ValueError('RESTORE_LEGACY_QUALITY_BINDING')
    status,reasons=_decision(report.technical,report.reference,True,True)
    if status!='pass' or reasons:raise ValueError('RESTORE_LEGACY_QUALITY_THRESHOLDS')
    require_restore_active(check_active);return RestoreLegacyQualityEvidence(target,sha256(raw).hexdigest(),report.threshold_profile_id,status)
