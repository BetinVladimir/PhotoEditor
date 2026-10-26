from __future__ import annotations
from dataclasses import dataclass
from datetime import datetime, timezone
from threading import Event, Lock, Thread
from time import monotonic, sleep
from typing import Callable
from uuid import UUID
import psutil
import psycopg
import sys
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, parse_json
from photo_worker.contracts.wire import TaskCommand, TaskError, TaskMetrics
from .database import Heartbeat, Lease, TaskDatabase
from .gateway import GatewayError
from .diagnostics import emit_failure_metadata
from .materialize import ArtifactTransferError
from .scratch import DiskQuota, ScratchError
from photo_worker.v2.input_closure import V2InputsInvalid


class PoisonCommand(ValueError):
    pass


class LeaseLost(RuntimeError):
    pass


class TaskCancelled(RuntimeError):
    pass


class OperationFailure(RuntimeError):
    def __init__(self, code: str, retryable: bool = False, args: dict | None = None):
        super().__init__(code)
        self.error = TaskError(code=code, retryable=retryable,
                               message_key='errors.'+code.lower(), args=args or {})


HEARTBEAT_SAFETY_MARGIN_SECONDS = 2.0
COMMIT_RETRY_ATTEMPTS = 6


@dataclass(frozen=True)
class OperationResult:
    outputs: list[ArtifactRef]
    review_required: bool = False
    peak_device_bytes: int | None = None
    bytes_read: int | None = None
    bytes_written: int | None = None


class ExecutionContext:
    def __init__(self, lease: Lease):
        self.lease = lease
        self._lost, self._cancelled = Event(), Event()
        self._lock = Lock()
        self._progress: dict = {}
        # Convert once from database UTC to a monotonic local deadline. Clock
        # skew can change the local estimate; SQL remains the authority for
        # every input/output operation and terminal commit.
        self._deadline = monotonic() + max(0, (lease.deadline-datetime.now(timezone.utc)).total_seconds())
        self._lease_until = monotonic() + max(0, (lease.until-datetime.now(timezone.utc)).total_seconds())

    def check_active(self):
        with self._lock:
            if self._lost.is_set() or monotonic() >= self._lease_until:
                # Once this attempt observes expiry, a delayed heartbeat must
                # never revive its local authority.
                self._lost.set()
                raise LeaseLost('Task lease lost')
            if self._cancelled.is_set():
                raise TaskCancelled('Task cancelled')
            if monotonic() >= self._deadline:
                raise OperationFailure('TIMEOUT', True)

    def accept_heartbeat(self, beat: Heartbeat) -> bool:
        with self._lock:
            if (self._lost.is_set() or monotonic() >= self._lease_until
                    or beat.accepted is not True or beat.until is None):
                self._lost.set()
                return False
            if beat.cancel_requested:
                self._cancelled.set()
                return False
            until = monotonic() + max(0, (beat.until-datetime.now(timezone.utc)).total_seconds())
            if until <= monotonic():
                self._lost.set()
                return False
            self._lease_until = until
            return True

    def progress(self, done: int, total: int):
        if isinstance(done, bool) or isinstance(total, bool) or not isinstance(done,int) or not isinstance(total,int) or not 0 <= done <= total:
            raise ValueError('Invalid progress')
        self.check_active()
        with self._lock:
            if self._progress and (total != self._progress['total'] or done < self._progress['done']):
                raise ValueError('Progress must be monotone with a stable total')
            self._progress = dict(done=done,total=total)

    def snapshot_progress(self):
        with self._lock:
            return dict(self._progress)


@dataclass(frozen=True)
class Operation:
    name: str
    resource_class: str
    execute: Callable[[ExecutionContext], OperationResult]
    version: str = '1'


class Executor:
    """Process one delivery. A normal return permits ACK; an exception does not.

    Handlers must check context before uploads and between bounded tiles/stages.
    Unknown operations are poison, never an identity pass-through. A lost lease
    permits ACK because recovery belongs to PostgreSQL, not Rabbit redelivery.
    """
    def __init__(self, database: TaskDatabase, worker_id: UUID, operations: list[Operation],
                 heartbeat_seconds: float = 10):
        if not 0 < heartbeat_seconds <= 10:
            raise ValueError('Heartbeat interval must be in (0,10]')
        # Registration is advertised before the first task arrives. Reject a
        # malformed supplied factory now, rather than reporting a non-executable
        # operation as installed and consuming its first real command later.
        for op in operations:
            if (not isinstance(op, Operation) or not isinstance(op.name, str)
                    or not 1 <= len(op.name) <= 120 or op.version != '1'
                    or op.resource_class not in ('cpu', 'gpu', 'media', 'archive', 'collection', 'mail')
                    or not callable(op.execute)):
                raise ValueError('Invalid operation installation')
        self.database, self.worker_id = database, worker_id
        self.operations = {op.name: op for op in operations}
        if len(self.operations) != len(operations):
            raise ValueError('Duplicate operation')
        self.heartbeat_seconds = heartbeat_seconds
        self._active_lock = Lock()
        self._active: set[ExecutionContext] = set()
        self._abort_epoch = 0

    def abort_active(self):
        """Transport loss/shutdown stops writes; PostgreSQL recovers the lease."""
        with self._active_lock:
            self._abort_epoch += 1
            for context in self._active:
                context._lost.set()

    def decode(self, raw: bytes) -> tuple[TaskCommand, Operation]:
        try:
            parse_json(raw, max_bytes=1048576)
            command = TaskCommand.model_validate_json(raw)
            archive = (command.operation == 'archive.build' and command.operation_version == '1'
                       and command.resource_class == 'archive')
            if not archive and len(raw)>65536:
                raise ValueError('Non-archive task command exceeds byte limit')
        except (ValueError,ValidationError) as exc:
            raise PoisonCommand('Invalid task command') from exc
        op = self.operations.get(command.operation)
        if op is None or op.version != command.operation_version or op.resource_class != command.resource_class or command.attempt > 6:
            raise PoisonCommand('Unsupported task command')
        return command, op

    @staticmethod
    def _commit_with_retry(stop: Event, call, attempts: int = COMMIT_RETRY_ATTEMPTS):
        """W-3: retry a terminal commit over transient transport errors.

        commit_* are fenced and idempotent in SQL ('replayed'/'stale'), so a
        repeat after an unknown result is safe. Non-transient errors and the
        final failure propagate unchanged; the message is then redelivered
        and the reaper handles the task.
        """
        delay = 1.0
        for attempt in range(attempts):
            try:
                return call()
            except (psycopg.OperationalError, OSError, TimeoutError) as exc:
                if attempt == attempts - 1:
                    raise
                emit_failure_metadata(exc,'DEPENDENCY_UNAVAILABLE','commit_retry')
                sleep(delay)
                delay = min(delay*2,15.0)

    def process(self, raw: bytes) -> str:
        command, op = self.decode(raw)
        with self._active_lock:
            epoch = self._abort_epoch
        claim = self.database.claim(command, self.worker_id)
        if claim.outcome in ('missing','terminal','busy','stale','cancelled','unavailable'):
            return claim.outcome
        if claim.outcome == 'unauthorized':
            raise PoisonCommand('Worker is not authorized for task')
        if claim.outcome != 'claimed' or claim.lease is None:
            raise RuntimeError('Invalid claim response')
        ctx = ExecutionContext(claim.lease)
        stop = Event()
        started = monotonic()
        process = psutil.Process()
        peak_memory = process.memory_info().rss

        def heartbeat():
            nonlocal peak_memory
            beat_failures = 0
            while not stop.wait(self.heartbeat_seconds if beat_failures == 0 else 0):
                try:
                    peak_memory = max(peak_memory,process.memory_info().rss)
                    beat = self.database.heartbeat(ctx.lease,ctx.snapshot_progress())
                    if not ctx.accept_heartbeat(beat):
                        return
                except Exception as exc:
                    emit_failure_metadata(exc,'LEASE_LOST','heartbeat')
                    # W-1: a transient control-plane error (VPN blip, DB
                    # restart) must not abort GPU work while the last lease
                    # confirmed by SQL is still valid locally. Retry with a
                    # short backoff; the SQL fence stays the authority for
                    # every write and commit. Once the local lease margin is
                    # exhausted, stop conservatively and let the reaper retry.
                    if monotonic() >= ctx._lease_until - HEARTBEAT_SAFETY_MARGIN_SECONDS:
                        ctx._lost.set(); return
                    # Back off 1s..5s, never past the next regular beat.
                    if stop.wait(min(5.0,1.0+0.5*beat_failures,self.heartbeat_seconds)):
                        return
                    beat_failures += 1
                    continue
                beat_failures = 0

        thread = Thread(target=heartbeat,name='task-heartbeat',daemon=True)
        with self._active_lock:
            self._active.add(ctx)
            if epoch != self._abort_epoch:
                ctx._lost.set()
        thread.start()
        result = None
        failure = None
        try:
            ctx.check_active()
            try:
                result = op.execute(ctx)
                if not isinstance(result,OperationResult):
                    raise TypeError('Invalid operation result')
                ctx.check_active()
            except LeaseLost:
                return 'lease_lost'
            except TaskCancelled:
                failure = TaskError(code='TASK_CANCELLED',retryable=False,message_key='errors.cancelled',args={})
            except OperationFailure as exc:
                failure = exc.error
            except GatewayError as exc:
                if exc.code == 'LEASE_LOST':
                    return 'lease_lost'
                allowed={'DEPENDENCY_UNAVAILABLE','ARTIFACT_MISSING','ARTIFACT_HASH_MISMATCH','DISK_QUOTA','CONFIG_INVALID'}
                code=exc.code if exc.code in allowed else 'INTERNAL_ERROR'
                emit_failure_metadata(exc,code,'operation')
                failure=TaskError(code=code,retryable=code=='DEPENDENCY_UNAVAILABLE' and exc.retryable,message_key='errors.'+code.lower(),args={})
            except ArtifactTransferError as exc:
                code='ARTIFACT_HASH_MISMATCH' if exc.args==('ARTIFACT_HASH_MISMATCH',) else 'INPUT_INVALID'
                failure=TaskError(code=code,retryable=False,message_key='errors.'+code.lower(),args={})
            except V2InputsInvalid:
                failure=TaskError(code='TASK_INPUTS_INVALID',retryable=False,
                                  message_key='errors.task_inputs_invalid',args={})
            except DiskQuota:
                failure=TaskError(code='DISK_QUOTA',retryable=False,message_key='errors.disk_quota',args={})
            except ScratchError:
                failure=TaskError(code='CONFIG_INVALID',retryable=False,message_key='errors.config_invalid',args={})
            except MemoryError:
                failure = TaskError(code='RESOURCE_OOM',retryable=True,message_key='errors.resource_oom',args={})
            except (psycopg.errors.SerializationFailure, psycopg.errors.DeadlockDetected) as exc:
                emit_failure_metadata(exc,'DEPENDENCY_UNAVAILABLE','operation')
                # The DB adapter already exhausted its bounded transaction retry.
                # Preserve this as a dependency failure for normal fenced task
                # recovery, never replay the operation inline or persist SQL text.
                failure = TaskError(code='DEPENDENCY_UNAVAILABLE',retryable=True,
                                    message_key='errors.dependency_unavailable',args={})
            except Exception as exc:
                # Never persist paths, SQL, tokens or arbitrary exception text.
                torch = sys.modules.get('torch')
                oom = torch is not None and isinstance(exc,torch.OutOfMemoryError)
                code = 'RESOURCE_OOM' if oom else 'INTERNAL_ERROR'
                emit_failure_metadata(exc,code,'operation')
                failure = TaskError(code=code,retryable=oom,message_key='errors.'+code.lower(),args={})
            metrics = TaskMetrics(duration_ms=max(0,int((monotonic()-started)*1000)),
                                  peak_memory_bytes=max(peak_memory,process.memory_info().rss),
                                  **({} if result is None else dict(peak_device_bytes=result.peak_device_bytes,
                                     bytes_read=result.bytes_read,bytes_written=result.bytes_written)))
            if ctx._lost.is_set():
                return 'lease_lost'
            if failure is not None:
                outcome = self._commit_with_retry(stop,lambda: self.database.commit_failure(ctx.lease,failure,metrics))
            else:
                payload = canonical_bytes(dict(outputs=[x.model_dump(mode='json') for x in result.outputs],
                                               metrics=metrics.model_dump(mode='json'),review_required=result.review_required))
                outcome,_,_ = self._commit_with_retry(stop,lambda: self.database.commit_success(ctx.lease,payload))
                if outcome == 'cancelled':
                    error = TaskError(code='TASK_CANCELLED',retryable=False,message_key='errors.cancelled',args={})
                    outcome = self._commit_with_retry(stop,lambda: self.database.commit_failure(ctx.lease,error,metrics))
            if outcome == 'conflict':
                raise RuntimeError('COMMIT_CONFLICT')
            if outcome not in ('committed','replayed','stale'):
                raise RuntimeError('Invalid task commit response')
            return outcome
        finally:
            stop.set()
            thread.join(timeout=11)
            with self._active_lock:
                self._active.discard(ctx)
