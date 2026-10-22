"""Stage complete presentation linkage after media and numerical verification."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import (ExposurePreviewNumericalEvidence,
    ExposurePreviewPresentation, RemovalPreviewNumericalEvidence, RemovalPreviewPresentation)
from .preview_media_stage import StagedPreviewMedia


def stage_preview_presentation(artifacts, *, evidence_ref,evidence_raw,media,width,height,
        icc_lock_sha,srgb_sha,max_output_bytes,verify_admission,check_active):
    return _stage_presentation(artifacts, evidence_ref=evidence_ref, evidence_raw=evidence_raw,
        media=media, width=width, height=height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes,
        verify_admission=verify_admission, check_active=check_active,
        evidence_model=ExposurePreviewNumericalEvidence, presentation_model=ExposurePreviewPresentation)


def stage_removal_preview_presentation(artifacts, *, evidence_ref, evidence_raw, media,
        width, height, icc_lock_sha, srgb_sha, max_output_bytes, verify_admission, check_active):
    return _stage_presentation(artifacts, evidence_ref=evidence_ref, evidence_raw=evidence_raw,
        media=media, width=width, height=height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes,
        verify_admission=verify_admission, check_active=check_active,
        evidence_model=RemovalPreviewNumericalEvidence, presentation_model=RemovalPreviewPresentation)


def _stage_presentation(artifacts, *, evidence_ref, evidence_raw, media, width, height,
        icc_lock_sha, srgb_sha, max_output_bytes, verify_admission, check_active,
        evidence_model, presentation_model):
    if (not isinstance(media,StagedPreviewMedia) or type(media.bytes_written) is not int
            or media.bytes_written<1 or not callable(verify_admission) or not callable(check_active)
            or type(max_output_bytes) is not int or not 1<=max_output_bytes<=2**53-1
            or not isinstance(evidence_raw,bytes) or not 2<=len(evidence_raw)<=1048576):
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    ref=ArtifactRef.model_validate_json(canonical_bytes(evidence_ref))
    if ref.artifact_id.int==0 or ref.sha256!=sha256(evidence_raw).hexdigest():
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    evidence=evidence_model.model_validate_json(evidence_raw)
    if canonical_bytes(evidence) != evidence_raw:
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    document=presentation_model(schema_version=1,params=evidence.params.model_copy(deep=True),
        numerical_evidence=ref,working_image=evidence.working_image,pixels=evidence.pixels,
        before=media.before,after=media.after,affected=media.affected,protected=media.protected,
        width=width,height=height,icc_lock_sha=icc_lock_sha,srgb_sha=srgb_sha,
        resize='area_linear_premult_v1',overlay='max_coverage_ceil_alpha_v1')
    raw=canonical_bytes(document)
    if len(raw)>1048576 or media.bytes_written+len(raw)>max_output_bytes:
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_BUDGET')
    def admitted():
        check_active()
        if verify_admission() is not True:raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_DENIED')
        check_active()
    admitted()
    result=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(document,'preview_presentation')))
    ids={value.artifact_id for value in (ref,evidence.working_image,evidence.pixels,
        document.params.plan,document.params.intent,document.params.candidate,
        document.params.source.source,document.params.protection,media.before,media.after,media.affected,media.protected)}
    if (result.artifact_id.int==0 or result.artifact_id in ids
            or result.sha256!=sha256(raw).hexdigest() or canonical_bytes(document)!=raw):
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    admitted()
    return result,media.bytes_written+len(raw)


def stage_background_cleanup_preview_presentation(artifacts, *, evidence_ref,evidence_raw,media,
        width,height,icc_lock_sha,srgb_sha,max_output_bytes,verify_admission,check_active):
    from photo_worker.contracts.semantic_preview import (BackgroundCleanupPreviewNumericalEvidence,
        BackgroundCleanupPreviewPresentation)
    return _stage_presentation(artifacts,evidence_ref=evidence_ref,evidence_raw=evidence_raw,
        media=media,width=width,height=height,icc_lock_sha=icc_lock_sha,srgb_sha=srgb_sha,
        max_output_bytes=max_output_bytes,verify_admission=verify_admission,check_active=check_active,
        evidence_model=BackgroundCleanupPreviewNumericalEvidence,
        presentation_model=BackgroundCleanupPreviewPresentation)


def stage_retouch_preview_presentation(artifacts, *, evidence_ref, evidence_raw, media,
        width, height, icc_lock_sha, srgb_sha, max_output_bytes, verify_admission, check_active):
    from photo_worker.contracts.semantic_preview import (RetouchPreviewNumericalEvidence,
        RetouchPreviewPresentation)
    if not isinstance(evidence_raw, bytes) or not 2 <= len(evidence_raw) <= 1048576:
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    evidence = RetouchPreviewNumericalEvidence.model_validate_json(evidence_raw)
    if canonical_bytes(evidence) != evidence_raw:
        raise ValueError('SEMANTIC_PREVIEW_PRESENTATION_INVALID')
    return _stage_presentation(artifacts, evidence_ref=evidence_ref, evidence_raw=evidence_raw,
        media=media, width=width, height=height, icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha, max_output_bytes=max_output_bytes,
        verify_admission=verify_admission, check_active=check_active,
        evidence_model=RetouchPreviewNumericalEvidence, presentation_model=RetouchPreviewPresentation)
