"""Explicit donor eye replacement intent; no identity/consent grant implied."""
from typing import Literal
from uuid import UUID
from pydantic import model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, Unit
from photo_worker.operations.session.contracts import VersionBinding


class PortraitOpenEyesParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    donor: VersionBinding
    source_face_id: UUID
    donor_face_id: UUID
    eyes_mask: ArtifactRef
    protected: ArtifactRef
    confirmation: ArtifactRef
    source_faces: ArtifactRef
    donor_faces: ArtifactRef

    @model_validator(mode="after")
    def explicit_donor(self):
        identities = [self.base.asset_version_id, self.donor.asset_version_id,
                      self.source_face_id, self.donor_face_id]
        refs = [self.base.source, self.donor.source, self.eyes_mask,
                self.protected, self.confirmation, self.source_faces, self.donor_faces]
        if any(value.int == 0 for value in identities) or any(ref.artifact_id.int == 0 for ref in refs):
            raise ValueError("Nonzero identity and artifact pins required")
        if (self.base.asset_version_id == self.donor.asset_version_id or
                self.base.source.artifact_id == self.donor.source.artifact_id):
            raise ValueError("Distinct donor frame required")
        if self.eyes_mask.artifact_id == self.protected.artifact_id:
            raise ValueError("Eyes and protected mask must be distinct")
        return self
