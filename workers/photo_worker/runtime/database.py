from __future__ import annotations
from dataclasses import dataclass
from datetime import datetime
from uuid import UUID
import psycopg
from time import sleep
from psycopg.types.json import Jsonb
from photo_worker.contracts.wire import TaskCommand, TaskError, TaskMetrics


@dataclass(frozen=True)
class Lease:
    command: TaskCommand
    worker_id: UUID
    fence: int
    until: datetime
    deadline: datetime
    execution_profile: dict
    input_digest: str


@dataclass(frozen=True)
class Claim:
    outcome: str
    lease: Lease | None


@dataclass(frozen=True)
class Heartbeat:
    accepted: bool
    until: datetime | None
    cancel_requested: bool


class TaskDatabase:
    """Each call has its own bounded connection/transaction, including heartbeat.

    The login must have photo_worker membership and an enabled registration bound
    to session_user. No tenant-wide table grants or caller-controlled SQL exist.
    """
    def __init__(self, dsn: str):
        self._dsn = dsn

    def _connect(self):
        return psycopg.connect(self._dsn, connect_timeout=5,
                              options='-c role=photo_worker -c statement_timeout=5000 -c lock_timeout=3000')

    def heartbeat_cpu_worker(self,worker_id:UUID,resource_class:str,operations:frozenset[str],ram_bytes:int,scratch_bytes:int)->bool:
        row=self._query('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,%s,%s)',(worker_id,resource_class,sorted(operations),ram_bytes,scratch_bytes))
        return row is not None and row[0] is True

    def heartbeat_accelerator_worker(self,worker_id:UUID,backend:str,operations:frozenset[str],ram_bytes:int,vram_bytes:int,scratch_bytes:int)->bool:
        row=self._query('SELECT photo.heartbeat_accelerator_worker(%s,%s,%s,%s,%s,%s)',(worker_id,backend,sorted(operations),ram_bytes,vram_bytes,scratch_bytes))
        return row is not None and row[0] is True

    def accelerator_operations(self,worker_id:UUID,backend:str)->frozenset[str]:
        with self._connect() as conn:
            rows=conn.execute("SELECT operation FROM photo.worker_runtime_operations(%s,'gpu',%s)",(worker_id,backend)).fetchall()
        if len(rows)>128:raise ValueError('Worker operation limit exceeded')
        return frozenset(row[0] for row in rows)

    def runtime_operations(self,worker_id:UUID,resource_class:str)->frozenset[str]:
        with self._connect() as conn:
            rows=conn.execute('SELECT operation FROM photo.worker_runtime_operations(%s,%s)',(worker_id,resource_class)).fetchall()
        if len(rows)>128:raise ValueError('Worker operation limit exceeded')
        return frozenset(row[0] for row in rows)

    def ping(self):
        with self._connect() as conn:
            conn.execute('SELECT 1').fetchone()

    def _query(self, query: str, params: tuple):
        for attempt in range(3):
            try:
                with self._connect() as conn:
                    row = conn.execute(query,params).fetchone()
                return row
            except (psycopg.errors.SerializationFailure,psycopg.errors.DeadlockDetected):
                if attempt == 2:
                    raise
                sleep(.01*(attempt+1))

    def admit_lens_user_composition(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v25_lens_user_composition(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_render_recipe(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v26_render_recipe(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def admit_region_preview(self, lease: Lease, params: bytes) -> bool:
        command = lease.command
        row = self._query(
            'SELECT photo.admit_v26_region_preview(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (command.tenant_id, command.run_id, command.task_id, command.attempt,
             lease.fence, lease.worker_id, command.params.artifact_id,
             command.params.sha256, params.decode('utf-8'), lease.input_digest))
        return row is not None and row[0] is True

    def claim(self, command: TaskCommand, worker_id: UUID) -> Claim:
        row = self._query('SELECT * FROM photo.claim_task(%s,%s,%s,%s)',
                               (command.task_id, command.attempt, worker_id,
                                Jsonb(command.model_dump(mode='json'))))
        outcome, fence, until, deadline, context = row
        if outcome != 'claimed':
            return Claim(outcome, None)
        if context['command'] != command.model_dump(mode='json'):
            raise ValueError('Claimed context does not match command')
        return Claim(outcome, Lease(command, worker_id, fence, until, deadline,
                                    context['execution_profile'], context['input_digest']))

    def heartbeat(self, lease: Lease, progress: dict) -> Heartbeat:
        row = self._query('SELECT * FROM photo.heartbeat_task(%s,%s,%s,%s,%s)',
                               (lease.command.task_id, lease.command.attempt, lease.fence,
                                lease.worker_id, Jsonb(progress)))
        return Heartbeat(*row)

    def commit_success(self, lease: Lease, manifest: bytes) -> tuple[str, UUID | None, str | None]:
        return self._query('SELECT * FROM photo.commit_task_success(%s,%s,%s,%s,%s)',
                                (lease.command.task_id, lease.command.attempt, lease.fence,
                                 lease.worker_id, manifest.decode('utf-8')))

    def commit_failure(self, lease: Lease, error: TaskError, metrics: TaskMetrics) -> str:
        return self._query('SELECT photo.commit_task_failure(%s,%s,%s,%s,%s,%s)',
                                (lease.command.task_id, lease.command.attempt, lease.fence,
                                 lease.worker_id, Jsonb(error.model_dump(mode='json')),
                                 Jsonb(metrics.model_dump(mode='json'))))[0]
