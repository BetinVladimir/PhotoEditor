"""API-scoped original metadata preparation; publication is a separate lifecycle.

Caller authenticates the principal and verifies WorkingImage/Canvas bytes and
explicit field approval. SQL reader independently repeats current original ACL.
No metadata-source artifact becomes ready merely by returning this value.
"""
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from .adoption import AdoptionInvalid
from .delivery_metadata_original_reader import MetadataOriginalScope,DeliveryMetadataOriginalReader
from .delivery_metadata_original_prepare import prepare_delivery_metadata_original
from .delivery_metadata_prepare import PreparedDeliveryMetadata


@dataclass(frozen=True)
class PreparedAuthorizedMetadataOriginal:
    scope: MetadataOriginalScope
    binding_jcs: bytes
    prepared: PreparedDeliveryMetadata


def prepare_authorized_metadata_original(connection,objects,*,scope,binding,metadata_id,
        policy,approved_fields=(),allowed_bucket,scratch_root,max_bytes,raw_executable=None,raw_tool_sha=None,
        check_active=lambda:None):
    check_active()
    if not isinstance(scope,MetadataOriginalScope) or not isinstance(binding,VersionBinding):
        raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_SERVICE_SCOPE')
    # Own binding before reader callbacks and bind both version and WorkingImage.
    binding_raw=canonical_bytes(binding)
    binding=VersionBinding.model_validate_json(binding_raw)
    if (binding.asset_version_id!=scope.version
            or binding.source.artifact_id!=scope.source.artifact_id
            or binding.source.sha256!=scope.source.sha256):
        raise AdoptionInvalid('DELIVERY_METADATA_ORIGINAL_SERVICE_BINDING')
    original=ArtifactRef(artifact_id=scope.original.artifact_id,sha256=scope.original.sha256)
    reader=DeliveryMetadataOriginalReader(connection,objects,scope=scope,
        allowed_bucket=allowed_bucket,check_active=check_active)
    prepared=prepare_delivery_metadata_original(reader,original,binding,metadata_id,policy,
        approved_fields=approved_fields,scratch_root=scratch_root,max_bytes=max_bytes,
        raw_executable=raw_executable,raw_tool_sha=raw_tool_sha,check_active=check_active)
    check_active()
    return PreparedAuthorizedMetadataOriginal(scope,binding_raw,prepared)
