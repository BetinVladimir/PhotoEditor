"""Logical metadata.write result binding; physical verification is mandatory too."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID,uuid5
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataParams,MetadataReport
from .delivery_metadata_freeze import verify_frozen_delivery_metadata


@dataclass(frozen=True)
class ValidatedDeliveryMetadataResult:
    params_jcs: bytes
    before_jcs: bytes
    after_jcs: bytes
    report_jcs: bytes
    projection_jcs: bytes


def validate_delivery_metadata_result(task,encoded_ref,params,before,after,report,raw,pin,binding,metadata_ref,
        source_raw,*,policy,approved_fields=()):
    if (not isinstance(task,UUID) or task.int==0 or not isinstance(encoded_ref,ArtifactRef)
            or encoded_ref.artifact_id.int==0 or policy not in ('copyright','approved_exif')):
        raise ValueError('DELIVERY_METADATA_RESULT_SCOPE')
    # Own and revalidate even models constructed through unchecked model_copy.
    params=MetadataParams.model_validate_json(canonical_bytes(params))
    before=EncodedImage.model_validate_json(canonical_bytes(before))
    after=EncodedImage.model_validate_json(canonical_bytes(after))
    report=MetadataReport.model_validate_json(canonical_bytes(report))
    if encoded_ref.sha256!=sha256(canonical_bytes(before)).hexdigest():
        raise ValueError('DELIVERY_METADATA_RESULT_SOURCE_SHA')
    frozen=verify_frozen_delivery_metadata(raw,pin,binding,metadata_ref,source_raw,
        policy=policy,approved_fields=approved_fields)
    projection=canonical_bytes(frozen.projection)
    projected_ref=ArtifactRef(artifact_id=uuid5(task,'delivery:metadata:projection:1'),sha256=sha256(projection).hexdigest())
    expected_policy='copyright_only' if policy=='copyright' else 'safe_camera'
    expected_gps=policy=='approved_exif' and 'gps' in frozen.approved_fields
    if (params.encoded!=encoded_ref or params.document!=projected_ref or params.policy!=expected_policy
            or params.copy_gps!=expected_gps or before.metadata_policy!='strip'
            or after.metadata_policy!=expected_policy):
        raise ValueError('DELIVERY_METADATA_RESULT_PARAMS')
    if (report.encoded!=params.encoded or report.document!=params.document or report.policy!=params.policy):
        raise ValueError('DELIVERY_METADATA_RESULT_REPORT')
    expected_fields=[name for name in ('author','copyright','camera_make','camera_model','lens','datetime_original','gps')
        if getattr(frozen.projection,name) is not None]
    if report.copied_fields!=expected_fields:
        raise ValueError('DELIVERY_METADATA_RESULT_FIELDS')
    ids=[encoded_ref.artifact_id,before.artifact.artifact_id,after.artifact.artifact_id,
        params.document.artifact_id,binding.source.artifact_id,metadata_ref.artifact_id]
    if any(identifier.int==0 for identifier in ids) or len(set(ids))!=len(ids):
        raise ValueError('DELIVERY_METADATA_RESULT_ALIAS')
    expected=before.model_copy(update={'artifact':after.artifact,'metadata_policy':expected_policy})
    if canonical_bytes(expected)!=canonical_bytes(after):
        raise ValueError('DELIVERY_METADATA_RESULT_RASTER_CLAIMS')
    return ValidatedDeliveryMetadataResult(canonical_bytes(params),canonical_bytes(before),canonical_bytes(after),
        canonical_bytes(report),projection)
