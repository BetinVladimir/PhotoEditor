"""Explicit qualified learning.evaluate installation; no inferred acceptance."""
from contextlib import contextmanager
from dataclasses import dataclass
import re
from typing import Callable

from photo_worker.runtime.executor import Operation, OperationFailure
from .evaluate_adapter import learning_evaluate_operations


@dataclass(frozen=True)
class LearningEvaluationInstallation:
    provider: object
    verify_runtime: Callable[[], None]
    qualification: object | None = None


class VerifiedLearningProvider:
    def __init__(self, installation):
        if (not isinstance(installation, LearningEvaluationInstallation)
                or not callable(installation.verify_runtime)):
            raise ValueError('LEARNING_INSTALLATION_INVALID')
        self._provider = installation.provider
        self._verify = installation.verify_runtime
        self._qualification = installation.qualification
        self._pins = self._read_pins()
        self.backend, self.renderer_version, self.model_lock_sha, self.icc_lock_sha = self._pins
        self._render = getattr(self._provider, 'render', None)
        if (self.backend != 'cuda' or self.renderer_version != 'v2.2'
                or not all(isinstance(value, str) and re.fullmatch('[0-9a-f]{64}', value)
                           for value in self._pins[2:]) or not callable(self._render)):
            raise ValueError('LEARNING_INSTALLATION_INVALID')
        if self._qualification is not None:
            from .qualification import LearningQualification
            proof = self._qualification
            if (not isinstance(proof, LearningQualification) or
                    proof.model_lock_sha != self.model_lock_sha or
                    proof.icc_lock_sha != self.icc_lock_sha):
                raise ValueError('LEARNING_INSTALLATION_INVALID')
        self.verify()

    def _read_pins(self):
        return tuple(getattr(self._provider, name, None) for name in
                     ('backend', 'renderer_version', 'model_lock_sha', 'icc_lock_sha'))

    def verify(self):
        if self._qualification is not None:
            from datetime import datetime, timezone
            if self._qualification.expires_at <= datetime.now(timezone.utc):
                raise ValueError('LEARNING_QUALIFICATION_EXPIRED')
        if self._read_pins() != self._pins or getattr(self._provider, 'render', None) != self._render:
            raise ValueError('LEARNING_INSTALLATION_CHANGED')
        if self._verify() is not None:
            raise ValueError('LEARNING_RUNTIME_UNQUALIFIED')
        if self._read_pins() != self._pins or getattr(self._provider, 'render', None) != self._render:
            raise ValueError('LEARNING_INSTALLATION_CHANGED')

    def verify_for_task(self, ctx):
        ctx.check_active()
        try:
            self.verify()
            if self._qualification is not None:
                profile = ctx.lease.execution_profile
                estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
                if (not isinstance(estimate, dict) or
                        type(estimate.get('vram_bytes')) is not int or
                        estimate['vram_bytes'] < self._qualification.peak_vram_bytes):
                    raise ValueError('LEARNING_VRAM_BUDGET_UNDERFUNDED')
        except Exception:
            # Cancellation wins over a coincident runtime qualification failure.
            ctx.check_active()
            raise OperationFailure('CONFIG_INVALID') from None
        ctx.check_active()

    @contextmanager
    def render(self, ctx, artifacts, item, styles, calibration):
        ctx.check_active()
        self.verify_for_task(ctx)
        if self._qualification is not None:
            from .qualification import required_learning_effects
            try:
                if not required_learning_effects(styles) <= self._qualification.effects:
                    raise ValueError('LEARNING_EFFECT_UNQUALIFIED')
            except (AttributeError, TypeError, ValueError):
                ctx.check_active()
                raise OperationFailure('CONFIG_INVALID') from None
        ctx.check_active()
        with self._render(ctx, artifacts, item, styles, calibration) as result:
            ctx.check_active()
            self.verify_for_task(ctx)
            ctx.check_active()
            yield result
            ctx.check_active()
            self.verify_for_task(ctx)
        ctx.check_active()
        self.verify_for_task(ctx)
        ctx.check_active()


def installed_learning_evaluate_operations(config, installation):
    provider = VerifiedLearningProvider(installation)
    operations = learning_evaluate_operations(config, provider)
    def bound(operation):
        def execute(ctx):
            ctx.check_active()
            provider.verify_for_task(ctx)
            ctx.check_active()
            result = operation.execute(ctx)
            ctx.check_active()
            provider.verify_for_task(ctx)
            ctx.check_active()
            return result
        return Operation(operation.name, operation.resource_class, execute, operation.version)
    return [bound(operation) for operation in operations]


def pinned_learning_evaluation_installation(pool, registry, *, verify_runtime,
                                            denoise_provider=None, depth_provider=None,
                                            background_verify_runtime=None,
                                            qualification=None):
    """Compose the native holdout provider with deployment-owned qualification.

    Runtime evidence must be authenticated by the supplied verifier. This factory
    neither creates qualification evidence nor enables an operation in a registry.
    Background decoding has its own explicit verifier; omission keeps asset mode
    closed while permitting qualified cases that do not decode background assets.
    """
    if not callable(verify_runtime):
        raise ValueError('LEARNING_INSTALLATION_INVALID')
    from .pinned_provider import PinnedColorHoldoutProvider
    provider = PinnedColorHoldoutProvider(pool, registry,
        denoise_provider=denoise_provider, depth_provider=depth_provider,
        background_verify_runtime=background_verify_runtime)
    model_lock_sha, icc_lock_sha = provider.model_lock_sha, provider.icc_lock_sha
    def check_dependencies():
        if (provider.pool is not pool or provider.registry is not registry
                or getattr(getattr(pool, 'lock', None), 'lock_id', None) != model_lock_sha
                or registry.resolve('work_rec2020_gamma24_v1').sha256 != icc_lock_sha
                or provider.denoise_provider is not denoise_provider
                or provider.depth_provider is not depth_provider
                or provider.background_verify_runtime is not background_verify_runtime):
            raise ValueError('LEARNING_INSTALLATION_CHANGED')
    def verify_installed_runtime():
        check_dependencies()
        result = verify_runtime()
        check_dependencies()
        return result
    installation = LearningEvaluationInstallation(provider, verify_installed_runtime,
                                                 qualification)
    # Validate the exact assembled provider now, before a caller can register it.
    VerifiedLearningProvider(installation)
    return installation
