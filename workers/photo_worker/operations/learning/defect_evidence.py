"""Exact V23 reviewed-defect holdout document; no review authority is inferred here."""
from __future__ import annotations

import hashlib
from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.defects import DefectReview


class FrozenDefectHoldoutEvidence(Contract):
    schema_version: Literal[1]
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    review_id: UUID
    run_id: UUID
    actor_id: UUID
    decision: Literal['accept']
    route: Literal['repair']
    decision_hash: SHA256
    revision: Annotated[int, Field(ge=1, le=2**53 - 1)]
    image: ArtifactRef
    protection: ArtifactRef
    params: ArtifactRef
    selection: DefectReview

    @model_validator(mode='after')
    def exact_decision(self):
        if any(value.int == 0 for value in
               (self.tenant_id, self.event_id, self.asset_version_id,
                self.review_id, self.run_id, self.actor_id)):
            raise ValueError('INVALID_DEFECT_HOLDOUT_IDENTITY')
        if (self.revision != self.selection.revision or
                self.image != self.selection.image or
                not self.selection.accepted_candidate_ids):
            raise ValueError('INVALID_DEFECT_HOLDOUT_SELECTION')
        refs = (self.image, self.protection, self.params,
                self.selection.proposal, self.selection.label_map)
        if (any(ref.artifact_id.int == 0 for ref in refs) or
                len({ref.artifact_id for ref in refs}) != len(refs)):
            raise ValueError('ALIASED_DEFECT_HOLDOUT_INPUT')
        decision = dict(version='defect_decision_v1', tenant_id=str(self.tenant_id),
                        review_id=str(self.review_id), run_id=str(self.run_id),
                        actor_id=str(self.actor_id), decision='accept',
                        selection=self.selection.model_dump(mode='json'))
        if canonical_hash(decision) != self.decision_hash:
            raise ValueError('DEFECT_HOLDOUT_DECISION_HASH_MISMATCH')
        return self


class DefectHoldoutExpected(Contract):
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    review_id: UUID
    run_id: UUID
    actor_id: UUID
    decision_hash: SHA256
    image: ArtifactRef
    protection: ArtifactRef
    params: ArtifactRef


def decode_frozen_defect_holdout_evidence(raw: bytes, artifact: ArtifactRef,
                                          expected: DefectHoldoutExpected) -> FrozenDefectHoldoutEvidence:
    """Verify exact registered bytes and SQL-supplied expected identity before use."""
    if (not isinstance(raw, bytes) or not 2 <= len(raw) <= 1_048_576 or
            artifact.artifact_id.int == 0 or
            hashlib.sha256(raw).hexdigest() != artifact.sha256):
        raise ValueError('INVALID_FROZEN_DEFECT_HOLDOUT')
    parse_json(raw)
    frozen = FrozenDefectHoldoutEvidence.model_validate_json(raw)
    if canonical_bytes(frozen) != raw or any((
            frozen.tenant_id != expected.tenant_id,
            frozen.event_id != expected.event_id,
            frozen.asset_version_id != expected.asset_version_id,
            frozen.review_id != expected.review_id,
            frozen.run_id != expected.run_id,
            frozen.actor_id != expected.actor_id,
            frozen.decision_hash != expected.decision_hash,
            frozen.image != expected.image,
            frozen.protection != expected.protection,
            frozen.params != expected.params)):
        raise ValueError('STALE_FROZEN_DEFECT_HOLDOUT')
    return frozen


def validate_holdout_defect_binding(case, event_id: UUID,
                                    evidence: FrozenDefectHoldoutEvidence) -> None:
    """Bind reviewed evidence to the exact frozen render case before replay."""
    from .contracts import RenderCase
    if (not isinstance(case, RenderCase) or
            not isinstance(evidence, FrozenDefectHoldoutEvidence) or
            case.defect_evidence is None or
            event_id.int == 0 or evidence.event_id != event_id or
            evidence.asset_version_id != case.binding.asset_version_id or
            evidence.image != case.working_image or
            evidence.protection != case.protected or
            evidence.selection.image != case.working_image):
        raise ValueError('HOLDOUT_DEFECT_BINDING_MISMATCH')
