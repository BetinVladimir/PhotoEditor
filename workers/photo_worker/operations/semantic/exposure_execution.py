"""Compute and stage an approved exposure within one admitted attempt.

The returned references still require canonical task finalization and adoption.
"""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import ApprovedSemanticEdit, TransferCandidate, EditIntent
from .exposure_source import bind_local_exposure_source
from .exposure_pipeline import execute_encoded_local_exposure, encoded_local_exposure_memory
from .exposure_stage import stage_local_exposure_result


def execute_and_stage_local_exposure(artifacts, edit, *, protection_ref,
        verify_origin, verify_color, verify_admission, check_active,
        max_bytes, max_output_bytes):
    if not all(callable(value) for value in
               (verify_origin, verify_color, verify_admission, check_active)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    owned = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
    check_active()
    source_raw = artifacts.contract_bytes(owned.source.source, WorkingImage)
    image = bind_local_exposure_source(owned, source_raw)
    pixels = image.canvas.width * image.canvas.height
    if (type(max_bytes) is not int or not 1 <= max_bytes <= 2**53-1
            or max_bytes < encoded_local_exposure_memory((image.canvas.height, image.canvas.width))
            or type(max_output_bytes) is not int or not 1 <= max_output_bytes <= 2**53-1
            or max_output_bytes < 12*pixels + 16384 + 1048576):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    candidate_raw = artifacts.contract_bytes(owned.candidate, TransferCandidate)
    intent_raw = artifacts.contract_bytes(owned.intent, EditIntent)
    encoded = execute_encoded_local_exposure(artifacts, owned, candidate_raw, intent_raw,
        protection_ref=protection_ref, verify_origin=verify_origin,
        verify_color=verify_color, verify_admission=verify_admission,
        max_bytes=max_bytes, check_active=check_active)
    # The encoded input remains resident while staging takes its own copy.
    return stage_local_exposure_result(artifacts, owned, source_raw, encoded,
        verify_admission=verify_admission, check_active=check_active,
        max_bytes=max_bytes-12*pixels, max_output_bytes=max_output_bytes)


def execute_and_publish_local_exposure(artifacts, edit, *, protection_ref,
        verify_origin, verify_color, verify_admission, check_active,
        max_bytes, max_output_bytes):
    from .exposure_pipeline import execute_measured_local_exposure
    from .exposure_result import publish_exposure_result
    owned=ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    check_active()
    if verify_admission(owned.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
    check_active()
    source_raw=artifacts.contract_bytes(owned.source.source,WorkingImage)
    image=bind_local_exposure_source(owned,source_raw)
    pixels=image.canvas.width*image.canvas.height
    if (type(max_bytes) is not int or not encoded_local_exposure_memory((image.canvas.height,image.canvas.width))<=max_bytes<=2**53-1
            or type(max_output_bytes) is not int or not 12*pixels+16384+1048576<=max_output_bytes<=2**53-1):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    from photo_worker.operations.optical.contracts import ImageReport
    encoded,report=execute_measured_local_exposure(artifacts,owned,
        artifacts.contract_bytes(owned.candidate,TransferCandidate),
        artifacts.contract_bytes(owned.intent,EditIntent),protection_ref=protection_ref,
        verify_origin=verify_origin,verify_color=verify_color,verify_admission=verify_admission,
        max_bytes=max_bytes,check_active=check_active)
    report=ImageReport.model_validate_json(canonical_bytes(report))
    measured_bytes=canonical_bytes(report)
    staged=stage_local_exposure_result(artifacts,owned,source_raw,encoded,
        verify_admission=verify_admission,check_active=check_active,
        max_bytes=max_bytes-12*pixels,max_output_bytes=max_output_bytes)
    return publish_exposure_result(artifacts,owned,staged,report,
        verify_report=lambda edit,image,value:canonical_bytes(value)==measured_bytes and image==staged.image,
        verify_admission=verify_admission,check_active=check_active,max_output_bytes=max_output_bytes)
