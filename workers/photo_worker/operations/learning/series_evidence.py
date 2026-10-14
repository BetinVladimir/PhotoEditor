"""Immutable Apply-series source binding for a V23 holdout example.

The document describes evidence; only current SQL authority and sealed task
inputs may grant access to the referenced artifacts.
"""
from hashlib import sha256
from typing import Literal
from uuid import UUID

from pydantic import model_validator

from photo_worker.contracts.base import (
    ArtifactRef, Contract, canonical_bytes, parse_json,
)


class FrozenSeriesHoldoutEvidence(Contract):
    schema_version: Literal[1]
    tenant_id: UUID
    event_id: UUID
    asset_version_id: UUID
    apply_run_id: UUID
    item_id: UUID
    producer_task_id: UUID
    source_image: ArtifactRef
    stage_image: ArtifactRef
    params: ArtifactRef
    series: ArtifactRef

    @model_validator(mode='after')
    def distinct_sources(self):
        if (any(value.int == 0 for value in (
                self.tenant_id, self.event_id, self.asset_version_id,
                self.apply_run_id, self.item_id, self.producer_task_id))
                or any(value.artifact_id.int == 0 for value in (
                    self.source_image, self.stage_image, self.params, self.series))
                or len({value.artifact_id for value in (
                    self.source_image, self.stage_image, self.params, self.series)}) != 4):
            raise ValueError('INVALID_HOLDOUT_SERIES_EVIDENCE')
        return self


def decode_frozen_series_holdout_evidence(
        raw: bytes, artifact: ArtifactRef,
        expected: FrozenSeriesHoldoutEvidence) -> FrozenSeriesHoldoutEvidence:
    if (type(raw) is not bytes or not 2 <= len(raw) <= 16384 or
            artifact.artifact_id.int == 0 or sha256(raw).hexdigest() != artifact.sha256):
        raise ValueError('INVALID_FROZEN_SERIES_HOLDOUT')
    parse_json(raw)
    value = FrozenSeriesHoldoutEvidence.model_validate_json(raw)
    if canonical_bytes(value) != raw or value != expected:
        raise ValueError('STALE_FROZEN_SERIES_HOLDOUT')
    return value
