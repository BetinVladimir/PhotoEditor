"""Validated, immutable per-node resource policy for the v2 deployment."""
from __future__ import annotations

from math import ceil
from pathlib import Path
from typing import Annotated, Literal

from pydantic import ConfigDict, Field, model_validator

from photo_worker.contracts.base import Contract, canonical_hash, parse_json

from .capacity import Capacity

Positive = Annotated[int, Field(ge=1, le=2**53 - 1)]


class QueueWeights(Contract):
    model_config = ConfigDict(frozen=True)
    interactive: Annotated[int, Field(ge=1, le=100)]
    capture: Annotated[int, Field(ge=1, le=100)]
    batch: Annotated[int, Field(ge=1, le=100)]
    training: Annotated[int, Field(ge=1, le=100)]


class CapacityPolicy(Contract):
    model_config = ConfigDict(frozen=True)
    schema_version: Literal[1]
    gpu_concurrency: Positive
    cpu_concurrency: Positive
    cpu_threads_per_task: Positive
    heavy_admission_inflight: Positive
    tenant_heavy_queued: Positive
    gpu_budget_bytes: Positive
    photo_ram_budget_bytes: Positive
    control_ram_budget_bytes: Positive
    scratch_free_floor_fraction: Annotated[float, Field(ge=0.05, le=0.5)]
    scratch_multiplier: Annotated[float, Field(ge=1, le=4)]
    tile_edge: Positive
    heartbeat_seconds: Positive
    offline_seconds: Positive
    lease_seconds: Positive
    renew_seconds: Positive
    queue_weights: QueueWeights
    unknown_cost_seconds: Positive
    max_retries_after_oom: Annotated[int, Field(ge=0, le=1)]

    @model_validator(mode='after')
    def coherent_intervals(self):
        if self.renew_seconds >= self.lease_seconds or self.heartbeat_seconds >= self.offline_seconds:
            raise ValueError('CAPACITY_POLICY_INTERVAL_INVALID')
        return self

    @property
    def sha256(self) -> str:
        return canonical_hash(self)

    def cap_measurement(self, measured: Capacity, *, resource_class: str,
                        scratch_total_bytes: int | None = None) -> Capacity:
        """Advertise no more than the operator budget and measured free resources."""
        ram_limit = self.photo_ram_budget_bytes if resource_class in ('gpu', 'media', 'archive') else self.control_ram_budget_bytes
        floor = ceil(scratch_total_bytes * self.scratch_free_floor_fraction) if scratch_total_bytes is not None else 0
        return Capacity(min(measured.ram_bytes, ram_limit), max(0, measured.scratch_bytes - floor),
                        min(measured.vram_bytes, self.gpu_budget_bytes))


def load_capacity_policy(path: Path) -> CapacityPolicy:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 65_536:
        raise ValueError('CAPACITY_POLICY_PATH_INVALID')
    return CapacityPolicy.model_validate(parse_json(path.read_bytes(), max_bytes=65_536))
