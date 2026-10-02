"""Pinned V23 holdout provider for complete color-only, no-person cases.

This provider recomputes adaptive/local inputs with the pinned person model
and rejects effect styles that require additional frozen inputs. Faces without
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
from .recipe import FrozenHoldoutStyles
from .workspace import open_holdout_workspace, render_holdout_triplet


class PinnedColorHoldoutProvider:
    backend = 'cuda'
    renderer_version = 'v2.2'

    def __init__(self, pool: ModelPool, registry: ICCRegistry):
        self.pool = pool
        self.registry = registry
        self.model_lock_sha = pool.lock.lock_id
        self.icc_lock_sha = registry.resolve('work_rec2020_gamma24_v1').sha256

    @contextmanager
    def render(self, ctx: ExecutionContext, artifacts: TaskArtifacts,
               item: TrainingExample, styles: FrozenHoldoutStyles,
               calibration: Calibration) -> Iterator[RenderedHoldout]:
        if (not calibration.qualified or
                calibration.model_lock_sha != self.model_lock_sha or
                not {'sfd', 'fan2d'} <= {entry.id for entry in self.pool.lock.models}):
            raise ValueError('HOLDOUT_DETECTOR_UNQUALIFIED')
        before = artifacts.contract(item.render_case.faces, FaceObservations)
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
                               for style in (styles.baseline, styles.candidate,
                                             styles.approved))
            with ExitStack() as inputs:
                person = (inputs.enter_context(open_holdout_person_stats(
                    workspace.source.rgb, workspace.source.alpha,
                    self.pool, self.backend, self.model_lock_sha,
                    artifacts.scratch, ctx.check_active))
                    if needs_person else None)
                render_holdout_triplet(
                    workspace, styles,
                    person_reliable=person.reliable if person else False,
                    features=person.features if person else None,
                    coverage_counts=person.coverage_counts if person else None,
                    person_mask=(person.mask if person and person.reliable else None),
                    region_masks=region_masks,
                    check_active=ctx.check_active)
            opacity = (_preview(workspace.source.alpha, 2048)
                       if workspace.source.alpha is not None else None)

            def detect(rgb: np.ndarray) -> FaceObservations:
                srgb, _ = work_to_model(_preview(rgb, 2048))
                if opacity is not None:
                    srgb = np.where(opacity[..., None] > 0, srgb, 0).astype(np.float32)
                return _face_observations(
                    srgb, item.render_case.binding, self.pool, self.backend,
                    calibration.min_face_edge_px, ctx.check_active)

            source_detected = detect(workspace.source.rgb)
            after = detect(workspace.candidate)
            status = ('no_person' if not before.faces and
                      not source_detected.faces and not after.faces
                      else 'not_measurable')
            verifier_qualified = False
            if workspace.source.effective_mask is not None:
                guards = measure_pixel_guards(
                    workspace.source.rgb, workspace.candidate,
                    workspace.source.effective_mask, workspace.source.protected)
                if guards.outside_mask_max_error > 0 or guards.protected_max_error > 0:
                    status = 'fail'
                elif (before.faces and item.render_case.consent_snapshot is not None
                      and 'identity512' in {entry.id for entry in self.pool.lock.models}):
                    consents = artifacts.contract(item.render_case.consent_snapshot,
                                                  ConsentSnapshot)
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
                        if opacity is not None:
                            srgb = np.where(opacity[..., None] > 0, srgb, 0).astype(np.float32)
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
                                aligned_canvas=True)
                            verifier_qualified = True
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
