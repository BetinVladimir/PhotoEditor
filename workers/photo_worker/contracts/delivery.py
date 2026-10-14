"""Worker delivery wire contracts matching implementation-v2 schemas.json."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, field_validator, model_validator, model_serializer
from photo_worker.contracts.base import ArtifactRef, Contract, SHA256
from photo_worker.operations.session.contracts import VersionBinding

class DeliveryPolicy(Contract):
    required_presets: Annotated[list[UUID], Field(min_length=1, max_length=16)]
    optional_presets: Annotated[list[UUID], Field(max_length=16)]
    require_review: bool
    metadata_policy: Literal['strip', 'copyright', 'approved_exif']
    provenance: Literal['off', 'required']
    signing_policy_id: UUID | None = None
    signing_policy_sha: SHA256 | None = None
    allow_omitted: bool
    approved_fields: Annotated[list[Literal['author','copyright','camera_make','camera_model','lens','datetime_original','gps']], Field(max_length=7)] = Field(default_factory=list)

    @field_validator('approved_fields')
    @classmethod
    def closed_unique_approval(cls, fields):
        if len(set(fields)) != len(fields):
            raise ValueError('DELIVERY_METADATA_APPROVAL')
        return sorted(fields)

    @model_validator(mode='after')
    def policy_approval(self):
        if len(self.required_presets) + len(self.optional_presets) > 16 or len(set(self.required_presets + self.optional_presets)) != len(self.required_presets) + len(self.optional_presets):
            raise ValueError('DELIVERY_PRESET_SELECTION')
        if self.metadata_policy != 'approved_exif' and self.approved_fields:
            raise ValueError('DELIVERY_METADATA_APPROVAL')
        if self.provenance == 'off' and (
            'signing_policy_id' in self.model_fields_set or 'signing_policy_sha' in self.model_fields_set
        ):
            raise ValueError('DELIVERY_SIGNING_POLICY_CHOICE')
        if self.provenance == 'required' and (self.signing_policy_id is None or self.signing_policy_id.int == 0 or self.signing_policy_sha is None):
            raise ValueError('DELIVERY_SIGNING_POLICY_CHOICE')
        return self

    @model_serializer(mode='wrap')
    def legacy_empty_approval(self, handler):
        value = handler(self)
        # Match Go omitempty, including nested frozen/assembly policy hashes.
        if not self.approved_fields:
            value.pop('approved_fields', None)
        if self.signing_policy_id is None:
            value.pop('signing_policy_id', None)
        if self.signing_policy_sha is None:
            value.pop('signing_policy_sha', None)
        return value

class FrozenPreset(Contract):
    preset_id: UUID
    preset_sha: SHA256
    config: ArtifactRef
    required: bool

class DeliverySlot(Contract):
    ordinal: Annotated[int, Field(ge=0, le=4999)]
    binding: VersionBinding
    preset_id: UUID
    preset_sha: SHA256
    required: bool
    state: Literal['ready', 'omitted', 'failed']
    export_id: UUID | None
    export: ArtifactRef | None
    reason: Annotated[str, Field(min_length=1, max_length=200)] | None

class DeliverySlotShard(Contract):
    schema_version: Literal[1]
    snapshot_digest: SHA256
    items: Annotated[list[DeliverySlot], Field(min_length=1, max_length=500)]

class DeliveryAssemblyInput(Contract):
    schema_version: Literal[1]
    snapshot_digest: SHA256
    item_count: Annotated[int, Field(ge=1, le=5000)]
    slot_count: Annotated[int, Field(ge=1, le=80000)]
    policy: DeliveryPolicy
    presets: Annotated[list[FrozenPreset], Field(min_length=1, max_length=16)]
    shards: Annotated[list[ArtifactRef], Field(min_length=1, max_length=160)]

class SignedDeliveryAssemblyEnvelope(Contract):
    schema_version: Literal[2]
    assembly: ArtifactRef
    signing_closure: ArtifactRef

    @model_validator(mode='after')
    def disjoint_documents(self):
        if self.assembly.artifact_id.int == 0 or self.signing_closure.artifact_id.int == 0 or self.assembly.artifact_id == self.signing_closure.artifact_id:
            raise ValueError('DELIVERY_SIGNED_ASSEMBLY_ALIAS')
        return self

class SignedDeliveryAssemblyEntry(Contract):
    ordinal: Annotated[int, Field(ge=0, le=4999)]
    preset_id: UUID
    export_id: UUID
    export: ArtifactRef
    signing_key: str
    task_id: UUID
    result_id: UUID
    signed: ArtifactRef
    manifest: ArtifactRef
    validation_sha: SHA256

class SignedDeliveryAssemblyClosure(Contract):
    schema_version: Literal[1]
    tenant_id: UUID
    actor_id: UUID
    parent_id: UUID
    policy_id: UUID
    policy_sha: SHA256
    entries: Annotated[list[SignedDeliveryAssemblyEntry], Field(min_length=1, max_length=80000)]

class SignedDeliveryManifest(Contract):
    schema_version: Literal[2]
    envelope_sha: SHA256
    assembly: ArtifactRef
    signing_closure: ArtifactRef
    snapshot_digest: SHA256
    slot_count: Annotated[int, Field(ge=1, le=80000)]
    ready_count: Annotated[int, Field(ge=1, le=80000)]
    omitted_count: Annotated[int, Field(ge=0, le=80000)]
    shards: Annotated[list[ArtifactRef], Field(min_length=1, max_length=160)]

class DeliveryManifest(Contract):
    schema_version: Literal[1]
    assembly_sha: SHA256
    snapshot_digest: SHA256
    slot_count: Annotated[int, Field(ge=1, le=80000)]
    ready_count: Annotated[int, Field(ge=1, le=80000)]
    omitted_count: Annotated[int, Field(ge=0, le=80000)]
    shards: Annotated[list[ArtifactRef], Field(min_length=1, max_length=160)]

class FrozenDeliveryInput(Contract):
    schema_version: Literal[1]
    snapshot: ArtifactRef
    snapshot_digest: SHA256
    policy: DeliveryPolicy
    presets: Annotated[list[FrozenPreset], Field(min_length=1, max_length=16)]

class DeliveryPreparation(Contract):
    schema_version: Literal[1]
    input: ArtifactRef
    snapshot_digest: SHA256
    item_count: Annotated[int, Field(ge=1, le=5000)]
    slot_count: Annotated[int, Field(ge=1, le=80000)]

class V2DeliveryPrepareParams(Contract):
    schema_version: Literal[1]
    input: ArtifactRef

class V2DeliveryAssembleParams(Contract):
    schema_version: Literal[1]
    assembly: ArtifactRef
