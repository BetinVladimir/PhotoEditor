"""Frozen delivery preparation; never reads mutable presets or starts export jobs."""
import hashlib
from collections.abc import Callable
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.delivery import DeliveryPreparation, FrozenDeliveryInput
from photo_worker.operations.session.contracts import InputSnapshot


def prepare_delivery(input_ref: ArtifactRef, input_raw: bytes,
                     read_object: Callable[[ArtifactRef], bytes],
                     validate_preset: Callable[[bytes], None],
                     cancelled: Callable[[], bool] = lambda: False) -> DeliveryPreparation:
    """Caller supplies scoped immutable reads and the existing export-preset validator.

    The validator must raise for unsupported config, not silently normalize it.
    No readiness/publication/qualification claim is made by this pure preflight.
    """
    def active():
        if cancelled():
            raise ValueError('DELIVERY_CANCELLED')

    def verified(ref, raw, model=None, max_bytes=1_048_576):
        active()
        if ref.artifact_id.int == 0 or not isinstance(raw, bytes) or not 2 <= len(raw) <= max_bytes or hashlib.sha256(raw).hexdigest() != ref.sha256:
            raise ValueError('DELIVERY_CONTENT_MISMATCH')
        value = parse_json(raw, max_bytes=max_bytes)
        if canonical_bytes(value) != raw:
            raise ValueError('DELIVERY_NONCANONICAL')
        active()
        parsed = model.model_validate_json(raw) if model else value
        if model is FrozenDeliveryInput and value['policy'].get('approved_fields', []) != parsed.policy.approved_fields:
            raise ValueError('DELIVERY_NONCANONICAL')
        return parsed

    input_ref = input_ref.model_copy(deep=True)
    active()
    frozen = verified(input_ref, input_raw, FrozenDeliveryInput)
    required, optional = set(frozen.policy.required_presets), set(frozen.policy.optional_presets)
    presets = {p.preset_id: p for p in frozen.presets}
    if (len(required) != len(frozen.policy.required_presets) or
            len(optional) != len(frozen.policy.optional_presets) or required & optional or
            len(presets) != len(frozen.presets) or set(presets) != required | optional):
        raise ValueError('DELIVERY_MATRIX_INVALID')
    if frozen.snapshot.artifact_id.int == 0:
        raise ValueError('DELIVERY_SNAPSHOT_INVALID')
    for preset in frozen.presets:
        active()
        if (preset.preset_id.int == 0 or preset.config.artifact_id.int == 0
                or preset.required != (preset.preset_id in required)
                or preset.preset_sha != preset.config.sha256):
            raise ValueError('DELIVERY_PRESET_INVALID')
    snapshot = verified(frozen.snapshot, read_object(frozen.snapshot.model_copy(deep=True)), InputSnapshot, 8_388_608)
    if snapshot.snapshot_id.int == 0 or snapshot.snapshot_digest != frozen.snapshot_digest or canonical_hash(snapshot.model_dump(mode='json', exclude={'snapshot_digest'})) != snapshot.snapshot_digest:
        raise ValueError('DELIVERY_SNAPSHOT_INVALID')
    seen_versions = set()
    for ordinal, item in enumerate(snapshot.items):
        active()
        if (item.ordinal != ordinal or item.working_image != item.binding.source
                or item.binding.asset_version_id in seen_versions
                or any(ref.artifact_id.int == 0 for ref in (item.binding.source, item.working_image, item.recipe))
                or item.binding.asset_version_id.int == 0):
            raise ValueError('DELIVERY_SNAPSHOT_INVALID')
        seen_versions.add(item.binding.asset_version_id)
    for preset in frozen.presets:
        active()
        raw = read_object(preset.config.model_copy(deep=True))
        config = verified(preset.config, raw)
        if config.get('metadata_policy') != frozen.policy.metadata_policy:
            raise ValueError('DELIVERY_METADATA_POLICY_MISMATCH')
        validate_preset(raw)
        active()
    return DeliveryPreparation(schema_version=1, input=input_ref.model_copy(deep=True),
                               snapshot_digest=snapshot.snapshot_digest,
                               item_count=len(snapshot.items),
                               slot_count=len(snapshot.items) * len(frozen.presets))
