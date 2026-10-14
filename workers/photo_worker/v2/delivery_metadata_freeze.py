"""Immutable Delivery metadata evidence; authenticated source ACL is external.

The frozen bytes bind exact version/canvas, metadata artifact and approved
projection. Syntax/hash verification never establishes approval authority.
"""
from hashlib import sha256
from typing import Literal
from pydantic import model_validator
from photo_worker.contracts.base import ArtifactRef,Contract,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from .delivery_metadata_projection import project_delivery_metadata


class FrozenDeliveryMetadata(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    metadata_source: ArtifactRef
    policy: Literal['strip','copyright','approved_exif']
    approved_fields: list[str]
    projection: MetadataDocument

    @model_validator(mode='after')
    def identity(self):
        if (self.binding.asset_version_id.int==0 or self.binding.source.artifact_id.int==0
                or self.metadata_source.artifact_id.int==0
                or self.metadata_source.artifact_id==self.binding.source.artifact_id):
            raise ValueError('DELIVERY_METADATA_BINDING')
        # Validate field names/cardinality without trusting the projection itself.
        project_delivery_metadata(MetadataDocument(),self.policy,approved_fields=tuple(self.approved_fields))
        if self.policy!='approved_exif' and self.approved_fields:
            raise ValueError('DELIVERY_METADATA_APPROVAL')
        if self.approved_fields!=sorted(self.approved_fields):
            raise ValueError('DELIVERY_METADATA_APPROVAL_ORDER')
        return self


def freeze_delivery_metadata(binding,metadata_ref,source_raw,policy,*,approved_fields=()):
    if not isinstance(binding,VersionBinding) or not isinstance(metadata_ref,ArtifactRef):
        raise ValueError('DELIVERY_METADATA_BINDING')
    source=_source(metadata_ref,source_raw)
    projection=project_delivery_metadata(source,policy,approved_fields=approved_fields)
    frozen=FrozenDeliveryMetadata(schema_version=1,binding=binding.model_copy(deep=True),
        metadata_source=metadata_ref.model_copy(deep=True),policy=policy,
        approved_fields=sorted(approved_fields),projection=projection)
    return canonical_bytes(frozen)


def _source(ref,raw):
    if (not isinstance(raw,bytes) or not 2<=len(raw)<=1048576
            or sha256(raw).hexdigest()!=ref.sha256):
        raise ValueError('DELIVERY_METADATA_SOURCE_BYTES')
    source=MetadataDocument.model_validate_json(raw)
    if canonical_bytes(source)!=raw:
        raise ValueError('DELIVERY_METADATA_SOURCE_CANONICAL')
    return source


def verify_frozen_delivery_metadata(raw,expected_sha,binding,metadata_ref,source_raw,*,policy,approved_fields=()):
    """Restore against externally authenticated task policy and approval fields.

    A self-consistent receipt cannot choose or widen its own metadata permission.
    Recompilation also enforces the exact canonical source/version/canvas scope.
    """
    if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=expected_sha:
        raise ValueError('DELIVERY_METADATA_FROZEN_BYTES')
    expected=freeze_delivery_metadata(binding,metadata_ref,source_raw,policy,
        approved_fields=approved_fields)
    if raw!=expected:
        raise ValueError('DELIVERY_METADATA_FROZEN_SCOPE')
    return FrozenDeliveryMetadata.model_validate_json(expected)
