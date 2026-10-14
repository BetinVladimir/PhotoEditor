"""Qualified identity verdict for a frozen holdout render.

The renderer supplies the pinned detector observations and the actual
effective mask. This module measures pixel guards itself; a caller cannot
claim zero error without supplying the pixels used by the verifier.
"""
from __future__ import annotations

import numpy as np
from uuid import UUID
from typing import Callable

from photo_worker.operations.identity.metrics import (
    compare_identity_observations, measure_pixel_guards,
)
from photo_worker.operations.session.contracts import (
    Calibration, ConsentSnapshot, FaceObservations, VersionBinding,
)


def confirmed_source_faces(before: FaceObservations,
                           consents: ConsentSnapshot) -> dict[UUID, UUID] | None:
    """Require an exact active grant for every frozen source face."""
    grants = {grant.consent_id: grant for grant in consents.grants}
    if len(grants) != len(consents.grants):
        raise ValueError('DUPLICATE_CONSENT_GRANT')
    confirmed: dict[UUID, UUID] = {}
    for face in consents.confirmed_faces:
        grant = grants.get(face.consent_id)
        if (grant is None or grant.purpose != 'identity_matching' or
                grant.person_id != face.person_id or grant.epoch != face.epoch):
            raise ValueError('INVALID_CONFIRMED_FACE_CONSENT')
        if face.asset_version_id != before.binding.asset_version_id:
            continue
        if face.face_id in confirmed:
            raise ValueError('DUPLICATE_CONFIRMED_FACE')
        confirmed[face.face_id] = face.person_id
    if not before.faces or any(
            face.face_id not in confirmed or
            face.person_id != confirmed[face.face_id] or
            face.embedding is None for face in before.faces):
        return None
    return confirmed


def match_after_faces(before: FaceObservations,
                      after: FaceObservations,
                      confirmed: dict[UUID, UUID]) -> dict[UUID, UUID] | None:
    """Use only unique geometric pairs before requesting identity embeddings."""
    if len(before.faces) != len(after.faces):
        return None
    mapped: dict[UUID, UUID] = {}
    for source in before.faces:
        candidates = []
        for target in after.faces:
            a, b = source.box, target.box
            overlap = max(0., min(a.x1, b.x1)-max(a.x0, b.x0)) * max(
                0., min(a.y1, b.y1)-max(a.y0, b.y0))
            area_a = (a.x1-a.x0)*(a.y1-a.y0)
            area_b = (b.x1-b.x0)*(b.y1-b.y0)
            iou = overlap / (area_a+area_b-overlap) if overlap else 0.
            if iou >= .3:
                candidates.append(target)
        if len(candidates) != 1:
            return None
        target = candidates[0]
        if target.face_id in mapped or source.face_id not in confirmed:
            return None
        mapped[target.face_id] = confirmed[source.face_id]
    return mapped if len(mapped) == len(after.faces) else None


def measure_holdout_identity(*, before: FaceObservations,
                             after: FaceObservations,
                             source_binding: VersionBinding,
                             candidate_binding: VersionBinding,
                             source_rgb: np.ndarray,
                             candidate_rgb: np.ndarray,
                             effective_mask: np.ndarray,
                             protected: np.ndarray,
                             calibration: Calibration,
                             detector_qualified: bool,
                             detector_succeeded: bool,
                             consent_valid: bool,
                             aligned_canvas: bool,
                             source_alpha: np.ndarray | None = None,
                             candidate_alpha: np.ndarray | None = None,
                             check_active: Callable[[], None] = lambda: None) -> str:
    """Return pass/fail/not_measurable/no_person from measured evidence.

    Consent and alignment are externally fenced facts. An absent proof is a
    non-measurable case, never a successful identity comparison.
    """
    check_active()
    if (type(detector_qualified) is not bool or
            type(detector_succeeded) is not bool or
            type(consent_valid) is not bool or
            type(aligned_canvas) is not bool or
            before.binding != source_binding or
            after.binding != candidate_binding or
            source_binding.asset_version_id != candidate_binding.asset_version_id or
            source_rgb.ndim != 3):
        raise ValueError('INVALID_HOLDOUT_IDENTITY_BINDING')
    guards = measure_pixel_guards(source_rgb, candidate_rgb,
                                  effective_mask, protected,
                                  before_alpha=source_alpha,
                                  after_alpha=candidate_alpha,
                                  check_active=check_active)
    if guards.outside_mask_max_error > 0 or guards.protected_max_error > 0:
        return 'fail'
    if not detector_qualified or not detector_succeeded or not aligned_canvas:
        return 'not_measurable'
    if not before.faces and not after.faces:
        return 'no_person'
    metrics = compare_identity_observations(
        before, after, calibration,
        detector_qualified=detector_qualified,
        consent_valid=consent_valid, aligned_canvas=aligned_canvas,
        canvas_size=(source_rgb.shape[1], source_rgb.shape[0]),
        outside_mask_max_error=guards.outside_mask_max_error,
        protected_max_error=guards.protected_max_error)
    check_active()
    return 'not_measurable' if metrics.status == 'review' else metrics.status
