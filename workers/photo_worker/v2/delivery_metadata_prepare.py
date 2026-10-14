"""Compose original-byte verification with an exact frozen metadata projection.

Original/version ACL and approval come from authenticated admission. Returned
bytes still require reservation, immutable publication and SQL completion.
"""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from .delivery_metadata_source import prepare_delivery_metadata_source,PreparedMetadataSource
from .delivery_metadata_projection import project_delivery_metadata
from .delivery_metadata_freeze import freeze_delivery_metadata


@dataclass(frozen=True)
class PreparedDeliveryMetadata:
    source: PreparedMetadataSource
    metadata_ref_jcs: bytes
    frozen_jcs: bytes
    frozen_sha256: str


def prepare_delivery_metadata(path,original,mime,expected_bytes,binding,metadata_id,policy,*,
        approved_fields=(),max_bytes,raw_executable=None,raw_tool_sha=None,check_active=lambda:None):
    check_active()
    if (not isinstance(binding,VersionBinding) or not isinstance(original,ArtifactRef)
            or not isinstance(metadata_id,UUID) or metadata_id.int==0
            or metadata_id in (original.artifact_id,binding.source.artifact_id)
            or original.artifact_id==binding.source.artifact_id
            or binding.asset_version_id.int==0 or binding.source.artifact_id.int==0):
        raise ValueError('DELIVERY_METADATA_PREPARE_BINDING')
    # Fail closed before reading private metadata if policy/approval is malformed.
    project_delivery_metadata(MetadataDocument(),policy,approved_fields=approved_fields)
    if policy!='approved_exif' and approved_fields:
        raise ValueError('DELIVERY_METADATA_APPROVAL')
    binding=VersionBinding.model_validate_json(canonical_bytes(binding))
    original=ArtifactRef.model_validate_json(canonical_bytes(original))
    prepared=prepare_delivery_metadata_source(path,original,mime,expected_bytes,
        max_bytes=max_bytes,include_gps=policy=='approved_exif' and 'gps' in approved_fields,
        raw_executable=raw_executable,raw_tool_sha=raw_tool_sha,check_active=check_active)
    check_active()
    ref=ArtifactRef(artifact_id=metadata_id,sha256=prepared.metadata_sha256)
    frozen=freeze_delivery_metadata(binding,ref,prepared.metadata_jcs,policy,
        approved_fields=approved_fields)
    check_active()
    return PreparedDeliveryMetadata(prepared,canonical_bytes(ref),frozen,sha256(frozen).hexdigest())
