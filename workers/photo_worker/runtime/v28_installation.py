"""Explicit V28 operation installation; absent qualification means no route."""

from photo_worker.runtime.executor import Operation, OperationFailure
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.contracts.base import canonical_bytes


class _LeaseBoundGlareAdmission:
    """Require server-issued donor proof even when a custom provider is installed."""

    def __init__(self, provider, database):
        self.provider = provider
        self.database = database

    def __getattr__(self, name):
        return getattr(self.provider, name)

    def admit(self, ctx, params, proof, source, donor, calibration):
        ctx.check_active()
        if self.database.glare_donor_source_ready(
                ctx.lease, canonical_bytes(params), bytes(proof)) is not True:
            raise OperationFailure('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
        ctx.check_active()
        decision = self.provider.admit(ctx, params, proof, source, donor, calibration)
        ctx.check_active()
        return decision is True


def installed_v28_operations(artifacts, database, config, *,
                            glare_factory=None, reflection_factory=None):
    """Bind trusted admission providers to the V28 handlers at process startup.

    Factories receive the task database and worker config. They must construct
    live tenant-scoped authorities; task Params cannot supply an authority.
    """
    if glare_factory is None and reflection_factory is None:
        return []
    from .service import WorkerConfigurationError
    if config.resource_class != 'gpu' or config.backend not in ('cpu', 'cuda'):
        raise WorkerConfigurationError('V28 requires a CPU or CUDA GPU-class worker')
    if (not callable(getattr(database, 'restoration_lease_ready', None))
            or not callable(getattr(database, 'restoration_qualification_ready', None))):
        raise WorkerConfigurationError('V28 live SQL lease/qualification gates are unavailable')

    def current_lease(ctx, task_artifacts, params_type):
        ctx.check_active()
        params = task_artifacts.params(params_type)
        body = canonical_bytes(params)
        def verify_current():
            ctx.check_active()
            if (len(body) > 65536
                    or database.restoration_lease_ready(ctx.lease, body) is not True
                    or database.restoration_qualification_ready(ctx.lease, body) is not True):
                raise OperationFailure('V28_AUTHORITY_UNAVAILABLE')
            ctx.check_active()
        verify_current()
        task_artifacts.load_v2_index()
        verify_current()

    installed = []
    if glare_factory is not None:
        if not callable(glare_factory):
            raise WorkerConfigurationError('Invalid glare admission factory')
        glare_admission = glare_factory(database, config)
        if (glare_admission is None or not callable(getattr(glare_admission, 'admit', None))
                or not callable(getattr(glare_admission, 'reader', None))
                or getattr(glare_admission, 'material_scratch_root', None) is None
                or type(getattr(glare_admission, 'material_max_bytes', None)) is not int
                or not 1 <= glare_admission.material_max_bytes <= config.max_scratch_bytes
                or getattr(glare_admission, 'alpha_mode', None) not in ('require_float32', 'normalize_uint16')):
            raise WorkerConfigurationError('Invalid glare admission installation')
        if not callable(getattr(database, 'glare_donor_source_ready', None)):
            raise WorkerConfigurationError('Glare donor SQL authority is unavailable')
        generative_fields = ('admit_generative', 'acquire_inpaint', 'model_lock_sha')
        if any(getattr(glare_admission, name, None) is not None for name in generative_fields):
            model_sha = getattr(glare_admission, 'model_lock_sha', None)
            if (not callable(getattr(glare_admission, 'admit_generative', None))
                    or not callable(getattr(glare_admission, 'acquire_inpaint', None))
                    or not isinstance(model_sha, str) or len(model_sha) != 64
                    or any(char not in '0123456789abcdef' for char in model_sha)):
                raise WorkerConfigurationError('Incomplete glare generative installation')
        from photo_worker.operations.portrait.glare import execute as execute_glare
        from photo_worker.contracts.portrait_restoration import PortraitGlareParams
        glare_admission = _LeaseBoundGlareAdmission(glare_admission, database)
        def glare(ctx):
            if ctx.lease.execution_profile.get('backend') != config.backend:
                raise OperationFailure('CONFIG_INVALID')
            with TaskArtifacts.create(ctx, artifacts, _V28_ROLES,
                                      defer_v2_index=True) as task_artifacts:
                current_lease(ctx, task_artifacts, PortraitGlareParams)
                result = execute_glare(ctx, task_artifacts, glare_admission)
                current_lease(ctx, task_artifacts, PortraitGlareParams)
                return result
        installed.append(Operation('portrait.glare', 'gpu', glare))
    if reflection_factory is not None:
        if not callable(reflection_factory):
            raise WorkerConfigurationError('Invalid reflection admission factory')
        reflection_admission = reflection_factory(database, config)
        lock_sha = getattr(reflection_admission, 'lock_sha', None)
        if (not isinstance(lock_sha, str) or len(lock_sha) != 64
                or any(c not in '0123456789abcdef' for c in lock_sha)
                or not callable(getattr(reflection_admission, 'admit', None))
                or not callable(getattr(reflection_admission, 'acquire', None))):
            raise WorkerConfigurationError('Invalid reflection admission installation')
        from photo_worker.operations.image.reflection import execute as execute_reflection
        from photo_worker.contracts.portrait_restoration import ImageReflectionParams
        def reflection(ctx):
            if ctx.lease.execution_profile.get('backend') != config.backend:
                raise OperationFailure('CONFIG_INVALID')
            with TaskArtifacts.create(ctx, artifacts, _V28_ROLES,
                                      defer_v2_index=True) as task_artifacts:
                current_lease(ctx, task_artifacts, ImageReflectionParams)
                result = execute_reflection(ctx, task_artifacts, reflection_admission)
                current_lease(ctx, task_artifacts, ImageReflectionParams)
                return result
        installed.append(Operation('image.reflection', 'gpu', reflection))
    return installed


_V28_ROLES = frozenset({'pixels', 'working_image', 'mask_pixels', 'mask',
                        'report', 'document'})
