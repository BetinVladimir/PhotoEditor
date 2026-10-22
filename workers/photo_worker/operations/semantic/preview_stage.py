"""Preview WorkingImage staging; never publishes an Apply or preview receipt."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput,ExposurePreviewNumericalEvidence
from .preview_binding import bind_exposure_preview_source
from .exposure_stage import stage_exposure_working_image


def stage_exposure_preview(artifacts,params,source_raw,encoded,*,verify_admission,
        check_active,max_bytes,max_output_bytes):
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    image=bind_exposure_preview_source(owned,source_raw)
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    staged=stage_exposure_working_image(artifacts,image,encoded,admitted=admitted,
        check_active=check_active,max_bytes=max_bytes,max_output_bytes=max_output_bytes)
    inputs={ref.artifact_id for ref in (owned.plan,owned.intent,owned.candidate,
        owned.source.source,owned.protection)}
    if staged.pixels.artifact_id in inputs or staged.image.artifact_id in inputs:
        raise ValueError('SEMANTIC_PREVIEW_ARTIFACT_ALIAS')
    return staged


@dataclass(frozen=True)
class StagedExposurePreview:
    pixels: ArtifactRef
    image: ArtifactRef
    evidence: ArtifactRef
    bytes_written: int


def execute_and_stage_exposure_preview(artifacts,params,*,verify_admission,
        verify_origin,verify_color,check_active,max_bytes,max_output_bytes):
    """Compute exact protected output and stage it; receipt publication is external."""
    from photo_worker.contracts.images import WorkingImage
    from .preview_loader import execute_artifact_exposure_preview
    if not all(callable(value) for value in (verify_admission,verify_origin,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=SemanticExposurePreviewInput.model_validate_json(canonical_bytes(params))
    if (type(max_bytes) is not int or not 64*1024**2<=max_bytes<=2**53-1
            or type(max_output_bytes) is not int or not 1<=max_output_bytes<=2**53-1):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    check_active()
    source_raw=artifacts.contract_bytes(owned.source.source,WorkingImage)
    image=bind_exposure_preview_source(owned,source_raw)
    pixels=image.canvas.width*image.canvas.height
    if max_output_bytes<12*pixels+16384+1048576:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    encoded=execute_artifact_exposure_preview(artifacts,owned,
        verify_admission=verify_admission,verify_origin=verify_origin,
        verify_color=verify_color,max_bytes=max_bytes,check_active=check_active)
    # The computed output remains resident while staging owns another copy.
    staged=stage_exposure_preview(artifacts,owned,source_raw,encoded,
        verify_admission=verify_admission,check_active=check_active,
        max_bytes=max_bytes-12*pixels,max_output_bytes=max_output_bytes)
    evidence=ExposurePreviewNumericalEvidence(schema_version=1,params=owned.model_copy(deep=True),
        working_image=staged.image.model_copy(deep=True),pixels=staged.pixels.model_copy(deep=True),
        algorithm='local_exposure_exact_replay_v1',protected_pixels_unchanged=True,
        outside_mask_max_error=0.)
    evidence_bytes=canonical_bytes(evidence)
    if staged.bytes_written+len(evidence_bytes)>max_output_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    check_active()
    evidence_ref=ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(evidence,'preview_evidence')))
    ids={ref.artifact_id for ref in (owned.plan,owned.intent,owned.candidate,
        owned.source.source,owned.protection,staged.image,staged.pixels)}
    if (evidence_ref.artifact_id.int==0 or evidence_ref.artifact_id in ids
            or evidence_ref.sha256!=sha256(evidence_bytes).hexdigest()
            or canonical_bytes(evidence)!=evidence_bytes):
        raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    check_active()
    return StagedExposurePreview(staged.pixels,staged.image,evidence_ref,
        staged.bytes_written+len(evidence_bytes))
