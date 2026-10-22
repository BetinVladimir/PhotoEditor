"""Shared lower pixel/coverage computation; no SQL result or receipt authority.

Normal effects sealing supplies already-admitted typed inputs. Operator
measurement supplies only its actual measured task/source/output/report chain.
This module cannot issue EditorExecutedBaseReceipt or qualification.
"""
from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.contracts.quality import QualityReport
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.optical.contracts import ImageReport, ImageResult
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.render_recipe import FrozenRenderRecipe


@dataclass(frozen=True)
class ComputedCoverageStep:
    task_id: object
    operation: str
    source: object
    output: object
    report: object | None

@dataclass(frozen=True)
class ComputedEffectsInputs:
    original_recipe: object
    original_source: object
    processed_source: object
    quality: object
    executed_style: object
    generative_requested: bool
    generated: object | None
    inherited_review: bool
    coverage_trace: tuple
    def __post_init__(self):
        from uuid import UUID
        from photo_worker.contracts.base import ArtifactRef
        from photo_worker.operations.session.contracts import VersionBinding
        if (any(type(v) is not ArtifactRef for v in (self.original_recipe,self.quality,self.executed_style))
                or any(type(v) is not VersionBinding for v in (self.original_source,self.processed_source))
                or self.original_source.asset_version_id!=self.processed_source.asset_version_id
                or type(self.generative_requested) is not bool or type(self.inherited_review) is not bool
                or not isinstance(self.coverage_trace,tuple) or len(self.coverage_trace)>128
                or self.generated is not None and type(self.generated) is not MaskRef):
            raise ValueError('COMPUTED_EFFECTS_BINDING_INVALID')
        seen=set()
        for step in self.coverage_trace:
            if (type(step) is not ComputedCoverageStep or type(step.task_id) is not UUID or not step.task_id.int
                    or step.task_id in seen or not isinstance(step.operation,str) or not 1<=len(step.operation)<=80
                    or any(type(v) is not ArtifactRef for v in (step.source,step.output))
                    or step.report is not None and type(step.report) is not ArtifactRef):
                raise ValueError('COMPUTED_EFFECTS_TRACE_INVALID')
            seen.add(step.task_id)

@dataclass(frozen=True)
class PreparedEffectsSeal:
    image: WorkingImage
    changed_path: object
    reservation: object
    report: ImageReport
    generated: MaskRef | None
    generated_path: object | None = None
    generated_reservation: object | None = None


@contextmanager
def prepare_computed_effects(artifacts, params, profile, check_active=lambda: None):
    if type(params) is not ComputedEffectsInputs:raise ValueError("COMPUTED_EFFECTS_INPUTS_REQUIRED")
    estimate = profile.get('resource_estimate', {})
    if profile.get('backend') != 'cpu': raise ValueError('EDITOR_EFFECTS_SEAL_PROFILE')
    def owned(ref, model):
        check_active(); value = model.model_validate_json(canonical_bytes(artifacts.contract(ref, model))); check_active(); return value
    origin = owned(params.original_recipe, FrozenRenderRecipe)
    if origin.recipe.source != params.original_source: raise ValueError('EDITOR_EFFECTS_SEAL_ORIGIN')
    image = owned(params.processed_source.source, WorkingImage)
    if image.dtype != 'float32' or canonical_hash(image.canvas) != params.processed_source.canvas_hash:
        raise ValueError('EDITOR_EFFECTS_SEAL_SOURCE')
    original = owned(params.original_source.source, WorkingImage)
    if (canonical_hash(original.canvas) != params.original_source.canvas_hash
            or image.canvas.source_sha256 != original.canvas.source_sha256 or image.color != original.color):
        raise ValueError('EDITOR_EFFECTS_SEAL_SOURCE_ORIGIN')
    owned(params.executed_style, StyleV2)
    quality = owned(params.quality, QualityReport)
    if (quality.target != params.processed_source.source or quality.target_kind != 'working'
            or quality.stage != 'master' or quality.threshold_profile_id != 'master_v1'
            or quality.status not in ('pass', 'review') or not quality.technical.finite):
        raise ValueError('EDITOR_EFFECTS_SEAL_QUALITY')
    h, w = image.canvas.height, image.canvas.width; pixels = h*w
    max_pixels = pixels
    for entry in params.coverage_trace:
        for reference in (entry.source,entry.output):
            metadata=owned(reference,WorkingImage)
            max_pixels=max(max_pixels,metadata.canvas.height*metadata.canvas.width)
    required = {'pixel_count': max_pixels, 'ram_bytes': 32*max_pixels+(64 << 20),
        'scratch_bytes': 48*max_pixels+(64 << 20), 'output_bytes': 8*pixels+(6 << 20), 'vram_bytes': 0}
    if max_pixels > 200000000 or any(type(estimate.get(key)) is not int or estimate[key] < bound for key, bound in required.items()):
        raise ValueError('EDITOR_EFFECTS_SEAL_RESOURCE')
    generated = params.generated
    if not params.coverage_trace and generated is not None and (generated.canvas != image.canvas or generated.transform_chain != image.transform_chain):
        raise ValueError('EDITOR_EFFECTS_SEAL_GENERATED_GEOMETRY')
    with ExitStack() as stack:
        generated_path=None;generated_owner=None
        if params.coverage_trace:
            from .coverage_trace import prepare_coverage_trace
            generated_path,generated_owner,coverage_image=stack.enter_context(prepare_coverage_trace(artifacts,params,check_active))
            if coverage_image!=image:raise ValueError('EDITOR_EFFECTS_SEAL_COVERAGE_TARGET')
            # A trace carries its initial mask; the final published mask is
            # constructed only after staging the computed final-geometry array.
            generated=None
        local = stack.enter_context(artifacts.input(image.pixels, suffix='.npy'))
        rgb = stack.enter_context(read_array(local.path, (h,w,3), 'float32', check_active))
        if image.alpha is not None:
            local_alpha = stack.enter_context(artifacts.input(image.alpha, suffix='.npy'))
            stack.enter_context(read_alpha_array(local_alpha.path, (h,w), check_active))
        if generated_path is not None:
            coverage=stack.enter_context(read_array(generated_path,(h,w),'float32',check_active,unit_interval=True))
        elif generated is not None:
            local_generated = stack.enter_context(artifacts.input(generated.artifact, suffix='.npy'))
            coverage = stack.enter_context(read_array(local_generated.path, (h,w), 'float32', check_active, unit_interval=True))
        else: coverage = None
        clipped = 0
        for y in range(0,h,256):
            for x in range(0,w,256):
                check_active(); sl = np.s_[y:y+256,x:x+256]; part = rgb[sl]
                if not np.isfinite(part).all(): raise ValueError('EDITOR_EFFECTS_SEAL_NONFINITE')
                clipped += int(np.count_nonzero(np.any((part < 0) | (part > 1), axis=2)))
                if coverage is not None and not np.all((coverage[sl] == 0) | (coverage[sl] == 1)):
                    raise ValueError('EDITOR_EFFECTS_SEAL_GENERATED_BINARY')
        review = quality.status == 'review' or params.inherited_review or params.generative_requested or clipped > 0
        report = ImageReport(schema_version=1, status='review' if review else 'pass', operation='editor.effects_seal',
            skipped_count=0, reason='EDITOR_EXECUTED_BASE_REVIEW' if review else None,
            # Identity operation: the input artifact is also the output artifact.
            # Outside-change error is exactly zero by unchanged reference/bytes,
            # not a guessed metric about original-to-processed differences.
            outside_mask_max_error=0., clipping_fraction=clipped/pixels,
            identity=None, transform=None, sources=[params.processed_source], registrations=[])
        with artifacts.scratch.reserve(4*pixels+8192) as reservation:
            path = reservation.path('editor-effects-identity-change', '.npy')
            changed = np.lib.format.open_memmap(path, mode='w+', dtype='<f4', shape=(h,w))
            try:
                for y in range(0,h,256): check_active(); changed[y:y+256] = 0
                changed.flush(); reservation.validate_file(path)
                if canonical_bytes(owned(params.quality, QualityReport)) != canonical_bytes(quality):
                    raise ValueError('EDITOR_EFFECTS_SEAL_QUALITY_CHANGED')
                if canonical_bytes(owned(params.processed_source.source, WorkingImage)) != canonical_bytes(image):
                    raise ValueError('EDITOR_EFFECTS_SEAL_SOURCE_CHANGED')
                check_active(); yield PreparedEffectsSeal(image, path, reservation, report, generated, generated_path, generated_owner); check_active()
            finally: changed._mmap.close()

