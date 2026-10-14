from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4, uuid5

import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import Canvas, MaskRef, TransformChain
from photo_worker.operations.interchange.person_origin_factory import DeclaredPersonOriginFactory
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_export_prepare import ExportSource
from photo_worker.v2.interchange_person_origin import PortablePersonOriginClaim, PortablePersonOriginEnvelope


def test_declared_person_origin_factory_rechecks_signed_physical_inputs():
    def ref(body=b'data'):
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(body).hexdigest())

    model_lock = 'd' * 64
    recipe, override, pixels, original = ref(b'recipe'), ref(b'override'), ref(b'pixels'), ref(b'source')
    canvas = Canvas(width=16, height=16, orientation=1, space_id='source', source_sha256='a' * 64)
    mask = MaskRef(artifact=pixels, canvas=canvas, transform_chain=TransformChain.create([]),
        selector='person', confidence=1, model_lock_id=model_lock, protected=False)
    mask_raw = canonical_bytes(mask)
    mask_ref = ref(mask_raw)
    entry_id = uuid4()
    binding = VersionBinding(asset_version_id=uuid4(), source=original, canvas_hash='b' * 64)
    private = Ed25519PrivateKey.generate()
    public = private.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    key_id = sha256(public).hexdigest()
    issuer_id = uuid4()
    claim = PortablePersonOriginClaim(schema_version=1, issuer_id=issuer_id, entry_id=entry_id,
        source=binding, recipe=recipe, override=override, mask_ref=mask_ref, mask_pixels=pixels,
        producer_task_id=uuid4(), producer_attempt=1, producer_fence=1,
        operation='mask.person', operation_version='1', model_lock_sha=model_lock,
        qualification_report=ref(b'report'))
    signature = private.sign(b'PhotoEditor/V30/person-mask-origin/v1\x00' + canonical_bytes(claim))
    receipt_raw = canonical_bytes(PortablePersonOriginEnvelope(schema_version=1,
        claim=claim, key_id=key_id, signature_hex=signature.hex()))
    receipt_ref = ArtifactRef(artifact_id=uuid5(entry_id,
        f'v30:person-origin:{mask_ref.artifact_id}'), sha256=sha256(receipt_raw).hexdigest())
    docs = {receipt_ref.artifact_id: receipt_raw, mask_ref.artifact_id: mask_raw}

    class Reader:
        entries = {(item.artifact_id, item.sha256): SimpleNamespace(ref=item, contract=kind)
            for item, kind in ((receipt_ref, 'PersonOriginReceipt'), (mask_ref, 'MaskRef'))}

        def read_json(self, item, _maximum):
            return docs[item.artifact_id]

        def evidence(self, item):
            return SimpleNamespace(media_type='application/json', bytes=len(docs[item.artifact_id]))

        def recheck(self):
            return None

    inventory = SimpleNamespace(sources=tuple(ExportSource(item, path, kind, 100, kind != 'MaskPixels')
        for item, path, kind in ((recipe, f'recipes/{recipe.artifact_id}.json', 'EditingRecipeV3'),
            (override, f'dependencies/{override.artifact_id}.json', 'Override'),
            (pixels, f'dependencies/{pixels.artifact_id}.bin', 'MaskPixels'))))
    plan = SimpleNamespace(entries=[SimpleNamespace(entry_id=entry_id, binding=binding, recipe=recipe)])
    factory = DeclaredPersonOriginFactory(trusted_keys={(issuer_id, key_id): public}, model_lock_sha=model_lock)
    ctx = SimpleNamespace(check_active=lambda: None)
    sources, mappings, qualify = factory(ctx, None, None, plan, Reader(), inventory)
    assert len(sources) == 2 and len(mappings) == 1
    assert qualify(mappings[0], sources[0], sources[1]) is True
    docs[receipt_ref.artifact_id] = receipt_raw[:-1] + b'!'
    assert qualify(mappings[0], sources[0], sources[1]) is False
    with pytest.raises(ValueError, match='INSTALLATION'):
        DeclaredPersonOriginFactory(trusted_keys={(issuer_id, '0' * 64): public}, model_lock_sha=model_lock)
