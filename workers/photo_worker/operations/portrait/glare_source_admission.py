"""Concrete source-only Glare installation using the existing locked Big-LaMa."""
from contextlib import contextmanager

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.models.pool import ModelPool
from photo_worker.operations.semantic.candidate_inpaint import infer_inpaint_candidate
from photo_worker.runtime.executor import OperationFailure
from .glare_donor_admission import GlareDonorAdmission


class _GlareInpaint:
    def __init__(self, engine):
        self.engine = engine

    def infer(self, rgb, mask, *, check_active):
        check_active()
        result = infer_inpaint_candidate(self.engine, rgb, mask > 0, 1_081_600)
        check_active()
        return result


class GlareSourceAdmission(GlareDonorAdmission):
    """Keep donor behavior and add independently authorized source-only inference."""
    def __init__(self, *args, pool, backend, **kwargs):
        super().__init__(*args, **kwargs)
        if (not isinstance(pool, ModelPool) or backend not in ('cpu', 'cuda')
                or pool.lock.lock_id != self.model_lock_sha
                or not callable(getattr(self.database, 'glare_generative_source_ready', None))):
            raise ValueError('GLARE_SOURCE_INSTALLATION_INVALID')
        entries = [e for e in pool.lock.models if e.id == 'big_lama']
        if (len(entries) != 1 or not entries[0].approved_for_install
                or entries[0].backends[backend].status != 'passed'
                or entries[0].architecture != 'torchscript.big_lama.v1'):
            raise ValueError('GLARE_SOURCE_MODEL_UNQUALIFIED')
        self.pool, self.backend = pool, backend

    def admit_generative(self, ctx, params, proof, source, calibration):
        ctx.check_active()
        command, profile = ctx.lease.command, ctx.lease.execution_profile
        body = canonical_bytes(params)
        if (command.operation != 'portrait.glare' or command.operation_version != '1'
                or command.resource_class != 'gpu' or params.mode != 'generative'
                or params.donor is not None or params.donor_faces is not None
                or profile.get('backend') != self.backend
                or profile.get('restoration_runtime_sha') != self.runtime_sha
                or params.model_lock_sha != self.model_lock_sha
                or calibration.model_lock_sha != self.model_lock_sha
                or canonical_hash(params) != command.params.sha256
                or not isinstance(proof, bytes) or not 2 <= len(proof) <= 65536
                or self.database.glare_generative_source_ready(ctx.lease, body, proof) is not True):
            return False
        ctx.check_active()
        return True

    @contextmanager
    def acquire_inpaint(self, ctx, backend):
        ctx.check_active()
        if backend != self.backend:
            raise OperationFailure('BACKEND_UNAVAILABLE')
        with self.pool.acquire('big_lama', backend) as engine:
            ctx.check_active()
            if not callable(getattr(engine, 'infer', None)):
                raise OperationFailure('MODEL_CONTRACT_ERROR')
            yield _GlareInpaint(engine)
            ctx.check_active()
