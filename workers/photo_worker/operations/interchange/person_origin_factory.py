"""Process-keyed V30 origin sources from exact declared export inputs."""

from hashlib import sha256
from uuid import UUID, uuid5

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.v2.interchange_export_prepare import ExportSource
from photo_worker.v2.interchange_manifest import BundlePersonOrigin
from photo_worker.v2.interchange_person_origin import (
    PortablePersonOriginEnvelope, PortablePersonOriginExpectation,
    verify_portable_person_origin,
)


class DeclaredPersonOriginFactory:
    def __init__(self, *, trusted_keys, model_lock_sha):
        if (not isinstance(trusted_keys, dict) or not trusted_keys
                or not isinstance(model_lock_sha, str) or len(model_lock_sha) != 64
                or any(ch not in '0123456789abcdef' for ch in model_lock_sha)):
            raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
        owned = {}
        for key, public in trusted_keys.items():
            if (not isinstance(key, tuple) or len(key) != 2 or not isinstance(key[0], UUID)
                    or key[0].int == 0 or not isinstance(key[1], str)
                    or not isinstance(public, bytes) or len(public) != 32
                    or sha256(public).hexdigest() != key[1]):
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_INSTALLATION')
            owned[key] = bytes(public)
        self._trusted = owned
        self._model_lock = model_lock_sha

    def __call__(self, ctx, artifacts, authority, plan, reader, inventory):
        ctx.check_active()
        by_entry = {entry.entry_id: entry for entry in plan.entries}
        if len(by_entry) != len(plan.entries):
            raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_ENTRY')
        qualified = {source.reference.artifact_id: source for source in inventory.sources}
        receipts = [entry for entry in reader.entries.values() if entry.contract == 'PersonOriginReceipt']
        if len(receipts) > 5000:
            raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_LIMIT')
        sources, mappings, pins = [], [], {}
        used_mask_refs = {}
        for declared in receipts:
            ctx.check_active()
            receipt_ref = declared.ref.model_copy(deep=True)
            raw = reader.read_json(receipt_ref, 16384)
            envelope = PortablePersonOriginEnvelope.model_validate_json(raw)
            claim = envelope.claim
            plan_entry = by_entry.get(claim.entry_id)
            path = f'origins/{claim.entry_id}/{claim.mask_ref.artifact_id}.json'
            identity = uuid5(claim.entry_id, f'v30:person-origin:{claim.mask_ref.artifact_id}')
            if (claim.model_lock_sha != self._model_lock or plan_entry is None
                    or plan_entry.binding != claim.source or plan_entry.recipe != claim.recipe
                    or receipt_ref.artifact_id != identity or path in pins
                    or qualified.get(claim.recipe.artifact_id) is None
                    or qualified[claim.recipe.artifact_id].reference != claim.recipe
                    or qualified[claim.recipe.artifact_id].contract != 'EditingRecipeV3'
                    or qualified.get(claim.override.artifact_id) is None
                    or qualified[claim.override.artifact_id].reference != claim.override
                    or qualified[claim.override.artifact_id].contract != 'Override'
                    or qualified.get(claim.mask_pixels.artifact_id) is None
                    or qualified[claim.mask_pixels.artifact_id].reference != claim.mask_pixels
                    or qualified[claim.mask_pixels.artifact_id].contract != 'MaskPixels'):
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_SCOPE')
            expected = PortablePersonOriginExpectation(entry_id=claim.entry_id,
                source=claim.source, recipe=claim.recipe, override=claim.override,
                mask_ref=claim.mask_ref, mask_pixels=claim.mask_pixels,
                model_lock_sha=self._model_lock)
            verify_portable_person_origin(raw, expected=expected, trusted_keys=self._trusted)
            mask_entry = reader.entries.get((claim.mask_ref.artifact_id, claim.mask_ref.sha256))
            if mask_entry is None or mask_entry.contract != 'MaskRef':
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_MASK_REF')
            mask_raw = reader.read_json(claim.mask_ref, 1 << 20)
            mask = MaskRef.model_validate_json(mask_raw)
            if (canonical_bytes(mask) != mask_raw or mask.selector != 'person'
                    or mask.protected or mask.object_id is not None
                    or mask.model_lock_id != self._model_lock
                    or mask.artifact != claim.mask_pixels):
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_MASK_REF')
            receipt_meta = reader.evidence(receipt_ref)
            mask_meta = reader.evidence(claim.mask_ref)
            if (receipt_meta.media_type != 'application/json' or receipt_meta.bytes != len(raw)
                    or mask_meta.media_type != 'application/json' or mask_meta.bytes != len(mask_raw)):
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_EVIDENCE')
            receipt_source = ExportSource(receipt_ref, path, 'PersonOriginReceipt', len(raw), True)
            mask_source = ExportSource(claim.mask_ref.model_copy(deep=True),
                f'dependencies/{claim.mask_ref.artifact_id}.json', 'MaskRef', len(mask_raw), True)
            old = used_mask_refs.setdefault(claim.mask_ref.artifact_id, mask_source)
            if old != mask_source:
                raise ValueError('INTERCHANGE_EXPORT_PERSON_ORIGIN_ALIAS')
            sources.append(receipt_source)
            mapping = BundlePersonOrigin(entry_id=claim.entry_id, recipe=claim.recipe,
                override=claim.override, mask_ref=claim.mask_ref,
                mask_pixels=claim.mask_pixels, path=path, sha256=receipt_ref.sha256)
            mappings.append(mapping)
            pins[path] = (canonical_bytes(mapping), raw, mask_raw, expected)
        sources.extend(used_mask_refs.values())

        def qualify(origin, receipt_source, mask_source):
            ctx.check_active()
            pinned = pins.get(origin.path)
            if (pinned is None or canonical_bytes(origin) != pinned[0]
                    or receipt_source.reference.sha256 != origin.sha256
                    or mask_source.reference != origin.mask_ref):
                return False
            fresh_receipt = reader.read_json(receipt_source.reference, 16384)
            fresh_mask = reader.read_json(mask_source.reference, 1 << 20)
            if fresh_receipt != pinned[1] or fresh_mask != pinned[2]:
                return False
            verify_portable_person_origin(fresh_receipt,
                expected=pinned[3], trusted_keys=self._trusted)
            reader.recheck()
            ctx.check_active()
            return True

        reader.recheck()
        ctx.check_active()
        return tuple(sources), tuple(mappings), qualify
