"""Portable V30 person-mask producer receipt; trust is supplied by the process.

The signature proves only that a configured issuer made the exact assertion.
The export issuer must first qualify the original mask.person producer, and the
importer must separately require a current target-version user acceptance.
"""
from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, parse_json
from photo_worker.operations.session.contracts import VersionBinding


_DOMAIN = b'PhotoEditor/V30/person-mask-origin/v1\x00'


class PortablePersonOriginClaim(Contract):
    schema_version: Literal[1]
    issuer_id: UUID
    source: VersionBinding
    recipe: ArtifactRef
    override: ArtifactRef
    mask_ref: ArtifactRef
    mask_pixels: ArtifactRef
    producer_task_id: UUID
    producer_attempt: Annotated[int, Field(strict=True, ge=1, le=9007199254740991)]
    producer_fence: Annotated[int, Field(strict=True, ge=1, le=9007199254740991)]
    operation: Literal['mask.person']
    operation_version: Literal['1']
    model_lock_sha: SHA256
    qualification_report: ArtifactRef

    @model_validator(mode='after')
    def identities(self):
        identifiers = (self.issuer_id, self.source.asset_version_id,
            self.source.source.artifact_id, self.recipe.artifact_id,
            self.override.artifact_id, self.mask_ref.artifact_id,
            self.mask_pixels.artifact_id, self.producer_task_id,
            self.qualification_report.artifact_id)
        if any(identifier.int == 0 for identifier in identifiers):
            raise ValueError('INTERCHANGE_PERSON_ORIGIN_ZERO_ID')
        return self


class PortablePersonOriginEnvelope(Contract):
    schema_version: Literal[1]
    claim: PortablePersonOriginClaim
    key_id: SHA256
    signature_hex: Annotated[str, Field(pattern=r'^[0-9a-f]{128}$')]


class PortablePersonOriginExpectation(Contract):
    source: VersionBinding
    recipe: ArtifactRef
    override: ArtifactRef
    mask_ref: ArtifactRef
    mask_pixels: ArtifactRef
    model_lock_sha: SHA256


def verify_portable_person_origin(raw, *, expected, trusted_keys):
    """Verify exact canonical bytes, scoped refs and an installed Ed25519 key.

    ``trusted_keys`` is process-owned {(issuer UUID, key SHA): raw public key}.
    Neither archive inventory nor an HTTP request may populate it. This does
    not confer consent or make a source producer true without issuer admission.
    """
    if not isinstance(raw, bytes) or not 2 <= len(raw) <= 16384:
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_SIZE')
    value = parse_json(raw, max_bytes=16384)
    if canonical_bytes(value) != raw:
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_CANONICAL')
    envelope = PortablePersonOriginEnvelope.model_validate_json(raw)
    if canonical_bytes(envelope) != raw:
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_FIELDS')
    expected = PortablePersonOriginExpectation.model_validate_json(canonical_bytes(expected))
    claim = envelope.claim
    if (claim.source != expected.source or claim.recipe != expected.recipe
            or claim.override != expected.override or claim.mask_ref != expected.mask_ref
            or claim.mask_pixels != expected.mask_pixels
            or claim.model_lock_sha != expected.model_lock_sha):
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_SCOPE')
    if not isinstance(trusted_keys, dict):
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_TRUST')
    public_bytes = trusted_keys.get((claim.issuer_id, envelope.key_id))
    if (not isinstance(public_bytes, bytes) or len(public_bytes) != 32
            or sha256(public_bytes).hexdigest() != envelope.key_id):
        raise ValueError('INTERCHANGE_PERSON_ORIGIN_TRUST')
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    public = Ed25519PublicKey.from_public_bytes(public_bytes)
    public.verify(bytes.fromhex(envelope.signature_hex), _DOMAIN + canonical_bytes(claim))
    return claim.model_copy(deep=True)
