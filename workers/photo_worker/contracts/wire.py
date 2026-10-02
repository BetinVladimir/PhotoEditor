from __future__ import annotations
from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef,SHA256


class ModelRef(Contract):
    id: str
    lock_id: SHA256


class TaskCommand(Contract):
    schema_version: Literal[1]
    event_id: UUID
    tenant_id: UUID
    run_id: UUID
    task_id: UUID
    attempt: Annotated[int,Field(ge=1)]
    operation: Annotated[str,Field(min_length=1,max_length=120)]
    operation_version: Literal['1']
    resource_class: Literal['cpu','gpu','media','archive','collection','mail']
    correlation_id: UUID
    inputs: Annotated[list[ArtifactRef],Field(max_length=512)]
    params: ArtifactRef
    model: ModelRef|None
    timeout_seconds: Annotated[int,Field(ge=1,le=7200)]
    traceparent: Annotated[str,Field(max_length=128)]


class TaskError(Contract):
    code: str
    retryable: bool
    message_key: str
    args: dict[str,str|int|float|bool|None]
    diagnostic_artifact_id: UUID|None=None


class TaskMetrics(Contract):
    duration_ms: Annotated[int,Field(ge=0)]
    peak_memory_bytes: Annotated[int,Field(ge=0)]
    peak_device_bytes: Annotated[int,Field(ge=0)]|None=None
    done: Annotated[int,Field(ge=0)]|None=None
    total: Annotated[int,Field(ge=0)]|None=None
    bytes_read: Annotated[int,Field(ge=0)]|None=None
    bytes_written: Annotated[int,Field(ge=0)]|None=None

    @model_validator(mode='after')
    def progress_bounds(self):
        if self.done is not None and self.total is not None and self.done>self.total:
            raise ValueError('Progress exceeds total')
        return self


class TaskResult(Contract):
    schema_version: Literal[1]
    event_id: UUID
    type: Literal['task.succeeded','task.failed','task.cancelled','task.progress']
    tenant_id: UUID
    run_id: UUID
    task_id: UUID
    attempt: Annotated[int,Field(ge=1)]
    fencing_token: Annotated[int,Field(ge=1)]
    outputs: Annotated[list[ArtifactRef],Field(max_length=128)]
    metrics: TaskMetrics
    error: TaskError|None

    @model_validator(mode='after')
    def error_contract(self):
        if self.type=='task.failed' and self.error is None:
            raise ValueError('Failed result must contain error')
        if self.type in ['task.succeeded','task.progress'] and self.error is not None:
            raise ValueError('Successful/progress result cannot contain error')
        if self.type=='task.progress' and self.outputs:
            raise ValueError('Progress cannot publish outputs')
        return self
