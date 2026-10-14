from hashlib import sha256
from uuid import uuid4

import pytest
from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_person_origin import (
    PortablePersonOriginClaim, PortablePersonOriginEnvelope,
    PortablePersonOriginExpectation, verify_portable_person_origin,
)


def fixture():
    def ref():
        return ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    source = VersionBinding(asset_version_id=uuid4(), source=ref(), canvas_hash='b' * 64)
    claim = PortablePersonOriginClaim(schema_version=1, issuer_id=uuid4(), source=source,
        recipe=ref(), override=ref(), mask_ref=ref(), mask_pixels=ref(),
        producer_task_id=uuid4(), producer_attempt=2, producer_fence=3,
        operation='mask.person', operation_version='1', model_lock_sha='c' * 64,
        qualification_report=ref())
    expected = PortablePersonOriginExpectation(source=source, recipe=claim.recipe,
        override=claim.override, mask_ref=claim.mask_ref, mask_pixels=claim.mask_pixels,
        model_lock_sha=claim.model_lock_sha)
    private = Ed25519PrivateKey.generate()
    public = private.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    key_id = sha256(public).hexdigest()
    signature = private.sign(b'PhotoEditor/V30/person-mask-origin/v1\x00' + canonical_bytes(claim))
    envelope = PortablePersonOriginEnvelope(schema_version=1, claim=claim,
        key_id=key_id, signature_hex=signature.hex())
    return canonical_bytes(envelope), expected, {(claim.issuer_id, key_id): public}


def test_portable_person_origin_requires_exact_trusted_signature_and_scope():
    raw, expected, keys = fixture()
    accepted = verify_portable_person_origin(raw, expected=expected, trusted_keys=keys)
    assert accepted.mask_pixels == expected.mask_pixels
    accepted.mask_pixels.sha256 = 'f' * 64
    assert verify_portable_person_origin(raw, expected=expected, trusted_keys=keys).mask_pixels == expected.mask_pixels


@pytest.mark.parametrize('change', ['signature', 'issuer', 'key', 'scope', 'canonical',
                                    'operation', 'attempt'])
def test_portable_person_origin_refuses_substitution(change):
    raw, expected, keys = fixture()
    import json
    value = json.loads(raw)
    if change == 'signature':
        value['signature_hex'] = '0' * 128
    elif change == 'issuer':
        value['claim']['issuer_id'] = str(uuid4())
    elif change == 'key':
        keys = {}
    elif change == 'scope':
        expected.mask_pixels = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
    elif change == 'operation':
        value['claim']['operation'] = 'mask.prompt'
    elif change == 'attempt':
        value['claim']['producer_attempt'] = 0
    if change == 'canonical':
        raw += b' '
    elif change != 'scope' and change != 'key':
        raw = canonical_bytes(value)
    with pytest.raises((ValueError, InvalidSignature)):
        verify_portable_person_origin(raw, expected=expected, trusted_keys=keys)
