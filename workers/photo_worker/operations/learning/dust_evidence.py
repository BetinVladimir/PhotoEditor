"""Frozen reviewed sensor-dust evidence for the V23 holdout replay path.

The document binds a decided review and its immutable inputs. SQL must supply
the expected identity and current review authority; this model grants none.
"""
from __future__ import annotations

from hashlib import sha256
from typing import Literal
from uuid import UUID

from pydantic import model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.dust import (DustCandidates, DustRepairParams,
    DustReview, accepted_dust_candidates)


class FrozenDustHoldoutEvidence(Contract):
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
    image: ArtifactRef
    stage_image: ArtifactRef
    protection: ArtifactRef
    proposal: ArtifactRef
    params: ArtifactRef
    selection: DustReview

    @model_validator(mode='after')
    def exact_decision(self):
        if any(value.int == 0 for value in (self.tenant_id, self.event_id,
                self.asset_version_id, self.review_id, self.run_id, self.actor_id)):
            raise ValueError('INVALID_DUST_HOLDOUT_IDENTITY')
        if (self.selection.proposal != self.proposal or
                not self.selection.accepted_candidate_ids or
                len({self.image.artifact_id, self.stage_image.artifact_id,
                     self.protection.artifact_id, self.proposal.artifact_id,
                     self.params.artifact_id}) != 5 or
                self.selection.base.artifact_id in {
                    self.protection.artifact_id, self.proposal.artifact_id,
                    self.params.artifact_id} or
                (self.selection.base.artifact_id == self.stage_image.artifact_id and
                 self.selection.base.sha256 != self.stage_image.sha256)):
            raise ValueError('INVALID_DUST_HOLDOUT_SELECTION')
        decision = {'version': 'sensor_dust_decision_v1',
                    'tenant_id': str(self.tenant_id), 'review_id': str(self.review_id),
                    'run_id': str(self.run_id), 'actor_id': str(self.actor_id),
                    'decision': 'accept', 'selection': self.selection.model_dump(mode='json')}
        if canonical_hash(decision) != self.decision_hash:
            raise ValueError('DUST_HOLDOUT_DECISION_HASH_MISMATCH')
        return self


class DustHoldoutExpected(Contract):
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    review_id: UUID
    run_id: UUID
    actor_id: UUID
    decision_hash: SHA256
    image: ArtifactRef
    stage_image: ArtifactRef
    protection: ArtifactRef
    proposal: ArtifactRef
    params: ArtifactRef


def decode_frozen_dust_holdout_evidence(raw: bytes, artifact: ArtifactRef,
                                        expected: DustHoldoutExpected) -> FrozenDustHoldoutEvidence:
    if (type(raw) is not bytes or not 2 <= len(raw) <= 1_048_576 or
            artifact.artifact_id.int == 0 or sha256(raw).hexdigest() != artifact.sha256):
        raise ValueError('INVALID_FROZEN_DUST_HOLDOUT')
    parse_json(raw)
    frozen = FrozenDustHoldoutEvidence.model_validate_json(raw)
    if canonical_bytes(frozen) != raw or any((
            frozen.tenant_id != expected.tenant_id,
            frozen.event_id != expected.event_id,
            frozen.asset_version_id != expected.asset_version_id,
            frozen.review_id != expected.review_id,
            frozen.run_id != expected.run_id,
            frozen.actor_id != expected.actor_id,
            frozen.decision_hash != expected.decision_hash,
            frozen.image != expected.image,
            frozen.stage_image != expected.stage_image,
            frozen.protection != expected.protection,
            frozen.proposal != expected.proposal,
            frozen.params != expected.params)):
        raise ValueError('STALE_FROZEN_DUST_HOLDOUT')
    return frozen


def validate_frozen_dust_material(evidence: FrozenDustHoldoutEvidence,
                                  params: DustRepairParams,
                                  proposal: DustCandidates) -> tuple[str, ...]:
    """Bind exact reviewed material before replaying any sensor-dust pixels.

    The caller must still obtain all three documents through authorized,
    SHA-pinned task inputs; this check cannot grant access or review authority.
    """
    evidence = FrozenDustHoldoutEvidence.model_validate_json(canonical_bytes(evidence))
    params = DustRepairParams.model_validate_json(canonical_bytes(params))
    proposal = DustCandidates.model_validate_json(canonical_bytes(proposal))
    if (params.image != evidence.stage_image or
            params.proposal != evidence.proposal or
            params.review != evidence.selection):
        raise ValueError('HOLDOUT_DUST_MATERIAL_CHANGED')
    selected = accepted_dust_candidates(params, proposal, evidence.selection)
    expected = tuple(evidence.selection.accepted_candidate_ids)
    if tuple(candidate.id for candidate in selected) != expected:
        raise ValueError('HOLDOUT_DUST_SELECTION_CHANGED')
    return expected


def validate_holdout_dust_binding(case, event_id: UUID,
                                  evidence: FrozenDustHoldoutEvidence) -> None:
    """Bind frozen dust evidence to the original training source and mask."""
    from .contracts import RenderCase
    if (not isinstance(case, RenderCase) or
            not isinstance(evidence, FrozenDustHoldoutEvidence) or
            case.dust_evidence is None or case.defect_evidence is not None or
            event_id.int == 0 or evidence.event_id != event_id or
            evidence.asset_version_id != case.binding.asset_version_id or
            evidence.image != case.working_image or
            evidence.protection != case.protected or
            evidence.stage_image == case.working_image):
        raise ValueError('HOLDOUT_DUST_BINDING_MISMATCH')
