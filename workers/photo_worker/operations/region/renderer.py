"""Attach external Region build identity to an installed complete provider."""
from contextlib import contextmanager
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from types import MethodType
import re

from photo_worker.runtime.executor import OperationFailure
from photo_worker.contracts.base import canonical_bytes
from .adapter import _valid_renderer_metadata
from .runtime_lock import load_region_runtime_lock

_FIELDS = ('renderer_version', 'runtime_sha256', 'model_lock_sha', 'icc_lock_sha',
    'backend', 'icc_root', 'supported_modules')
_CALLBACKS = ('verify_runtime', 'admit_resources', 'render')


def _same_callback(current, pinned):
    # Attribute access creates fresh bound-method objects; compare their actual
    # function and receiver without invoking provider-defined equality.
    if isinstance(current, MethodType) and isinstance(pinned, MethodType):
        return current.__func__ is pinned.__func__ and current.__self__ is pinned.__self__
    return current is pinned


@dataclass(frozen=True, init=False)
class PinnedRegionRenderer:
    """Provider still proves model/ICC/native/backend qualification independently."""
    _provider: object
    _runtime_path: Path
    _pins: tuple
    _callbacks: tuple
    _pipeline: object
    _max_input_bytes: int | None
    _calibration: object
    _device_pin: str | None

    def __init__(self, provider, runtime_path: Path, expected_sha: str, *,
                 render_pipeline=None, max_input_bytes=None, calibration_catalog=None):
        from .calibration import PinnedRegionCalibrationCatalog
        if getattr(provider, 'backend', None) == 'mps' and calibration_catalog is None:
            raise ValueError('MPS Region requires independent device calibration')
        device_pin = getattr(provider,"device_profile_sha",None) if calibration_catalog is not None else None
        if calibration_catalog is not None and (type(calibration_catalog) is not PinnedRegionCalibrationCatalog
                or not isinstance(device_pin,str) or re.fullmatch(r"[0-9a-f]{64}",device_pin) is None):
            raise ValueError("Invalid Region calibration catalog")
        if ((render_pipeline is None) != (max_input_bytes is None)
                or render_pipeline is not None and (not callable(render_pipeline)
                    or type(max_input_bytes) is not int or not 1 <= max_input_bytes <= 2**53-1)):
            raise ValueError("Invalid Region full-canvas pipeline configuration")
        if (not _valid_renderer_metadata(provider)
                or provider.renderer_version != 'v2.2' or provider.backend not in ('cuda', 'mps')
                or provider.runtime_sha256 != expected_sha
                or not isinstance(runtime_path, Path) or not runtime_path.is_absolute()):
            raise ValueError('Invalid pinned Region provider')
        object.__setattr__(self, '_provider', provider)
        object.__setattr__(self, '_runtime_path', runtime_path)
        object.__setattr__(self, '_pins', tuple(getattr(provider, field) for field in _FIELDS))
        object.__setattr__(self, '_callbacks', tuple(getattr(provider, field) for field in _CALLBACKS))
        object.__setattr__(self, '_pipeline', render_pipeline)
        object.__setattr__(self, '_max_input_bytes', max_input_bytes)
        object.__setattr__(self, '_calibration', calibration_catalog)
        object.__setattr__(self, '_device_pin', device_pin)
        self.verify_runtime()

    def __getattr__(self, name):
        if name in _FIELDS:
            return self._pins[_FIELDS.index(name)]
        raise AttributeError(name)

    def _check_provider(self):
        if (self._calibration is not None and getattr(self._provider,"device_profile_sha",None) != self._device_pin
                or tuple(getattr(self._provider, field, None) for field in _FIELDS) != self._pins
                or any(not _same_callback(getattr(self._provider, field, None), pinned)
                    for field, pinned in zip(_CALLBACKS, self._callbacks))):
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')

    def _check_device(self, profile):
        if profile["region_qualification"]["qualification"]["device_profile_sha"] != self._device_pin:
            raise OperationFailure("REGION_CALIBRATION_UNAVAILABLE")

    def verify_runtime(self):
        self._check_provider()
        if self._calibration is not None:
            self._calibration.verify_installed()
        load_region_runtime_lock(self._runtime_path, self.runtime_sha256)
        self._callbacks[0]()
        self._check_provider()
        # Provider verification may take time; keep the build pin valid across it.
        load_region_runtime_lock(self._runtime_path, self.runtime_sha256)

    def admit_resources(self, ctx, params):
        ctx.check_active()
        owned=deepcopy(params)
        before=canonical_bytes(owned)
        original_profile=getattr(getattr(ctx,'lease',None),'execution_profile',None)
        profile=deepcopy(original_profile) if isinstance(original_profile,dict) else None
        self.verify_runtime()
        if self._calibration is not None:
            self._calibration.admit(ctx.lease.execution_profile, owned)
            self._check_device(ctx.lease.execution_profile)
        elif isinstance(getattr(getattr(ctx, "lease", None), "execution_profile", None), dict) and "region_qualification" in ctx.lease.execution_profile:
            raise OperationFailure("REGION_CALIBRATION_UNAVAILABLE")
        self._callbacks[1](ctx, owned)
        if (canonical_bytes(owned)!=before
                or profile is not None and ctx.lease.execution_profile!=profile):
            raise OperationFailure('CONFIG_INVALID')
        self._check_provider()
        ctx.check_active()

    @contextmanager
    def render(self, ctx, artifacts, params, source, style):
        ctx.check_active()
        params,source,style=deepcopy((params,source,style))
        metadata=tuple(canonical_bytes(value) for value in (params,source,style))
        original_profile=getattr(getattr(ctx,'lease',None),'execution_profile',None)
        profile=deepcopy(original_profile) if isinstance(original_profile,dict) else None
        def active():
            ctx.check_active()
            self._check_provider()
            current=tuple(canonical_bytes(value) for value in (params,source,style))
            # The mapped pipeline retains its more specific source/style guards.
            if current[0]!=metadata[0] or self._pipeline is None and current[1:]!=metadata[1:]:
                raise OperationFailure('CONFIG_INVALID')
            if profile is not None and ctx.lease.execution_profile != profile:
                raise OperationFailure('CONFIG_INVALID')
        self.verify_runtime()
        active()
        if self._calibration is not None:
            self._calibration.admit(profile, params, source)
            self._check_device(profile)
        elif isinstance(getattr(getattr(ctx, "lease", None), "execution_profile", None), dict) and "region_qualification" in ctx.lease.execution_profile:
            raise OperationFailure("REGION_CALIBRATION_UNAVAILABLE")
        if self._pipeline is None:
            prepared = self._callbacks[2](ctx, artifacts, params, source, style)
        else:
            from .mapped import prepare_region_full_canvas
            estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
            budget = estimate.get('scratch_bytes') if isinstance(estimate, dict) else None
            if type(budget) is not int or not 1 <= budget <= 2**53-1:
                raise OperationFailure('CONFIG_INVALID')
            prepared = prepare_region_full_canvas(artifacts, source, style,
                lambda metadata, effective, original: self._pipeline(
                    ctx, artifacts, params, metadata, effective, original),
                max_input_bytes=min(self._max_input_bytes, budget), check_active=active)
        with prepared as full:
            active()
            yield full
            active()
        active()
