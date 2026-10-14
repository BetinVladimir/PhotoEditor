"""Package staged exposure and measured quality into the shared ImageResult."""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.operations.optical.contracts import ImageReport, ImageResult


@dataclass(frozen=True)
class PublishedExposure:
    root: ArtifactRef
    review_required: bool
    bytes_written: int


def publish_exposure_result(artifacts, edit, staged, report, *, verify_report,
        verify_admission, check_active, max_output_bytes):
    if not all(callable(value) for value in (verify_report, verify_admission, check_active)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    report = ImageReport.model_validate_json(canonical_bytes(report))
    image = ArtifactRef.model_validate_json(canonical_bytes(staged.image))
    pixels = ArtifactRef.model_validate_json(canonical_bytes(staged.pixels))
    if (edit.kind != 'local_exposure' or report.operation != 'semantic.local_exposure'
            or report.status not in ('pass', 'review') or report.sources != [edit.source]
            or report.outside_mask_max_error != 0 or report.clipping_fraction != 0
            or report.skipped_count != 0 or image.artifact_id.int == 0
            or pixels.artifact_id.int == 0 or image.artifact_id == pixels.artifact_id
            or type(staged.bytes_written) is not int or staged.bytes_written < 1):
        raise ValueError('SEMANTIC_EXPOSURE_REPORT_INVALID')
    report_bytes = canonical_bytes(report)
    # A report ref changes only fixed-length UUID/SHA fields in the root.
    placeholder = ArtifactRef(artifact_id=image.artifact_id, sha256='f'*64)
    result = ImageResult(schema_version=1, working_image=image, report=placeholder,
        changed_mask=edit.mask.model_copy(deep=True), generated_mask=None)
    required = staged.bytes_written + len(report_bytes) + len(canonical_bytes(result))
    if type(max_output_bytes) is not int or not required <= max_output_bytes <= 2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission(edit.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
        check_active()
    admitted()
    if verify_report(edit.model_copy(deep=True), image.model_copy(deep=True),
                     report.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_REPORT_UNVERIFIED')
    admitted()
    report_ref = ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(report, 'report')))
    if (report_ref.artifact_id.int == 0 or report_ref.artifact_id in
            (image.artifact_id, pixels.artifact_id) or canonical_bytes(report) != report_bytes):
        raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
    result = result.model_copy(update={'report': report_ref})
    root_bytes = canonical_bytes(result)
    admitted()
    root = ArtifactRef.model_validate_json(canonical_bytes(artifacts.stage_contract(result, 'document')))
    if (root.artifact_id.int == 0 or root.artifact_id in
            (image.artifact_id, pixels.artifact_id, report_ref.artifact_id)
            or canonical_bytes(result) != root_bytes):
        raise ValueError('SEMANTIC_EXPOSURE_OUTPUT_INVALID')
    admitted()
    return PublishedExposure(root, report.status == 'review',
        staged.bytes_written + len(report_bytes) + len(root_bytes))
