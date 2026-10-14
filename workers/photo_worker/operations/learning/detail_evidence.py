"""Frozen V23 portrait-detail inputs; SQL producer authority is external.

This module does not enable portrait detail in the holdout pipeline. The
scheduler must select the event-owned successful producer and register every
referenced object before an Evaluate worker may consume this document.
"""
from __future__ import annotations

from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID, uuid5

from pydantic import Field, model_validator

from photo_worker.contracts.base import (
    ArtifactRef, Contract, canonical_bytes, parse_json,
)
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.contracts.resolved import ResolvedStyle


class FrozenPortraitDetailHoldoutEvidence(Contract):
    schema_version: Literal[1]
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    run_id: UUID
    task_id: UUID
    result_id: UUID
    attempt_id: UUID
    source: ArtifactRef
    image: ArtifactRef
    protection: ArtifactRef
    params: ArtifactRef
    resolved: ArtifactRef
    mask_artifacts: Annotated[list[ArtifactRef], Field(min_length=1, max_length=160)]

    @model_validator(mode='after')
    def closed_identity(self):
        identities = (self.tenant_id, self.event_id, self.asset_version_id,
                      self.run_id, self.task_id, self.result_id, self.attempt_id)
        refs = (self.source, self.image, self.protection, self.params,
                self.resolved,
                *self.mask_artifacts)
        if (any(identity.int == 0 for identity in identities) or
                any(ref.artifact_id.int == 0 for ref in refs) or
                len({ref.artifact_id for ref in refs}) != len(refs) or
                self.mask_artifacts != sorted(
                    self.mask_artifacts, key=lambda ref: ref.artifact_id.bytes)):
            raise ValueError('INVALID_DETAIL_HOLDOUT_IDENTITY')
        return self


class PortraitDetailHoldoutExpected(Contract):
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    run_id: UUID
    task_id: UUID
    result_id: UUID
    attempt_id: UUID
    source: ArtifactRef
    image: ArtifactRef
    protection: ArtifactRef
    params: ArtifactRef
    resolved: ArtifactRef
    mask_artifacts: Annotated[list[ArtifactRef], Field(min_length=1, max_length=160)]


def decode_frozen_detail_evidence(raw: bytes, artifact: ArtifactRef,
                                  expected: PortraitDetailHoldoutExpected,
                                  ) -> FrozenPortraitDetailHoldoutEvidence:
    """Check original canonical bytes against a SQL-selected producer identity."""
    if (type(raw) is not bytes or not 2 <= len(raw) <= 1_048_576 or
            artifact.artifact_id.int == 0 or
            sha256(raw).hexdigest() != artifact.sha256):
        raise ValueError('INVALID_FROZEN_DETAIL_HOLDOUT')
    parse_json(raw)
    frozen = FrozenPortraitDetailHoldoutEvidence.model_validate_json(raw)
    if canonical_bytes(frozen) != raw or any((
            frozen.tenant_id != expected.tenant_id,
            frozen.event_id != expected.event_id,
            frozen.asset_version_id != expected.asset_version_id,
            frozen.run_id != expected.run_id,
            frozen.task_id != expected.task_id,
            frozen.result_id != expected.result_id,
            frozen.attempt_id != expected.attempt_id,
            frozen.source != expected.source,
            frozen.image != expected.image,
            frozen.protection != expected.protection,
            frozen.params != expected.params,
            frozen.resolved != expected.resolved,
            frozen.mask_artifacts != expected.mask_artifacts)):
        raise ValueError('STALE_FROZEN_DETAIL_HOLDOUT')
    return frozen


def validate_frozen_detail_material(evidence: FrozenPortraitDetailHoldoutEvidence,
                                    params: PortraitParams, image: WorkingImage,
                                    protection: MaskRef,
                                    resolved: ResolvedStyle) -> None:
    """Require the exact source, masks and enabled effect in frozen Params.

    Physical NPY bytes, the source/producer SQL lineage and three-arm execution
    remain separate mandatory gates. This check grants no artifact access.
    """
    if (not isinstance(evidence, FrozenPortraitDetailHoldoutEvidence) or
            not isinstance(params, PortraitParams) or
            not isinstance(image, WorkingImage) or
            not isinstance(protection, MaskRef) or
            not isinstance(resolved, ResolvedStyle) or
            evidence.source == evidence.image or
            params.image != evidence.image or
            params.resolved != evidence.resolved or
            resolved.params.image != evidence.image or
            resolved.mode == 'skip' or
            not protection.protected or
            not any(canonical_bytes(mask) == canonical_bytes(protection)
                    for mask in params.protected)):
        raise ValueError('HOLDOUT_DETAIL_MATERIAL_CHANGED')
    detail = resolved.modules.extensions.portrait_detail
    masks = tuple(region.mask for region in params.regions) + tuple(params.protected)
    by_id = {mask.artifact.artifact_id: mask.artifact for mask in masks}
    refs = sorted(by_id.values(),
                  key=lambda ref: ref.artifact_id.bytes)
    if (detail is None or not detail.enabled or not params.regions or
            len({(mask.artifact.artifact_id, mask.artifact.sha256)
                 for mask in masks}) != len(by_id) or
            refs != evidence.mask_artifacts or
            any(mask.canvas != image.canvas or
                mask.transform_chain != image.transform_chain for mask in masks)):
        raise ValueError('HOLDOUT_DETAIL_MATERIAL_CHANGED')


def validate_holdout_detail_binding(case, event_id: UUID,
                                   training_run_id: UUID, tenant_id: UUID,
                                   evidence_ref: ArtifactRef,
                                   evidence: FrozenPortraitDetailHoldoutEvidence) -> None:
    """Bind worker-visible evidence to the frozen manifest and training run.

    SQL still owns the event→Apply producer proof; this check prevents an
    admitted Fit task from silently ignoring or substituting that evidence.
    """
    from .contracts import RenderCase
    if (not isinstance(case, RenderCase) or
            not isinstance(evidence, FrozenPortraitDetailHoldoutEvidence) or
            case.detail_evidence != evidence_ref or
            case.defect_evidence is not None or case.dust_evidence is not None or
            event_id.int == 0 or training_run_id.int == 0 or tenant_id.int == 0 or
            evidence_ref.artifact_id != uuid5(training_run_id,
                f'v23-detail/{event_id}') or
            evidence.tenant_id != tenant_id or evidence.event_id != event_id or
            evidence.asset_version_id != case.binding.asset_version_id or
            evidence.source != case.working_image or
            evidence.protection != case.protected or
            evidence.image == evidence.source):
        raise ValueError('HOLDOUT_DETAIL_BINDING_MISMATCH')
