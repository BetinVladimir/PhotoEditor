"""V28 glare and reflection intents; these types do not install an executor."""
from typing import Literal

from pydantic import model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, Unit
from photo_worker.operations.session.contracts import VersionBinding


class PortraitGlareParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    donor: VersionBinding | None
    glasses_mask: ArtifactRef
    protected: ArtifactRef
    mode: Literal['donor', 'generative']
    confirmation: ArtifactRef
    model_lock_sha: SHA256
    calibration: ArtifactRef
    source_faces: ArtifactRef
    donor_faces: ArtifactRef | None

    @model_validator(mode='after')
    def bound_intent(self):
        if self.base.asset_version_id.int == 0 or self.base.source.artifact_id.int == 0:
            raise ValueError('Glare source binding required')
        if (self.mode == 'donor') != (self.donor is not None and self.donor_faces is not None):
            raise ValueError('Glare mode and donor evidence disagree')
        if self.donor is not None and (
                self.donor.asset_version_id.int == 0 or self.donor.source.artifact_id.int == 0
                or self.donor.asset_version_id == self.base.asset_version_id
                or self.donor.source.artifact_id == self.base.source.artifact_id):
            raise ValueError('Distinct donor frame required')
        refs = [self.glasses_mask, self.protected, self.confirmation,
                self.calibration, self.source_faces]
        if self.donor_faces is not None:
            refs.append(self.donor_faces)
        ids = [ref.artifact_id for ref in refs]
        if any(value.int == 0 for value in ids) or len(set(ids)) != len(ids):
            raise ValueError('Distinct nonzero glare evidence required')
        return self


class ImageReflectionParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    mask: ArtifactRef
    protected: ArtifactRef
    model_lock_sha: SHA256
    calibration: ArtifactRef

    @model_validator(mode='after')
    def bound_intent(self):
        if self.base.asset_version_id.int == 0 or self.base.source.artifact_id.int == 0:
            raise ValueError('Reflection source binding required')
        ids = [self.mask.artifact_id, self.protected.artifact_id,
               self.calibration.artifact_id]
        if any(value.int == 0 for value in ids) or len(set(ids)) != len(ids):
            raise ValueError('Distinct nonzero reflection evidence required')
        return self
