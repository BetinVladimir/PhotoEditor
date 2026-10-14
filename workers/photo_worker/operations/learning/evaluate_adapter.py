"""Fenced learning.evaluate task adapter over measured holdout renders.

The operation constructor requires an explicit qualified render provider. The
worker registry does not create one from model-shape compatibility alone.
"""
from __future__ import annotations

from contextlib import contextmanager
from typing import ContextManager, Iterator, Protocol

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.style import StyleV2
from photo_worker.models.install import ModelError
from photo_worker.operations.session.contracts import (Calibration,
    DescriptorShard, FaceObservations)
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import (ExecutionContext, Operation,
                                           OperationFailure, OperationResult)

from .contracts import (CandidateProfile, LearningEvaluateParams,
                        FrozenRecipe, TrainingExample, TrainingManifest)
from .evaluation import RenderedHoldout, evaluate_holdout
from .descriptor import validate_training_descriptor
from .recipe import (FrozenHoldoutStyles, materialize_holdout_styles,
                     validate_frozen_render_case)


class QualifiedRenderProvider(Protocol):
    """Pinned renderer plus qualified detector/verifier owned by the deployment."""
    renderer_version: str
    model_lock_sha: str
    icc_lock_sha: str
    backend: str

    def render(self, ctx: ExecutionContext, artifacts: TaskArtifacts,
               item: TrainingExample,
               styles: FrozenHoldoutStyles,
               calibration: Calibration) -> ContextManager[RenderedHoldout]: ...


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts,
            provider: QualifiedRenderProvider) -> OperationResult:
    profile = ctx.lease.execution_profile
    if (not isinstance(profile, dict) or profile.get('renderer_version') != 'v2.2' or
            profile.get('backend') != 'cuda' or
            provider.backend != 'cuda' or
            provider.renderer_version != 'v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    try:
        ctx.check_active()
        params = artifacts.params(LearningEvaluateParams).model_copy(deep=True)
        ctx.check_active()
        if (params.model_lock_sha != provider.model_lock_sha or
                params.icc_lock_sha != provider.icc_lock_sha):
            raise ValueError('EVALUATION_LOCK_MISMATCH')
        manifest = artifacts.contract(params.manifest, TrainingManifest).model_copy(deep=True)
        ctx.check_active()
        baseline = artifacts.contract(params.baseline, CandidateProfile).model_copy(deep=True)
        ctx.check_active()
        candidate = artifacts.contract(params.candidate, CandidateProfile).model_copy(deep=True)
        ctx.check_active()
        calibration = artifacts.contract(params.calibration, Calibration).model_copy(deep=True)
        ctx.check_active()
        holdout = sum(item.split == 'holdout' for item in manifest.examples)
        if holdout < 10:
            raise ValueError('INVALID_HOLDOUT_SPLIT')
        completed = 0

        @contextmanager
        def render(item: TrainingExample, base: CandidateProfile,
                   fitted: CandidateProfile) -> Iterator[RenderedHoldout]:
            nonlocal completed
            ctx.check_active()
            render_case = item.render_case
            descriptor = artifacts.contract(render_case.descriptor_shard, DescriptorShard)
            ctx.check_active()
            validate_training_descriptor(item, descriptor)
            base_recipe = artifacts.contract(render_case.base_recipe, FrozenRecipe)
            ctx.check_active()
            approved_recipe = artifacts.contract(render_case.approved_recipe,
                                                 FrozenRecipe)
            ctx.check_active()
            validate_frozen_render_case(render_case, base_recipe,
                                        approved_recipe, manifest)
            base_style: StyleV2 = artifacts.style(base_recipe.base_style)
            ctx.check_active()
            styles = materialize_holdout_styles(
                item, manifest, base_recipe, approved_recipe, base_style,
                base, fitted)
            with provider.render(ctx, artifacts, item.model_copy(deep=True),
                                 styles, calibration.model_copy(deep=True)) as evidence:
                ctx.check_active()
                yield evidence
                ctx.check_active()
            completed += 1
            ctx.progress(completed, holdout + 1)

        def source_observations(item: TrainingExample) -> FaceObservations:
            ctx.check_active()
            observations = artifacts.contract(item.render_case.faces, FaceObservations)
            ctx.check_active()
            return observations

        ctx.progress(0, holdout + 1)
        result = evaluate_holdout(params, manifest, baseline, candidate,
                                  calibration, render, source_observations,
                                  artifacts.scratch.directory,
                                  check_active=ctx.check_active,
                                  scratch_space=artifacts.scratch)
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active()
    ref = artifacts.stage_contract(result, 'document')
    ctx.check_active()
    ctx.progress(holdout + 1, holdout + 1)
    return OperationResult(outputs=[ref], bytes_written=len(canonical_bytes(result)))


def learning_evaluate_operations(config: ArtifactConfig,
                                 provider: QualifiedRenderProvider) -> list[Operation]:
    if provider is None:
        raise ValueError('Qualified render provider required')

    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config, frozenset({'document'})) as artifacts:
            return execute(ctx, artifacts, provider)

    return [Operation('learning.evaluate', 'gpu', run)]
