"""Validate the exact frozen Cartesian matrix without accumulating export slots."""
import hashlib
from collections.abc import Callable
from uuid import UUID, uuid5
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.contracts.delivery import (DeliveryAssemblyInput, DeliveryManifest,
    DeliverySlotShard, SignedDeliveryAssemblyEnvelope, SignedDeliveryAssemblyClosure,
    SignedDeliveryManifest)


def assemble_delivery(assembly_ref: ArtifactRef, assembly_raw: bytes,
                      read_shard: Callable[[ArtifactRef], bytes],
                      cancelled: Callable[[], bool] = lambda: False) -> DeliveryManifest:
    return _assemble_delivery(assembly_ref, assembly_raw, read_shard, cancelled, None)


def _assemble_delivery(assembly_ref, assembly_raw, read_shard, cancelled, signed_entries):
    """Readers must enforce scoped immutable closure access; this verifies bytes/semantics.

    Provenance-required is intentionally unavailable until a qualified V31 verifier
    is supplied. Metadata/export readiness are authoritative adoption prerequisites,
    not something an assembly JSON can prove by itself.
    """
    def active():
        if cancelled():
            raise ValueError('DELIVERY_CANCELLED')

    def fail():
        raise ValueError('DELIVERY_MATRIX_INVALID')

    def verified(ref, raw, model):
        active()
        if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 1_048_576
                or hashlib.sha256(raw).hexdigest() != ref.sha256):
            raise ValueError('DELIVERY_CONTENT_MISMATCH')
        value = parse_json(raw)
        if canonical_bytes(value) != raw:
            raise ValueError('DELIVERY_NONCANONICAL')
        active()
        parsed = model.model_validate_json(raw)
        if model is DeliveryAssemblyInput:
            # A frozen consent list must already be in the order emitted by
            # the compiler; model normalization must not reinterpret its pin.
            if value['policy'].get('approved_fields', []) != parsed.policy.approved_fields:
                raise ValueError('DELIVERY_NONCANONICAL')
        return parsed

    assembly_ref = assembly_ref.model_copy(deep=True)
    active()
    if assembly_ref.artifact_id.int == 0:
        fail()
    assembly = verified(assembly_ref, assembly_raw, DeliveryAssemblyInput)
    policy = assembly.policy
    if policy.provenance == 'required' and signed_entries is None:
        raise ValueError('DELIVERY_PROVENANCE_UNAVAILABLE')
    if policy.provenance == 'off' and signed_entries is not None:
        fail()
    required, optional = set(policy.required_presets), set(policy.optional_presets)
    if (len(required) != len(policy.required_presets) or
            len(optional) != len(policy.optional_presets) or required & optional):
        fail()
    presets = {p.preset_id: p for p in assembly.presets}
    if len(presets) != len(assembly.presets) or set(presets) != required | optional:
        fail()
    if any(p.required != (p.preset_id in required) or p.preset_id.int == 0 or
           p.config.artifact_id.int == 0 or p.config.sha256 != p.preset_sha for p in assembly.presets):
        fail()
    if assembly.slot_count != assembly.item_count * len(presets):
        fail()
    if len(assembly.shards) != (assembly.slot_count + 499) // 500:
        fail()
    ids = [r.artifact_id for r in assembly.shards]
    if len(set(ids)) != len(ids) or any(i.int == 0 for i in ids) or assembly_ref.artifact_id in ids:
        fail()
    order = sorted(presets, key=str)
    ready = omitted = count = 0
    binding = None
    seen_versions = set()
    seen_exports = set()
    signed_cursor = 0
    for index, ref in enumerate(assembly.shards):
        active()
        shard = verified(ref.model_copy(deep=True), read_shard(ref.model_copy(deep=True)), DeliverySlotShard)
        expected_length = min(500, assembly.slot_count - index * 500)
        if shard.snapshot_digest != assembly.snapshot_digest or len(shard.items) != expected_length:
            fail()
        for slot in shard.items:
            active()
            ordinal, preset_index = divmod(count, len(order))
            if slot.ordinal != ordinal or slot.preset_id != order[preset_index]:
                fail()
            preset = presets[slot.preset_id]
            if slot.preset_sha != preset.preset_sha or slot.required != preset.required:
                fail()
            if preset_index == 0:
                binding = slot.binding
                if (binding.asset_version_id.int == 0 or binding.source.artifact_id.int == 0
                        or binding.asset_version_id in seen_versions):
                    fail()
                seen_versions.add(binding.asset_version_id)
            elif slot.binding != binding:
                fail()
            if slot.state == 'ready':
                if slot.export_id is None or slot.export_id.int == 0 or slot.export is None or slot.export.artifact_id.int == 0 or slot.reason is not None:
                    fail()
                if slot.export_id in seen_exports:
                    fail()
                seen_exports.add(slot.export_id)
                if signed_entries is not None:
                    if signed_cursor >= len(signed_entries):
                        fail()
                    entry = signed_entries[signed_cursor]
                    if (entry.ordinal != slot.ordinal or entry.preset_id != slot.preset_id
                            or entry.export_id != slot.export_id or entry.export != slot.export):
                        fail()
                    signed_cursor += 1
                ready += 1
            elif slot.state == 'omitted':
                if slot.required or not policy.allow_omitted or not slot.reason or not slot.reason.strip() or slot.export_id is not None or slot.export is not None:
                    fail()
                omitted += 1
            else:
                fail()
            count += 1
    active()
    if count != assembly.slot_count or ready < assembly.item_count * len(required) or (signed_entries is not None and signed_cursor != len(signed_entries)):
        fail()
    return DeliveryManifest(schema_version=1, assembly_sha=assembly_ref.sha256,
                            snapshot_digest=assembly.snapshot_digest, slot_count=count,
                            ready_count=ready, omitted_count=omitted,
                            shards=[ref.model_copy(deep=True) for ref in assembly.shards])


def assemble_signed_delivery(envelope_ref: ArtifactRef, envelope_raw: bytes,
                             read_document: Callable[[ArtifactRef], bytes],
                             tenant: UUID, actor: UUID, parent: UUID,
                             cancelled: Callable[[], bool] = lambda: False) -> SignedDeliveryManifest:
    """Reconstruct signed matrix semantics; caller must supply scoped, immutable bytes.

    SQL admission and V31 physical/operator verification are separate barriers.
    """
    def read(ref, maximum):
        if cancelled():
            raise ValueError('DELIVERY_CANCELLED')
        raw = read_document(ref.model_copy(deep=True))
        if (not isinstance(raw, bytes) or not 2 <= len(raw) <= maximum
                or hashlib.sha256(raw).hexdigest() != ref.sha256
                or canonical_bytes(parse_json(raw)) != raw):
            raise ValueError('DELIVERY_CONTENT_MISMATCH')
        return bytes(raw)

    if (envelope_ref.artifact_id.int == 0 or not isinstance(envelope_raw, bytes)
            or not 2 <= len(envelope_raw) <= 65536
            or hashlib.sha256(envelope_raw).hexdigest() != envelope_ref.sha256
            or canonical_bytes(parse_json(envelope_raw)) != envelope_raw):
        raise ValueError('DELIVERY_SIGNED_ENVELOPE_INVALID')
    envelope = SignedDeliveryAssemblyEnvelope.model_validate_json(envelope_raw)
    if envelope_ref.artifact_id in (envelope.assembly.artifact_id, envelope.signing_closure.artifact_id):
        raise ValueError('DELIVERY_SIGNED_ENVELOPE_ALIAS')
    assembly_raw = read(envelope.assembly, 1048576)
    closure_raw = read(envelope.signing_closure, 8388608)
    assembly = DeliveryAssemblyInput.model_validate_json(assembly_raw)
    closure = SignedDeliveryAssemblyClosure.model_validate_json(closure_raw)
    if (assembly.policy.provenance != 'required' or closure.tenant_id != tenant
            or closure.actor_id != actor or closure.parent_id != parent
            or closure.policy_id != assembly.policy.signing_policy_id
            or closure.policy_sha != assembly.policy.signing_policy_sha):
        raise ValueError('DELIVERY_SIGNED_SCOPE_INVALID')
    seen = set()
    previous = None
    for entry in closure.entries:
        key = (entry.ordinal, str(entry.preset_id))
        expected_key = str(uuid5(parent, f'{entry.ordinal}:{entry.preset_id}'))
        workflow = uuid5(tenant, f'provenance:{actor}:{expected_key}')
        if (previous is not None and key <= previous or entry.signing_key != expected_key
                or entry.task_id != uuid5(workflow, 'provenance.sign:0')
                or entry.result_id.int == 0 or entry.export_id.int == 0
                or entry.signed.artifact_id.int == 0 or entry.manifest.artifact_id.int == 0
                or len({entry.export.artifact_id, entry.signed.artifact_id, entry.manifest.artifact_id}) != 3
                or entry.task_id in seen or entry.result_id in seen
                or entry.export.artifact_id in seen or entry.signed.artifact_id in seen
                or entry.manifest.artifact_id in seen):
            raise ValueError('DELIVERY_SIGNED_CLOSURE_INVALID')
        seen.update((entry.task_id, entry.result_id, entry.export.artifact_id,
                     entry.signed.artifact_id, entry.manifest.artifact_id))
        previous = key
    ordinary = _assemble_delivery(envelope.assembly, assembly_raw,
                                  lambda ref: read(ref, 1048576), cancelled, closure.entries)
    input_ids = [envelope_ref.artifact_id, envelope.assembly.artifact_id,
                 envelope.signing_closure.artifact_id]
    input_ids.extend(ref.artifact_id for ref in ordinary.shards)
    for entry in closure.entries:
        input_ids.extend((entry.export.artifact_id, entry.signed.artifact_id,
                          entry.manifest.artifact_id))
    if len(set(input_ids)) != len(input_ids):
        raise ValueError('DELIVERY_SIGNED_ENVELOPE_ALIAS')
    return SignedDeliveryManifest(schema_version=2, envelope_sha=envelope_ref.sha256,
        assembly=envelope.assembly.model_copy(deep=True),
        signing_closure=envelope.signing_closure.model_copy(deep=True),
        snapshot_digest=ordinary.snapshot_digest, slot_count=ordinary.slot_count,
        ready_count=ordinary.ready_count, omitted_count=ordinary.omitted_count,
        shards=[ref.model_copy(deep=True) for ref in ordinary.shards])
