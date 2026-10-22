import base64
import json
from uuid import uuid4, uuid5
import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_historical_origin import (
    HistoricalOriginReceipt, SignedHistoricalOriginAuthority,
    verify_historical_origin_receipt)


def test_unsigned_or_unknown_issuer_never_authorizes_history():
    key = Ed25519PrivateKey.generate()
    issuer = uuid4()
    body = b'{}'
    receipt = HistoricalOriginReceipt(schema='historical-origin-receipt/v1', issuer_id=issuer,
        key_id='local', payload=base64.b64encode(body).decode(),
        signature=base64.b64encode(key.sign(b'photoeditor:historical-origin:v1\x00'+body)).decode())
    with pytest.raises(AdoptionInvalid):
        verify_historical_origin_receipt(canonical_bytes(receipt), {})
    # Even a cryptographically valid envelope must have a fully typed scope.
    public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    with pytest.raises(AdoptionInvalid):
        verify_historical_origin_receipt(canonical_bytes(receipt), {(issuer, 'local'): public})


def test_trust_installation_copies_operator_keys_and_rejects_empty():
    with pytest.raises(ValueError):
        SignedHistoricalOriginAuthority({})
    key = (uuid4(), 'operator')
    keys = {key: b'x'*32}
    authority = SignedHistoricalOriginAuthority(keys)
    keys.clear()
    assert authority.trusted[key] == b'x'*32


def test_signature_binds_workflow_target_and_original_prepared_separately():
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.interchange_historical_origin import HistoricalOriginPayload
    from hashlib import sha256
    key = Ed25519PrivateKey.generate()
    issuer = uuid4()
    public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    ref = lambda: ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    original = ref()
    payload = HistoricalOriginPayload(schema='historical-origin-payload/v1',
        tenant_id=uuid4(), project_id=uuid4(), workflow_id=uuid4(), run_id=uuid4(),
        entry_id=uuid4(), target=VersionBinding(asset_version_id=uuid4(), source=ref(), canvas_hash='b'*64),
        original_source=ref(), contract='WorkingImage', original=original,
        prepared=ref(), original_image=original, original_boxes=None,
        model_lock_id='', backend='', producer_mode='local_canonical')
    body = canonical_bytes(payload)
    receipt = HistoricalOriginReceipt(schema='historical-origin-receipt/v1', issuer_id=issuer,
        key_id='local', payload=base64.b64encode(body).decode(),
        signature=base64.b64encode(key.sign(b'photoeditor:historical-origin:v1\x00'+body)).decode())
    trusted = {(issuer, 'local'): public}
    assert verify_historical_origin_receipt(canonical_bytes(receipt), trusted) == payload
    changed = payload.model_copy(update={'workflow_id': uuid4()})
    edited = receipt.model_copy(update={'payload': base64.b64encode(canonical_bytes(changed)).decode()})
    with pytest.raises(AdoptionInvalid):
        verify_historical_origin_receipt(canonical_bytes(edited), trusted)
    assert payload.original != payload.prepared


def test_receipt_binding_rejects_other_run_before_installing_ancestry():
    from types import SimpleNamespace
    from hashlib import sha256
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.operations.session.contracts import VersionBinding
    from photo_worker.v2.interchange_historical_origin import HistoricalOriginPayload
    key = Ed25519PrivateKey.generate()
    issuer = uuid4()
    public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    ref = lambda: ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    original = ref()
    payload = HistoricalOriginPayload(schema='historical-origin-payload/v1',
        tenant_id=uuid4(), project_id=uuid4(), workflow_id=uuid4(), run_id=uuid4(),
        entry_id=uuid4(), target=VersionBinding(asset_version_id=uuid4(), source=ref(), canvas_hash='b'*64),
        original_source=ref(), contract='WorkingImage', original=original,
        prepared=ref(), original_image=original, original_boxes=None,
        model_lock_id='', backend='', producer_mode='local_canonical')
    body = canonical_bytes(payload)
    receipt = HistoricalOriginReceipt(schema='historical-origin-receipt/v1', issuer_id=issuer,
        key_id='local', payload=base64.b64encode(body).decode(),
        signature=base64.b64encode(key.sign(b'photoeditor:historical-origin:v1\x00'+body)).decode())
    raw = canonical_bytes(receipt)
    receipt_ref = ArtifactRef(artifact_id=uuid5(payload.workflow_id, 'interchange:historical-origin:'
        +str(payload.entry_id)+':'+payload.contract+':'+str(payload.original.artifact_id)), sha256=sha256(raw).hexdigest())
    authority = SignedHistoricalOriginAuthority({(issuer, 'local'): public})
    plan = SimpleNamespace(entries=[SimpleNamespace(entry_id=payload.entry_id, binding=payload.target)])
    with pytest.raises(AdoptionInvalid):
        authority.bind_inputs(entries=[(receipt_ref, 'HistoricalOriginReceipt', 'application/json'),
            (payload.prepared, 'WorkingImage', 'application/json')], read=lambda *_: raw,
            tenant_id=payload.tenant_id, project_id=payload.project_id,
            workflow_id=payload.workflow_id, run_id=uuid4(), plan=plan,
            check_active=lambda: None, recheck_inputs=lambda: None)


def test_historical_schema_alias_preserves_existing_canonical_envelope_bytes():
    raw = canonical_bytes({
        'schema': 'historical-origin-receipt/v1',
        'issuer_id': '00000000-0000-0000-0000-000000000001',
        'key_id': 'local', 'payload': 'e30=', 'signature': 'AA==',
    })
    receipt = HistoricalOriginReceipt.model_validate_json(raw)
    assert canonical_bytes(receipt) == raw
    assert receipt.schema_tag == 'historical-origin-receipt/v1'
    schema = HistoricalOriginReceipt.model_json_schema()
    assert 'schema' in schema['properties']
    assert 'schema_tag' not in schema['properties']
    renamed = json.loads(raw)
    renamed['schema_tag'] = renamed.pop('schema')
    with pytest.raises(ValueError):
        HistoricalOriginReceipt.model_validate_json(json.dumps(renamed))
