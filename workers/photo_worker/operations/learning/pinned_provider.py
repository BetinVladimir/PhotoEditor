"""Pinned V23 holdout provider for aligned color and portrait beauty cases.

This provider recomputes adaptive/local inputs with the pinned person model
and shares production multi-face beauty using frozen source observations.
Other effects requiring additional frozen inputs still refuse. Faces without
current consent/effective-mask evidence
remain not_measurable. It is not registered by the worker until those cases
can also be evaluated by a qualified full renderer.
"""
from __future__ import annotations

from contextlib import ExitStack, contextmanager
from typing import Iterator

import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.color.icc import ICCRegistry
from photo_worker.models.pool import ModelPool
from photo_worker.operations.identity.metrics import measure_pixel_guards
from photo_worker.operations.session.contracts import (
    Calibration, ConsentSnapshot, FaceObservations,
)
from photo_worker.operations.session.describe import _face_observations, _preview
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext

from .contracts import TrainingExample
from .evaluation import RenderedHoldout
from .identity import (confirmed_source_faces, match_after_faces,
                       measure_holdout_identity)
from .person_stats import open_holdout_person_stats
from .skin import open_holdout_skin
from .beauty_render import recover_holdout_landmarks
from .recipe import FrozenHoldoutStyles
from .workspace import open_holdout_workspace, render_holdout_triplet


class PinnedColorHoldoutProvider:
    backend = 'cuda'
    renderer_version = 'v2.2'

    def __init__(self, pool: ModelPool, registry: ICCRegistry, *, denoise_provider=None, depth_provider=None, background_verify_runtime=None):
        if denoise_provider is not None:
            from photo_worker.v2.render_native_base import NativeDenoiseProvider
            if (not isinstance(denoise_provider, NativeDenoiseProvider)
                    or denoise_provider.backend != self.backend
                    or denoise_provider.pool is not pool):
                raise ValueError("HOLDOUT_DENOISE_INSTALLATION_REQUIRED")
        if depth_provider is not None:
            from .depth import HoldoutDepthProvider
            if (not isinstance(depth_provider, HoldoutDepthProvider)
                    or depth_provider.backend != self.backend or depth_provider.pool is not pool):
                raise ValueError('HOLDOUT_DEPTH_INSTALLATION_REQUIRED')
        if background_verify_runtime is not None and not callable(background_verify_runtime):
            raise ValueError('HOLDOUT_BACKGROUND_DECODER_INSTALLATION_REQUIRED')
        self.background_verify_runtime = background_verify_runtime
        self.depth_provider = depth_provider
        self.denoise_provider = denoise_provider
        self.pool = pool
        self.registry = registry
        self.model_lock_sha = pool.lock.lock_id
        self.icc_lock_sha = registry.resolve('work_rec2020_gamma24_v1').sha256

    @contextmanager
    def render(self, ctx: ExecutionContext, artifacts: TaskArtifacts,
               item: TrainingExample, styles: FrozenHoldoutStyles,
               calibration: Calibration) -> Iterator[RenderedHoldout]:
        ctx.check_active()
        if (not calibration.qualified or
                calibration.model_lock_sha != self.model_lock_sha or
                not {'sfd', 'fan2d'} <= {entry.id for entry in self.pool.lock.models}):
            raise ValueError('HOLDOUT_DETECTOR_UNQUALIFIED')
        # Own all comparison arms before artifact/model callbacks. Later stages
        # must render the same styles admitted here, even if caller data changes.
        styles = FrozenHoldoutStyles(styles.baseline.model_copy(deep=True),
            styles.candidate.model_copy(deep=True), styles.approved.model_copy(deep=True))
        from .pipeline_admission import validate_holdout_pipeline
        reviewed_defect = item.render_case.defect_evidence is not None
        reviewed_dust = item.render_case.dust_evidence is not None
        reviewed_detail = item.render_case.detail_evidence is not None
        reviewed_series = item.render_case.series_evidence is not None
        validate_holdout_pipeline(styles, reviewed_defect=reviewed_defect,
                                  reviewed_dust=reviewed_dust,
                                  reviewed_detail=reviewed_detail,
                                  reviewed_series=reviewed_series)
        before = artifacts.contract(item.render_case.faces, FaceObservations).model_copy(deep=True)
        ctx.check_active()
        if before.binding != item.render_case.binding:
            raise ValueError('INVALID_EVALUATION_FACE_BINDING')
        work_profile = self.registry.resolve('work_rec2020_gamma24_v1')
        if work_profile.sha256 != self.icc_lock_sha:
            raise ValueError('INVALID_ICC_LOCK')
        with open_holdout_workspace(artifacts, item.render_case,
                                    work_profile.sha256,
                                    ctx.check_active) as workspace:
            rules = styles.candidate.extensions.region_adjustments
            if (any(style.extensions.region_adjustments != rules
                    for style in (styles.baseline, styles.approved)) or
                    len({rule.region_id for rule in rules}) != len(rules) or
                    set(workspace.source.region_masks) !=
                    {rule.region_id for rule in rules}):
                raise ValueError('HOLDOUT_REGION_BINDING_MISMATCH')
            region_masks = {}
            for rule in rules:
                mask, pixels = workspace.source.region_masks[rule.region_id]
                if mask.selector != rule.selector or mask.confidence < .75:
                    raise ValueError('HOLDOUT_REGION_BINDING_MISMATCH')
                region_masks[rule.region_id] = pixels
            needs_person = any(style.mode == 'adaptive' or style.local.enabled
                               or (style.extensions.background is not None
                                   and style.extensions.background.mode != 'keep')
                               for style in (styles.baseline, styles.candidate,
                                             styles.approved))
            with ExitStack() as inputs:
                reviewed_source_rgb = None
                if reviewed_defect:
                    from .defect_replay import open_holdout_reviewed_defect
                    reviewed_source_rgb = inputs.enter_context(open_holdout_reviewed_defect(
                        artifacts, item, workspace, styles.baseline,
                        execution_profile=ctx.lease.execution_profile,
                        check_active=ctx.check_active))
                reviewed_dust_replay = None
                if reviewed_dust:
                    from .dust_replay import open_holdout_reviewed_dust
                    reviewed_dust_replay = inputs.enter_context(open_holdout_reviewed_dust(
                        artifacts, item, workspace, styles,
                        execution_profile=ctx.lease.execution_profile,
                        check_active=ctx.check_active))
                reviewed_detail_replay = None
                if reviewed_detail:
                    from .detail_replay import open_holdout_portrait_detail
                    reviewed_detail_replay = inputs.enter_context(
                        open_holdout_portrait_detail(
                            artifacts, item, workspace, styles,
                            training_run_id=ctx.lease.command.run_id,
                            tenant_id=ctx.lease.command.tenant_id,
                            execution_profile=ctx.lease.execution_profile,
                            check_active=ctx.check_active))
                series_correction = None
                if reviewed_series:
                    from .series_replay import open_holdout_series
                    series_correction = inputs.enter_context(open_holdout_series(
                        artifacts, item, workspace, styles,
                        training_run_id=ctx.lease.command.run_id,
                        tenant_id=ctx.lease.command.tenant_id,
                        check_active=ctx.check_active))
                person = (inputs.enter_context(open_holdout_person_stats(
                    workspace.source.rgb, workspace.source.alpha,
                    self.pool, self.backend, self.model_lock_sha,
                    artifacts.scratch, ctx.check_active))
                    if needs_person else None)
                needs_beauty = any(style.beauty.enabled for style in
                                   (styles.baseline, styles.candidate, styles.approved))
                landmarks = (recover_holdout_landmarks(
                    workspace.source.rgb, workspace.source.image, before,
                    self.pool, self.backend, self.model_lock_sha, ctx.check_active,
                    alpha=workspace.source.alpha)
                    if needs_beauty else ())
                skin = (inputs.enter_context(open_holdout_skin(
                    workspace.source.rgb, before, self.pool, self.backend,
                    self.model_lock_sha, artifacts.scratch, ctx.check_active))
                    if needs_beauty else None)
                from .depth import open_holdout_depth
                needs_depth = any(style.extensions.background is not None and
                    style.extensions.background.mode == 'blur' for style in
                    (styles.baseline, styles.candidate, styles.approved))
                depth = (inputs.enter_context(open_holdout_depth(workspace.source,
                    self.depth_provider, artifacts.scratch, model_lock_sha=self.model_lock_sha,
                    execution_profile=ctx.lease.execution_profile, check_active=ctx.check_active))
                    if needs_depth else None)
                asset_background = None
                asset_pin = item.render_case.background_asset
                asset_style_ref = None
                needs_asset = any(style.extensions.background is not None and
                    style.extensions.background.mode == 'asset' for style in
                    (styles.baseline, styles.candidate, styles.approved))
                if needs_asset:
                    from .contracts import FrozenRecipe
                    from .background_asset import open_holdout_background_asset
                    verify_decoder = self.background_verify_runtime
                    if not callable(verify_decoder) or asset_pin is None:
                        raise ValueError('HOLDOUT_BACKGROUND_DECODER_INSTALLATION_REQUIRED')
                    backgrounds = [style.extensions.background for style in
                        (styles.baseline, styles.candidate, styles.approved)]
                    if any(value != backgrounds[0] for value in backgrounds[1:]):
                        raise ValueError('HOLDOUT_BACKGROUND_BINDING_MISMATCH')
                    recipe = artifacts.contract(item.render_case.base_recipe, FrozenRecipe)
                    asset_style_ref = recipe.base_style
                    pixels = workspace.source.rgb.shape[0]*workspace.source.rgb.shape[1]
                    retained = dict(ram_bytes=128*pixels+1073741824,
                        scratch_bytes=144*pixels+64*16*min(pixels,4194304)+
                        4*pixels*len(region_masks)+536870912+2097152)
                    def verify_background():
                        ctx.check_active()
                        if self.background_verify_runtime is not verify_decoder:
                            raise ValueError('HOLDOUT_BACKGROUND_DECODER_INSTALLATION_CHANGED')
                        if verify_decoder() is not None:
                            raise ValueError('HOLDOUT_BACKGROUND_DECODER_UNQUALIFIED')
                        ctx.check_active()
                        if self.background_verify_runtime is not verify_decoder:
                            raise ValueError('HOLDOUT_BACKGROUND_DECODER_INSTALLATION_CHANGED')
                    verify_background()
                    asset_background = inputs.enter_context(open_holdout_background_asset(
                        artifacts, styles.candidate, asset_style_ref, asset_pin, self.registry,
                        work_icc_sha=self.icc_lock_sha, execution_profile=ctx.lease.execution_profile,
                        retained_resources=retained, check_active=verify_background))
                elif asset_pin is not None:
                    raise ValueError('HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH')
                from .denoise import open_holdout_denoise
                def prepare_source(style):
                    return open_holdout_denoise(
                        artifacts, item.render_case.working_image, workspace.source.rgb,
                        style, self.denoise_provider, model_lock_sha=self.model_lock_sha,
                        execution_profile=ctx.lease.execution_profile,
                        check_active=ctx.check_active)
                render_holdout_triplet(
                    workspace, styles, artifacts=artifacts,
                    observations=before, binding=item.render_case.binding,
                    skin_probability=skin, landmarks=landmarks,
                    prepare_source=prepare_source,
                    background_subject=person.mask if person else None,
                    background_depth=depth,
                    background_asset=asset_background,
                    background_pin=asset_pin,
                    background_style_ref=asset_style_ref,
                    work_icc_sha=self.icc_lock_sha,
                    background_confidence=person.confidence if person else 0.0,
                    person_reliable=person.reliable if person else False,
                    features=person.features if person else None,
                    coverage_counts=person.coverage_counts if person else None,
                    person_mask=(person.mask if person and person.reliable else None),
                    region_masks=region_masks,
                    reviewed_source_rgb=reviewed_source_rgb,
                    reviewed_dust=reviewed_dust_replay,
                    reviewed_detail=reviewed_detail_replay,
                    series_correction=series_correction,
                    series_item_id=(item.render_case.series_evidence and
                        series_correction.item_id),
                    check_active=ctx.check_active)
            source_opacity = (_preview(workspace.source.alpha, 2048)
                              if workspace.source.alpha is not None else None)
            output_opacity = _preview(workspace.alpha, 2048)

            def detect(rgb: np.ndarray, opacity) -> FaceObservations:
                ctx.check_active()
                srgb, _ = work_to_model(_preview(rgb, 2048))
                if opacity is not None:
                    srgb = np.where(opacity[..., None] > 0, srgb, 0).astype(np.float32)
                return _face_observations(
                    srgb, item.render_case.binding, self.pool, self.backend,
                    calibration.min_face_edge_px, ctx.check_active)

            source_detected = detect(workspace.source.rgb, source_opacity)
            after = detect(workspace.candidate, output_opacity)
            status = ('no_person' if not before.faces and
                      not source_detected.faces and not after.faces
                      else 'not_measurable')
            verifier_qualified = False
            if workspace.source.effective_mask is not None:
                guards = measure_pixel_guards(
                    workspace.source.rgb, workspace.candidate,
                    workspace.source.effective_mask, workspace.source.protected,
                    check_active=ctx.check_active)
                if guards.outside_mask_max_error > 0 or guards.protected_max_error > 0:
                    status = 'fail'
                elif (before.faces and item.render_case.consent_snapshot is not None
                      and 'identity512' in {entry.id for entry in self.pool.lock.models}):
                    consents = artifacts.contract(item.render_case.consent_snapshot,
                                                  ConsentSnapshot)
                    ctx.check_active()
                    confirmed = confirmed_source_faces(before, consents)
                    source_by_id = {face.face_id: face for face in source_detected.faces}
                    source_reproduced = (len(source_by_id) == len(before.faces) and
                        all(face.face_id in source_by_id and
                            face.box == source_by_id[face.face_id].box
                            for face in before.faces))
                    mapping = (match_after_faces(before, after, confirmed)
                               if confirmed is not None and source_reproduced else None)
                    if mapping is not None:
                        srgb, _ = work_to_model(_preview(workspace.candidate, 2048))
                        srgb = np.where(output_opacity[..., None] > 0, srgb, 0).astype(np.float32)
                        verified_after = _face_observations(
                            srgb, item.render_case.binding, self.pool, self.backend,
                            calibration.min_face_edge_px, ctx.check_active,
                            confirmed=mapping)
                        if ([(face.face_id, face.box) for face in verified_after.faces] ==
                                [(face.face_id, face.box) for face in after.faces]):
                            status = measure_holdout_identity(
                                before=before, after=verified_after,
                                source_binding=item.render_case.binding,
                                candidate_binding=item.render_case.binding,
                                source_rgb=workspace.source.rgb,
                                candidate_rgb=workspace.candidate,
                                effective_mask=workspace.source.effective_mask,
                                protected=workspace.source.protected,
                                calibration=calibration, detector_qualified=True,
                                detector_succeeded=True, consent_valid=True,
                                aligned_canvas=True, check_active=ctx.check_active)
                            verifier_qualified = True
            ctx.check_active()
            yield RenderedHoldout(
                event_id=item.event_id, binding=item.render_case.binding,
                renderer_version=self.renderer_version,
                icc_lock_sha=self.icc_lock_sha,
                model_lock_sha=self.model_lock_sha,
                baseline_rgb=workspace.baseline,
                candidate_rgb=workspace.candidate,
                approved_rgb=workspace.approved,
                alpha=workspace.alpha,
                protected=workspace.source.protected,
                detector_qualified=True, detector_succeeded=True,
                detected_faces=len(after.faces), verifier_qualified=verifier_qualified,
                identity_status=status)
            ctx.check_active()
