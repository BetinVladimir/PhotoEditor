"""V23 identity comparison on an exact shared canvas.

Geometric comparisons require the source operation's pinned sampler and are
rejected until that transform is available in the task input closure.
"""
from __future__ import annotations

from contextlib import ExitStack

import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.operations.session.contracts import Calibration, ConsentSnapshot
from photo_worker.operations.session.describe import (_confirmed_faces,
                                                     _face_observations, _preview)
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import (ExecutionContext, Operation,
                                           OperationFailure, OperationResult)

from .contracts import IdentityCompareParams, IdentityFaceMetric, IdentityReport
from .metrics import compare_identity_observations, measure_pixel_guards


REQUIRED_MODELS = frozenset({'sfd', 'fan2d', 'identity512'})


def _changed_pixels(before: np.ndarray, after: np.ndarray,
                    before_alpha: np.ndarray | None,
                    after_alpha: np.ndarray | None,
                    target: np.memmap, check_active, tile: int = 512) -> None:
    height, width = before.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active()
            region = np.s_[y:y + tile, x:x + tile]
            changed = np.any(before[region].view(np.uint32) !=
                             after[region].view(np.uint32), axis=2)
            left = before_alpha[region] if before_alpha is not None else 1.
            right = after_alpha[region] if after_alpha is not None else 1.
            if before_alpha is not None and before_alpha.dtype==np.uint16:left=left.astype(np.float32)/np.float32(65535)
            if after_alpha is not None and after_alpha.dtype==np.uint16:right=right.astype(np.float32)/np.float32(65535)
            target[region] = np.asarray(changed | (left != right), np.float32)
    target.flush()


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts,
            pool: ModelPool, backend: str) -> OperationResult:
    profile = ctx.lease.execution_profile
    if profile.get('renderer_version') != 'v2.2' or profile.get('backend') != backend:
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(IdentityCompareParams)
        if params.model_lock_sha != pool.lock.lock_id:
            raise ModelError('MODEL_LOCK_MISMATCH')
        if not REQUIRED_MODELS <= {item.id for item in pool.lock.models}:
            raise ModelError('MODEL_UNAVAILABLE')
        calibration = artifacts.contract(params.calibration, Calibration)
        consents = artifacts.contract(params.consents, ConsentSnapshot)
        if not calibration.qualified or calibration.model_lock_sha != params.model_lock_sha:
            raise ModelError('MODEL_UNQUALIFIED')
        before = artifacts.contract(params.before.source, WorkingImage)
        after = artifacts.contract(params.after.source, WorkingImage)
        effective = artifacts.contract(params.effective_mask, MaskRef)
        protected = artifacts.contract(params.protected, MaskRef)
        if (canonical_hash(before.canvas) != params.before.canvas_hash or
                canonical_hash(after.canvas) != params.after.canvas_hash or
                before.dtype != 'float32' or after.dtype != 'float32' or
                before.color != after.color or
                before.color.profile_id != 'work_rec2020_gamma24_v1' or
                before.canvas != after.canvas or
                before.transform_chain != after.transform_chain):
            raise ValueError('IDENTITY_CANVAS_UNSUPPORTED')
        if (effective.canvas != after.canvas or
                effective.transform_chain != after.transform_chain or
                effective.protected or protected.canvas != after.canvas or
                protected.transform_chain != after.transform_chain or
                not protected.protected):
            raise ValueError('IDENTITY_MASK_BINDING_INVALID')
        selected = {params.before.asset_version_id, params.after.asset_version_id}
        confirmed = _confirmed_faces(consents, selected)
        height, width = after.canvas.height, after.canvas.width
        if height * width > 200_000_000:
            raise ValueError('IDENTITY_PIXEL_LIMIT')
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc

    ctx.progress(0, 4)
    try:
        with ExitStack() as stack:
            def mapped(ref, shape, dtype='float32', unit=True):
                local = stack.enter_context(artifacts.input(ref, suffix='.npy'))
                return stack.enter_context(read_array(local.path, shape, dtype,
                                                       ctx.check_active,
                                                       unit_interval=unit))
            before_rgb = mapped(before.pixels, (height, width, 3))
            after_rgb = mapped(after.pixels, (height, width, 3))
            def opacity(ref):
                local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
                return stack.enter_context(read_alpha_array(local.path,(height,width),ctx.check_active))
            before_alpha = (opacity(before.alpha)
                            if before.alpha is not None else None)
            after_alpha = (opacity(after.alpha)
                           if after.alpha is not None else None)
            effective_pixels = mapped(effective.artifact, (height, width))
            protected_pixels = mapped(protected.artifact, (height, width))
            guards = measure_pixel_guards(before_rgb, after_rgb,
                                          effective_pixels, protected_pixels,
                                          before_alpha=before_alpha,
                                          after_alpha=after_alpha,
                                          check_active=ctx.check_active)
            ctx.progress(1, 4)
            before_srgb, _ = work_to_model(_preview(before_rgb, 2048))
            after_srgb, _ = work_to_model(_preview(after_rgb, 2048))
            observations_before = _face_observations(
                before_srgb, params.before, pool, backend,
                calibration.min_face_edge_px, ctx.check_active,
                confirmed.get(params.before.asset_version_id))
            observations_after = _face_observations(
                after_srgb, params.after, pool, backend,
                calibration.min_face_edge_px, ctx.check_active,
                confirmed.get(params.after.asset_version_id))
            comparison = compare_identity_observations(
                observations_before, observations_after, calibration,
                detector_qualified=True, consent_valid=True,
                aligned_canvas=True, canvas_size=(width, height),
                outside_mask_max_error=guards.outside_mask_max_error,
                protected_max_error=guards.protected_max_error)
            ctx.progress(2, 4)
            with artifacts.scratch.reserve(height * width * 4 + 8192) as reservation:
                path = reservation.path('identity-changed', '.npy')
                changed = np.lib.format.open_memmap(
                    path, mode='w+', dtype='<f4', shape=(height, width))
                try:
                    _changed_pixels(before_rgb, after_rgb, before_alpha,
                                    after_alpha, changed, ctx.check_active)
                    reservation.validate()
                finally:
                    changed._mmap.close()
                pixel_ref = artifacts.stage(path, reservation, 'mask_pixels',
                                            'application/x-npy')
                pixel_bytes = path.stat().st_size
        mask = MaskRef(artifact=pixel_ref, canvas=after.canvas,
                       transform_chain=after.transform_chain,
                       object_id=None, selector='identity_changed',
                       confidence=1, model_lock_id=params.model_lock_sha,
                       protected=False)
        mask_ref = artifacts.stage_contract(mask, 'mask')
        ctx.progress(3, 4)
        report = IdentityReport(schema_version=1, status=comparison.status,
            faces=[IdentityFaceMetric(face_id=item.face_id,
                                      similarity=item.similarity,
                                      landmark_drift=item.landmark_drift)
                   for item in comparison.faces],
            changed_mask=mask_ref, generated_mask=None,
            outside_mask_max_error=guards.outside_mask_max_error,
            calibration=params.calibration)
        report_ref = artifacts.stage_contract(report, 'document')
        ctx.progress(4, 4)
        return OperationResult(outputs=[pixel_ref, mask_ref, report_ref],
                               review_required=report.status != 'pass',
                               bytes_written=pixel_bytes + len(canonical_bytes(mask))
                               + len(canonical_bytes(report)))
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc


def identity_compare_operations(config: ArtifactConfig, pool: ModelPool,
                                backend: str) -> list[Operation]:
    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config,
                                  frozenset({'document', 'mask', 'mask_pixels'})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation('identity.compare', 'gpu', run)]
