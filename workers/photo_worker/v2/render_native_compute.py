"""Actual native compute assembly without deployment/admission claims."""
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
from .render_scalars import ResolvedRenderOverride
from .render_inputs import read_render_document
from .render_runtime import load_render_runtime_lock


from dataclasses import dataclass,asdict
@dataclass(frozen=True)
class OperatorRenderExecutionIdentity:
    model_lock_sha:str
    runtime_lock_sha:str
    backend:str
    device_profile_sha:str
    dataset_sha:str
    calibration_sha:str
    def __post_init__(self):
        if self.backend!='cpu' or any(not isinstance(v,str) or re.fullmatch('[0-9a-f]{64}',v) is None for k,v in asdict(self).items() if k!='backend'):
            raise ValueError('OPERATOR_RENDER_EXECUTION_IDENTITY_INVALID')

@dataclass(frozen=True)
class OperatorComputedBase:
    """Measured source/evidence, never authenticated SQL receipt authority.

    The operator validates actual complete Apply outputs/witnesses before this
    construction. Normal worker/replay retains its scoped receipt validation.
    """
    processed_source: object
    evidence: object
    modules: frozenset
    inherited_review: bool
    source_result: object
    def __post_init__(self):
        from photo_worker.operations.session.contracts import VersionBinding
        from photo_worker.contracts.base import ArtifactRef
        from photo_worker.operations.optical.contracts import ImageResult
        if (type(self.processed_source) is not VersionBinding or type(self.evidence) is not ArtifactRef
                or not isinstance(self.modules,frozenset) or not self.modules
                or type(self.inherited_review) is not bool
                or type(self.source_result) is not ImageResult or self.source_result.working_image!=self.processed_source.source):
            raise ValueError('OPERATOR_COMPUTED_BASE_INVALID')

class NativeRenderComputeCore:
    """Lower production compute only: no worker(), replay() or SQL admission.

    Identity pins describe the bytes being measured, not an already passed
    qualification. The external operator policy must authorize these inputs.
    """
    def __init__(self,*,runtime_path,execution_identity,icc_lock_sha,provider_peak,
                 max_input_bytes,max_mask_bytes,max_tile_bytes,max_document_bytes):
        if (not isinstance(runtime_path,Path) or not runtime_path.is_absolute()
                or type(execution_identity) is not OperatorRenderExecutionIdentity
                or re.fullmatch('[0-9a-f]{64}',icc_lock_sha or '') is None
                or any(type(v) is not int or not 1<=v<=2**53-1 for v in (max_input_bytes,max_mask_bytes,max_tile_bytes,max_document_bytes))):
            raise ValueError('OPERATOR_RENDER_CORE_INVALID')
        from .render_resources import render_resources
        render_resources(1,1,mask_refs=0,mask_nodes=0,provider_peak=provider_peak)
        self.runtime_path=runtime_path;self.pin=execution_identity;self.icc=icc_lock_sha;self.backend='cpu'
        self._peak=tuple(provider_peak.items());self._peak_for=None
        self.input_bytes,self.mask_bytes,self.tile_bytes=max_input_bytes,max_mask_bytes,max_tile_bytes
        self.document_bytes=max_document_bytes
    def verify(self,pin=None):
        if pin is not None and pin!=self.pin:raise ValueError('OPERATOR_RENDER_EXECUTION_IDENTITY_CHANGED')
        load_render_runtime_lock(self.runtime_path,self.pin.runtime_lock_sha)
    def provider_peak_for(self,inputs,profile):
        return dict(self._peak) if self._peak_for is None else self._peak_for(inputs.source.canvas.width,inputs.source.canvas.height,profile)

    def operator_assembly(self, computed_base):
        # Local measurement is neither a sealed receipt nor SQL admission.
        if type(self) is not NativeRenderComputeCore or type(computed_base) is not OperatorComputedBase:
            raise ValueError('OPERATOR_COMPUTED_BASE_REQUIRED')
        return self.assembly(parents=(computed_base.source_result,),_operator_base=computed_base)

    def assembly(self, *, parents=(), executed_base=None, executed_modules=None, _operator_base=None):
        if _operator_base is not None and (type(self) is not NativeRenderComputeCore
                or type(_operator_base) is not OperatorComputedBase or executed_base is not None):
            raise ValueError('OPERATOR_COMPUTED_BASE_REQUIRED')
        def layers(artifacts, inputs, profile, active):
            def binding(params, state):
                self.verify(); active()
                # Params may pin a deterministic intermediate, rather than the
                # recipe master. The proposal engine independently compares its
                # physical RGB/alpha with the current reconstructed stage bits.
                image = artifacts.contract(params.image, WorkingImage)
                source = state.source
                if (image.canvas != source.canvas or image.transform_chain != source.transform_chain
                        or image.color != source.color or image.dtype != 'float32'):
                    raise ValueError('RENDER_NATIVE_LAYER_SOURCE_CHANGED')
                for mask in getattr(params, 'protected', ()):
                    if mask.canvas != image.canvas or mask.transform_chain != image.transform_chain:
                        raise ValueError('RENDER_NATIVE_LAYER_MASK_CHANGED')
                active()
            return combine_render_layer_factories(
                PinnedConservativeProviderFactory(runtime_sha256=self.pin.runtime_lock_sha,
                    verify_runtime=self.verify, validate_binding=binding),
                PinnedDetailProviderFactory(runtime_sha256=self.pin.runtime_lock_sha,
                    verify_runtime=self.verify, validate_binding=binding))(artifacts, inputs, profile, active)
        def overrides(artifacts, inputs, profile, active):
            # Whole module replacements contribute no additive scalar delta.
            load_render_overrides(artifacts, inputs, max_document_bytes=self.document_bytes, check_active=active)
            zero = CorrectionVector(ev=0, log_r=0, log_g=0, log_b=0, saturation_delta=0, retouch_delta=0)
            return tuple(ResolvedRenderOverride(ref.model_copy(deep=True), zero.model_copy(deep=True))
                         for ref in inputs.recipe.recipe.overrides)
        def base(artifacts, inputs, profile, active):
            @contextmanager
            def render(state, style, original, masks):
                self.verify(); active()
                if state.recipe.recipe.executed_base_receipt is not None:
                    if _operator_base is not None:
                        if (_operator_base.processed_source != state.recipe.recipe.source
                                or _operator_base.evidence != state.recipe.recipe.executed_base_receipt):
                            raise ValueError('OPERATOR_COMPUTED_BASE_BINDING_CHANGED')
                    elif executed_base is None or executed_base[0].processed_source != state.recipe.recipe.source:
                        raise ValueError('RENDER_EXECUTED_BASE_AUTHORITY_REQUIRED')
                    # Original is the hash-verified processed source. Identity base
                    # retains its generated coverage. Authored layers already ran
                    # in the admitted precompose stage and cannot run twice.
                    yield original
                    active()
                    return
                from .render_color import color_provider_peak
                from .render_resources import admit_render
                peak = color_provider_peak(state.source.canvas.width, state.source.canvas.height,
                    style.effective, tile_size=profile['tile_size'])
                admit_render(profile, state.source.canvas.width, state.source.canvas.height,
                    mask_refs=0, mask_nodes=0, provider_peak=peak)
                with prepare_color_base(artifacts, state, style, original, person_reliable=False,
                        read_protected=masks.protection if state.protection is not None else None,
                        tile_size=profile['tile_size'], check_active=active) as pixels:
                    yield pixels
                active()
            return render
        def measure(artifacts, inputs, profile, active):
            def measured(state, prepared):
                self.verify(); active()
                rgb = prepared.render.composition.pixels.rgb
                h, w = state.source.canvas.height, state.source.canvas.width
                count = 0; outside = 0.; tile = profile['tile_size']
                # Reopen the pinned source independently. A changed-mask bit is
                # not itself an oracle for the measured outside-mask error.
                with artifacts.input(state.source.pixels, suffix='.npy') as source:
                    with read_array(source.path, (h, w, 3), 'float32', active) as original:
                        with read_array(prepared.render.composition.changes.path, (h, w), 'float32', active, unit_interval=True) as changed:
                            for y in range(0, h, tile):
                                for x in range(0, w, tile):
                                    active(); sl = np.s_[y:y+tile, x:x+tile]; part = rgb[sl]
                                    if not np.isfinite(part).all(): raise ValueError('RENDER_NATIVE_NONFINITE')
                                    count += int(np.count_nonzero(np.any((part < 0) | (part > 1), axis=2)))
                                    unchanged = changed[sl] == 0
                                    if np.any(unchanged):
                                        outside = max(outside, float(np.max(np.abs(part[unchanged].astype(np.float64)-original[sl][unchanged]))))
                fraction = count / (h*w)
                review = (fraction > 0 or prepared.style.effective.quality.status == 'needs_review'
                    or executed_base is not None and executed_base[2].status == 'review'
                    or _operator_base is not None and _operator_base.inherited_review)
                report = ImageReport(schema_version=1, status='review' if review else 'pass',
                    operation='render.compose', skipped_count=0,
                    reason='RENDER_NATIVE_CLIPPING' if fraction > 0 else 'RENDER_STYLE_REVIEW' if review else None,
                    outside_mask_max_error=outside, clipping_fraction=fraction, identity=None,
                    transform=None, sources=[state.recipe.recipe.source], registrations=[])
                active(); return MeasuredAuthoredRender(report, tuple(parent.model_copy(deep=True) for parent in parents))
            return measured
        return RenderProviderAssembly(base_factory=base, layer_factory=layers,
            override_factory=overrides, measure_factory=measure,
            preparation=dict(supported_modules=(_operator_base.modules if _operator_base is not None else executed_modules if executed_base is not None else frozenset({'color', 'strength'})),
                max_params_document_bytes=self.document_bytes,
                max_override_document_bytes=self.document_bytes,
                max_input_bytes=self.input_bytes, max_mask_bytes=self.mask_bytes,
                max_tile_bytes=self.tile_bytes))

