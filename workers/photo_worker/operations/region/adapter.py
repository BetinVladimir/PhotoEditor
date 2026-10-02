"""Region task orchestration requiring relational admission and complete renderer."""
from dataclasses import dataclass
from pathlib import Path
import re
from typing import ContextManager, Protocol

import numpy as np

from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.style import StyleV2
from photo_worker.operations.learning.contracts import FrozenRecipe
from photo_worker.operations.learning.recipe import materialize_frozen_style
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult
from .contracts import RegionPreviewParams
from .presentation import prepare_region_presentation
from .stage import stage_region_result
from .resources import admit_region_source
from photo_worker.v2.render_modules import required_render_modules as required_region_modules


@dataclass(frozen=True)
class RenderedRegionCanvas:
    rgb: np.ndarray
    alpha: np.ndarray | None


class RegionAdmission(Protocol):
    def admit(self, ctx: ExecutionContext, params: RegionPreviewParams) -> None:
        """Prove task/attempt/fence, immutable plan, closure and live authority."""
        ...


class QualifiedRegionRenderer(Protocol):
    renderer_version: str
    runtime_sha256: str
    model_lock_sha: str
    icc_lock_sha: str
    backend: str
    icc_root: Path
    supported_modules: frozenset[str]

    def verify_runtime(self) -> None:
        """Verify installed pinned operator/model/ICC bytes and qualification."""
        ...

    def admit_resources(self, ctx: ExecutionContext, params: RegionPreviewParams) -> None:
        """Check full render/style/presentation RAM and scratch before input IO."""
        ...

    def render(self, ctx: ExecutionContext, artifacts: TaskArtifacts,
               params: RegionPreviewParams, source: WorkingImage,
               style: StyleV2) -> ContextManager[RenderedRegionCanvas]:
        """Run the complete recipe on its full canvas; never silently skip modules."""
        ...


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts,
            admission: RegionAdmission | None,
            renderer: QualifiedRegionRenderer | None) -> OperationResult:
    if admission is None or renderer is None:
        raise OperationFailure('REGION_RUNTIME_UNAVAILABLE')
    profile = ctx.lease.execution_profile
    runtime_sha = getattr(renderer, 'runtime_sha256', None)
    if (not isinstance(runtime_sha, str) or re.fullmatch(r'[0-9a-f]{64}', runtime_sha) is None
            or profile.get('region_runtime_sha256') != runtime_sha
            or renderer.renderer_version != 'v2.2' or renderer.backend not in ('cpu', 'cuda')
            or profile.get('backend') != renderer.backend
            or profile.get('renderer_version') != renderer.renderer_version
            or profile.get('model_lock_sha') != renderer.model_lock_sha
            or profile.get('icc_lock_sha') != renderer.icc_lock_sha):
        raise OperationFailure('CONFIG_INVALID')
    try:
        ctx.check_active()
        renderer.verify_runtime()
        ctx.check_active()
        params = artifacts.params(RegionPreviewParams)
        ctx.check_active()
        if (params.frozen_recipe.model_lock_sha != renderer.model_lock_sha
                or params.frozen_recipe.icc_lock_sha != renderer.icc_lock_sha):
            raise ValueError('REGION_RUNTIME_PIN_MISMATCH')
        admission.admit(ctx, params)
        ctx.check_active()
        renderer.admit_resources(ctx, params)
        ctx.check_active()
        recipe = artifacts.contract(params.recipe.ref, FrozenRecipe)
        ctx.check_active()
        if recipe != params.frozen_recipe:
            raise ValueError('REGION_RECIPE_MISMATCH')
        with artifacts.stream(params.source.ref) as stream:
            chunks = []
            count = 0
            while count <= params.source.bytes:
                ctx.check_active()
                chunk = stream.read(min(65536, params.source.bytes + 1 - count))
                if not chunk:
                    break
                chunks.append(chunk)
                count += len(chunk)
            source_body = b''.join(chunks)
        if len(source_body) != params.source.bytes:
            raise ValueError('REGION_SOURCE_LENGTH_MISMATCH')
        source = params.validate_source(source_body)
        # Independent minimum check: a provider cannot waive source mapping costs.
        admit_region_source(profile, source.canvas.width, source.canvas.height,
            has_alpha=source.alpha is not None, alpha_mode="normalize_uint16")
        ctx.check_active()
        base = artifacts.style(recipe.base_style)
        ctx.check_active()
        style = materialize_frozen_style(recipe, base, recipe.base_style,
            recipe.source, renderer.icc_lock_sha, renderer.model_lock_sha)
        required = required_region_modules(style)
        if not isinstance(renderer.supported_modules, frozenset) or not required <= renderer.supported_modules:
            raise ValueError('REGION_COMPLETE_RENDERER_REQUIRED')
        ctx.check_active()
        ctx.progress(0, 3)
        ctx.check_active()
        with renderer.render(ctx, artifacts, params, source, style) as full:
            ctx.check_active()
            presentation = prepare_region_presentation(params, source_body,
                full.rgb, full.alpha, renderer.icc_root,
                runtime_icc_lock_sha=renderer.icc_lock_sha, check_active=ctx.check_active)
            ctx.progress(1, 3)
        ctx.check_active()
        result = stage_region_result(artifacts, params, source_body, presentation,
            check_active=ctx.check_active)
        ctx.progress(3, 3)
        return result
    except OperationFailure:
        raise
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc


def region_operations(config: ArtifactConfig, admission: RegionAdmission,
                      renderer: QualifiedRegionRenderer) -> list[Operation]:
    if admission is None or renderer is None:
        raise ValueError('Region admission and qualified complete renderer required')
    if renderer.backend != "cuda":
        raise ValueError("Registered RegionPreview requires qualified CUDA renderer")
    runtime_sha = getattr(renderer, "runtime_sha256", None)
    if not isinstance(runtime_sha, str) or re.fullmatch(r"[0-9a-f]{64}", runtime_sha) is None:
        raise ValueError("Region renderer runtime digest required")
    renderer.verify_runtime()
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'derivative', 'document'})) as artifacts:
            return execute(ctx, artifacts, admission, renderer)
    return [Operation('region.preview', 'gpu' if renderer.backend == 'cuda' else 'media', run)]

