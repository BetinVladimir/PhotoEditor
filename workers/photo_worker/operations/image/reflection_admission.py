"""Live V28 reflection authority composed with the existing qualified ModelPool."""

from contextlib import contextmanager

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.executor import OperationFailure


class ReflectionModelAdmission:
    """Bind a single locked model to live SQL task and qualification authority."""

    def __init__(self, database, pool: ModelPool, backend: str,
                 runtime_sha: str):
        if (not isinstance(pool, ModelPool) or backend not in ('cpu', 'cuda')
                or not isinstance(runtime_sha, str) or len(runtime_sha) != 64
                or any(char not in '0123456789abcdef' for char in runtime_sha)
                or not callable(getattr(database, 'restoration_lease_ready', None))
                or not callable(getattr(database, 'restoration_qualification_ready', None))):
            raise ValueError('REFLECTION_INSTALLATION_INVALID')
        entries = [entry for entry in pool.lock.models if entry.id == 'reflection_rgb']
        if (len(entries) != 1 or not entries[0].approved_for_install
                or entries[0].backends[backend].status != 'passed'):
            raise ValueError('REFLECTION_MODEL_UNQUALIFIED')
        self.database = database
        self.pool = pool
        self.backend = backend
        self.lock_sha = pool.lock.lock_id
        self.runtime_sha = runtime_sha

    def admit(self, ctx, params, source, target, protected, calibration) -> bool:
        ctx.check_active()
        command = ctx.lease.command
        body = canonical_bytes(params)
        if (command.operation != 'image.reflection'
                or command.operation_version != '1'
                or ctx.lease.execution_profile.get('backend') != self.backend
                or ctx.lease.execution_profile.get('restoration_runtime_sha') != self.runtime_sha
                or params.model_lock_sha != self.lock_sha
                or calibration.model_lock_sha != self.lock_sha
                or canonical_hash(params) != command.params.sha256
                or len(body) > 65536):
            return False
        if (self.database.restoration_lease_ready(ctx.lease, body) is not True
                or self.database.restoration_qualification_ready(ctx.lease, body) is not True):
            return False
        ctx.check_active()
        return True

    @contextmanager
    def acquire(self, ctx, backend: str):
        ctx.check_active()
        if backend != self.backend:
            raise OperationFailure('BACKEND_UNAVAILABLE')
        with self.pool.acquire('reflection_rgb', backend) as engine:
            ctx.check_active()
            if (not callable(getattr(engine, 'infer', None))
                    or type(getattr(engine, 'size', None)) is not int):
                raise OperationFailure('MODEL_CONTRACT_ERROR')
            yield engine
            ctx.check_active()
