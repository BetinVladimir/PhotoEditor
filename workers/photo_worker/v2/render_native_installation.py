"""Trusted in-process native installation shared by worker and adopter.

No environment-selected imports, archived callback names or inferred model
qualification. Deployment supplies the measured complete qualification pin and
its live verifier; task/adopter SQL admission independently checks that pin.
"""
from contextlib import contextmanager
from pathlib import Path
from uuid import uuid4
import re
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.scratch import ScratchSpace
from .render_color import prepare_color_base
from .render_conservative import PinnedConservativeProviderFactory
from .render_detail import PinnedDetailProviderFactory
from .render_execute import MeasuredAuthoredRender
from .render_overrides import load_render_overrides
from .render_providers import RenderProviderAssembly, combine_render_layer_factories
from .render_qualification_pin import RenderQualificationPin
from .render_scalars import ResolvedRenderOverride
from .render_task import ProfiledTaskRenderer, SQLRenderTaskAdmission
from .render_inputs import read_render_document
from .render_runtime import load_render_runtime_lock


from .render_native_compute import NativeRenderComputeCore

class NativeRenderInstallation(NativeRenderComputeCore):
    """Concrete global color + native layer assembly, with explicit coverage.

    Model-dependent/base geometry modules remain unavailable until their real
    evidence/provider installation is supplied. No layer or module is omitted.
    The same assembly and engine identity is used for independent adoption.
    """
    def __init__(self, *, runtime_path, qualification_pin, icc_lock_sha,
                 verify_qualification, provider_peak, max_input_bytes,
                 max_mask_bytes, max_tile_bytes, max_document_bytes=128 << 20, provider_peak_for=None):
        if (not isinstance(runtime_path, Path) or not runtime_path.is_absolute()
                or not isinstance(qualification_pin, RenderQualificationPin)
                or not isinstance(icc_lock_sha, str) or re.fullmatch('[0-9a-f]{64}', icc_lock_sha) is None
                or not callable(verify_qualification)
                or any(type(value) is not int or not 1 <= value <= 2**53-1 for value in
                       (max_input_bytes, max_mask_bytes, max_tile_bytes, max_document_bytes))):
            raise ValueError('RENDER_NATIVE_INSTALLATION_INVALID')
        from .render_resources import render_resources
        render_resources(1, 1, mask_refs=0, mask_nodes=0, provider_peak=provider_peak)
        self.backend = qualification_pin.backend
        self.pin = qualification_pin
        self.runtime_path = runtime_path
        self.icc = icc_lock_sha
        self._verify = verify_qualification
        self._peak = tuple(provider_peak.items())
        self.input_bytes, self.mask_bytes, self.tile_bytes = max_input_bytes, max_mask_bytes, max_tile_bytes
        self.document_bytes = max_document_bytes
        if provider_peak_for is not None and not callable(provider_peak_for):raise ValueError("RENDER_CALIBRATION_SELECTOR_INVALID")
        self._peak_for = provider_peak_for

    def provider_peak_for(self, inputs, profile):
        return dict(self._peak) if self._peak_for is None else self._peak_for(inputs.source.canvas.width, inputs.source.canvas.height, profile)

    def verify(self, pin=None):
        if pin is not None and pin != self.pin:
            raise ValueError('RENDER_NATIVE_QUALIFICATION_CHANGED')
        load_render_runtime_lock(self.runtime_path, self.pin.runtime_lock_sha)
        # Literal True is mandatory; an empty or permissive missing verifier
        # cannot silently count as independently verified deployment evidence.
        if self._verify(self.pin) is not True:
            raise ValueError('RENDER_NATIVE_QUALIFICATION_UNAVAILABLE')

    def worker(self, database):
        self.verify()
        admission = SQLRenderTaskAdmission(database)
        def providers(ctx, artifacts, inputs):
            raw = read_render_document(artifacts, ctx.lease.command.params, ctx.check_active)
            scoped = admission.inputs(ctx, raw, self.pin)
            parents = tuple(artifacts.contract(ref, ImageResult) for ref in scoped.parent_roots)
            ctx.check_active()
            from .render_executed_base import qualify_scoped_executed_base
            from .render_modules import required_render_modules
            from photo_worker.contracts.style import StyleV2
            executed = qualify_scoped_executed_base(artifacts, inputs, scoped, ctx.check_active)
            modules = None if executed is None else required_render_modules(
                artifacts.contract(executed[0].executed_style, StyleV2))
            return self.assembly(parents=parents, executed_base=executed, executed_modules=modules).worker(ctx, artifacts, inputs)
        return ProfiledTaskRenderer(backend=self.backend, runtime_path=self.runtime_path,
            runtime_sha256=self.pin.runtime_lock_sha, model_lock_sha=self.pin.model_lock_sha,
            icc_lock_sha=self.icc, provider_peak=dict(self._peak), verify_qualification=self.verify,
            load_providers=providers, qualification_pin=self.pin, provider_peak_for=self.provider_peak_for)

    def replay(self, scratch_root, scratch_bytes):
        from .render_profiled_replay import ProfiledRenderReplay
        if (not isinstance(scratch_root, Path) or not scratch_root.is_absolute()
                or type(scratch_bytes) is not int or not 1 <= scratch_bytes <= 2**53-1):
            raise ValueError('RENDER_NATIVE_REPLAY_SCRATCH_INVALID')
        self.verify()
        def make_scratch(scoped):
            bound = min(scratch_bytes, scoped.execution_profile()['resource_estimate']['scratch_bytes'])
            return ScratchSpace(scratch_root, uuid4(), uuid4(), 1, bound)
        def providers(artifacts, inputs, scoped):
            from .render_executed_base import qualify_scoped_executed_base
            from .render_modules import required_render_modules
            from photo_worker.contracts.style import StyleV2
            active = artifacts.check_active
            executed = qualify_scoped_executed_base(artifacts, inputs, scoped, active)
            modules = None if executed is None else required_render_modules(
                artifacts.contract(executed[0].executed_style, StyleV2))
            return self.assembly(executed_base=executed, executed_modules=modules).replay(artifacts, inputs, scoped)
        return ProfiledRenderReplay(backend=self.backend, runtime_path=self.runtime_path,
            runtime_sha256=self.pin.runtime_lock_sha, model_lock_sha=self.pin.model_lock_sha,
            icc_lock_sha=self.icc, provider_peak=dict(self._peak), verify_qualification=self.verify,
            load_providers=providers, make_scratch=make_scratch, qualification_pin=self.pin, provider_peak_for=self.provider_peak_for)
