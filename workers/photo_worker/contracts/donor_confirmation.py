"""Server-authored donor proof document; live consent authority is separate."""
import hashlib
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import Contract, canonical_bytes, parse_json
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams


class DonorConfirmation(Contract):
    schema_version: Literal[1]
    person_id: UUID
    source: VersionBinding
    donor: VersionBinding | None
    source_face_id: UUID
    donor_face_id: UUID | None
    source_observation_id: UUID
    donor_observation_id: UUID | None
    consent_id: UUID
    epoch: Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]
    actor_id: UUID

    @model_validator(mode='after')
    def consistent_donor(self):
        required = [self.person_id, self.source_face_id, self.source_observation_id,
                    self.consent_id, self.actor_id, self.source.asset_version_id,
                    self.source.source.artifact_id]
        if any(value.int == 0 for value in required):
            raise ValueError('Nonzero confirmation pins required')
        present = [self.donor is not None, self.donor_face_id is not None,
                   self.donor_observation_id is not None]
        if any(present) and not all(present):
            raise ValueError('Donor evidence must be complete or absent')
        if self.donor is not None:
            if any(value.int == 0 for value in [self.donor_face_id, self.donor_observation_id,
                                               self.donor.asset_version_id, self.donor.source.artifact_id]):
                raise ValueError('Nonzero donor pins required')
            if self.donor.asset_version_id == self.source.asset_version_id:
                raise ValueError('Distinct donor version required')
        return self


def verify_open_eyes_confirmation(params: PortraitOpenEyesParams, raw: bytes) -> DonorConfirmation:
    """Verify immutable bytes and request binding, never restore live consent."""
    parse_json(raw, max_bytes=65_536)
    proof = DonorConfirmation.model_validate_json(raw)
    if canonical_bytes(proof) != raw or hashlib.sha256(raw).hexdigest() != params.confirmation.sha256:
        raise ValueError('DONOR_CONFIRMATION_BYTES')
    if (proof.source != params.base or proof.donor != params.donor or
            proof.source_face_id != params.source_face_id or proof.donor_face_id != params.donor_face_id):
        raise ValueError('DONOR_CONFIRMATION_BINDING')
    return proof
