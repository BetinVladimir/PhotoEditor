"""Validated, immutable per-node resource policy for the v2 deployment."""
from __future__ import annotations

import os
import stat
from decimal import Decimal
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
    heartbeat_seconds: Annotated[int, Field(ge=1, le=10)]
    offline_seconds: Positive
    lease_seconds: Positive
    renew_seconds: Annotated[int, Field(ge=1, le=10)]
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
        if (not isinstance(measured, Capacity)
                or resource_class not in ('gpu', 'media', 'archive', 'cpu')
                or any(type(value) is not int or not 0 <= value <= 2**53 - 1
                       for value in (measured.ram_bytes, measured.vram_bytes, measured.scratch_bytes))
                or (scratch_total_bytes is not None and
                    (type(scratch_total_bytes) is not int or not 1 <= scratch_total_bytes <= 2**53 - 1
                     or measured.scratch_bytes > scratch_total_bytes))):
            raise ValueError('CAPACITY_MEASUREMENT_INVALID')
        ram_limit = self.photo_ram_budget_bytes if resource_class in ('gpu', 'media', 'archive') else self.control_ram_budget_bytes
        # Interpret the policy's decimal fraction exactly: binary multiplication
        # can round a positive fractional byte down to an integer near 2**53.
        # Reserve ceil(total * fraction) without float arithmetic or Decimal
        # context-dependent multiplication.
        floor = 0
        if scratch_total_bytes is not None:
            numerator, denominator = Decimal(str(self.scratch_free_floor_fraction)).as_integer_ratio()
            floor = (scratch_total_bytes * numerator + denominator - 1) // denominator
        return Capacity(min(measured.ram_bytes, ram_limit), max(0, measured.scratch_bytes - floor),
                        min(measured.vram_bytes, self.gpu_budget_bytes))


def trusted_capacity_owner(owner, effective=None):
    if effective is None:
        effective = os.geteuid()
    return owner in (0, effective)


def _open_capacity_policy(path: Path):
    parts = path.parts
    if not path.is_absolute() or len(parts) < 2 or '..' in parts:
        raise ValueError('CAPACITY_POLICY_PATH_INVALID')
    directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        root = os.fstat(directory)
        if (not stat.S_ISDIR(root.st_mode) or root.st_mode & 0o022
                or not trusted_capacity_owner(root.st_uid)):
            raise ValueError('CAPACITY_POLICY_PATH_INVALID')
        for part in parts[1:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=directory)
            try:
                info = os.fstat(child)
                if (not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o022
                        or not trusted_capacity_owner(info.st_uid)):
                    raise ValueError('CAPACITY_POLICY_PATH_INVALID')
            except BaseException:
                os.close(child)
                raise
            os.close(directory)
            directory = child
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=directory)
        return directory, descriptor
    except BaseException:
        os.close(directory)
        raise


def load_capacity_policy(path: Path) -> CapacityPolicy:
    # Bind the admitted byte ceiling to one bounded regular-file read. Never
    # follow a replaced symlink or let a growing file bypass the JSON budget.
    path=Path(os.path.abspath(path))
    directory,descriptor=_open_capacity_policy(path)
    try:
        before=os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or not 2<=before.st_size<=65_536
                or before.st_mode & 0o022 or not trusted_capacity_owner(before.st_uid)):
            raise ValueError('CAPACITY_POLICY_PATH_INVALID')
        body=bytearray()
        while len(body)<=before.st_size:
            chunk=os.read(descriptor,min(8192,before.st_size+1-len(body)))
            if not chunk:break
            body.extend(chunk)
        after=os.fstat(descriptor)
        current=os.stat(path.name,dir_fd=directory,follow_symlinks=False)
        external=path.stat(follow_symlinks=False)
        identity=lambda item:(item.st_dev,item.st_ino,item.st_size,item.st_mtime_ns,item.st_ctime_ns,item.st_mode,item.st_uid)
        if (len(body)!=before.st_size or identity(before)!=identity(after)
                or identity(after)!=identity(current) or identity(current)!=identity(external)):
            raise ValueError('CAPACITY_POLICY_CHANGED')
    finally:
        os.close(descriptor)
        os.close(directory)
    return CapacityPolicy.model_validate(parse_json(bytes(body),max_bytes=65_536))
