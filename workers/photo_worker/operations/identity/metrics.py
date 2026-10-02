"""Deterministic face pairing and identity safety metrics.

This module consumes observations from a separately qualified detector and
identity model. It does not manufacture embeddings or consent evidence.
"""
from __future__ import annotations

from dataclasses import dataclass
from math import hypot, isfinite
from uuid import UUID

import numpy as np
from scipy.optimize import linear_sum_assignment

from photo_worker.operations.session.contracts import (
    Calibration, FaceObservation, FaceObservations,
)


@dataclass(frozen=True)
class FaceMetric:
    face_id: UUID
    matched_face_id: UUID | None
    similarity: float | None
    landmark_drift: float | None


@dataclass(frozen=True)
class IdentityMetrics:
    status: str
    faces: tuple[FaceMetric, ...]


@dataclass(frozen=True)
class PixelGuards:
    outside_mask_max_error: float
    protected_max_error: float


def measure_pixel_guards(before: np.ndarray, after: np.ndarray,
                         effective_mask: np.ndarray,
                         protected: np.ndarray, *, tile: int = 512) -> PixelGuards:
    """Measure every aligned pixel; any positive protected weight is strict."""
    shape = before.shape
    if (len(shape) != 3 or shape[2] not in (3, 4) or
            shape[0] < 1 or shape[1] < 1 or shape[0] * shape[1] > 200_000_000 or
            after.shape != shape or effective_mask.shape != shape[:2] or
            protected.shape != shape[:2] or tile not in (128, 256, 512, 1024)):
        raise ValueError('INVALID_IDENTITY_PIXELS')
    outside_error = protected_error = 0.0
    for y in range(0, shape[0], tile):
        for x in range(0, shape[1], tile):
            sl = np.s_[y:y + tile, x:x + tile]
            source = np.asarray(before[sl])
            result = np.asarray(after[sl])
            effective = np.asarray(effective_mask[sl])
            guard = np.asarray(protected[sl])
            if (not all(np.isfinite(value).all() for value in
                        (source, result, effective, guard)) or
                    np.any(effective < 0) or np.any(effective > 1) or
                    np.any(guard < 0) or np.any(guard > 1)):
                raise ValueError('INVALID_IDENTITY_PIXELS')
            difference = np.max(np.abs(source.astype(np.float64) -
                                       result.astype(np.float64)), axis=2)
            outside = effective == 0
            protected_pixels = guard > 0
            if np.any(outside):
                outside_error = max(outside_error,
                                    float(np.max(difference[outside])))
            if np.any(protected_pixels):
                protected_error = max(protected_error,
                                      float(np.max(difference[protected_pixels])))
    return PixelGuards(outside_error, protected_error)


def _box(face: FaceObservation) -> tuple[float, float, float, float]:
    box = face.box
    values = (box.x0, box.y0, box.x1, box.y1)
    if (not all(isfinite(value) for value in values) or
            not 0 <= box.x0 < box.x1 <= 1 or
            not 0 <= box.y0 < box.y1 <= 1):
        raise ValueError('INVALID_FACE_BOX')
    return values


def _iou(a: FaceObservation, b: FaceObservation) -> float:
    ax0, ay0, ax1, ay1 = _box(a)
    bx0, by0, bx1, by1 = _box(b)
    overlap = max(0.0, min(ax1, bx1) - max(ax0, bx0)) * max(
        0.0, min(ay1, by1) - max(ay0, by0))
    area_a = (ax1 - ax0) * (ay1 - ay0)
    area_b = (bx1 - bx0) * (by1 - by0)
    return overlap / (area_a + area_b - overlap)


def _similarity(a: FaceObservation, b: FaceObservation) -> float | None:
    if a.embedding is None or b.embedding is None:
        return None
    left = np.asarray(a.embedding, dtype=np.float64)
    right = np.asarray(b.embedding, dtype=np.float64)
    if (left.shape != (512,) or right.shape != (512,) or
            not np.isfinite(left).all() or not np.isfinite(right).all()):
        raise ValueError('INVALID_IDENTITY_EMBEDDING')
    norms = np.linalg.norm(left) * np.linalg.norm(right)
    if norms <= 1e-8:
        return None
    return float(np.clip(np.dot(left, right) / norms, -1, 1))


def _drift(a: FaceObservation, b: FaceObservation) -> float:
    before = np.asarray([(p.x, p.y) for p in a.landmarks], dtype=np.float64)
    after = np.asarray([(p.x, p.y) for p in b.landmarks], dtype=np.float64)
    if (before.shape != (68, 2) or after.shape != (68, 2) or
            not np.isfinite(before).all() or not np.isfinite(after).all()):
        raise ValueError('INVALID_FACE_LANDMARKS')
    x0, y0, x1, y1 = _box(a)
    diagonal = hypot(x1 - x0, y1 - y0)
    if diagonal <= 1e-8:
        raise ValueError('INVALID_FACE_BOX')
    return float(np.median(np.linalg.norm(before - after, axis=1)) / diagonal)


def compare_identity_observations(before: FaceObservations,
                                  after: FaceObservations,
                                  calibration: Calibration,
                                  *, detector_qualified: bool,
                                  consent_valid: bool,
                                  aligned_canvas: bool,
                                  canvas_size: tuple[int, int],
                                  outside_mask_max_error: float,
                                  protected_max_error: float) -> IdentityMetrics:
    """Pair on IoU≥0.3, maximize cardinality, then minimize center distance.

    Caller must prove a pinned transform placed both observations in the same
    canvas. Version-specific canvas hashes can differ with source identity.
    Detector qualification, consent epochs and exact pixel guards are caller
    evidence.
    """
    if (len(canvas_size) != 2 or any(type(v) is not int or v < 1 or v > 200_000
                                      for v in canvas_size) or
            not calibration.qualified or
            not isfinite(outside_mask_max_error) or
            not isfinite(protected_max_error) or
            outside_mask_max_error < 0 or protected_max_error < 0 or
            len(before.faces) > 100 or len(after.faces) > 100):
        raise ValueError('INVALID_IDENTITY_INPUT')
    if outside_mask_max_error > 0 or protected_max_error > 0:
        return IdentityMetrics('fail', ())
    if not aligned_canvas:
        return IdentityMetrics('not_measurable', ())
    if not detector_qualified:
        return IdentityMetrics('not_measurable', ())
    left = sorted(before.faces, key=lambda face: face.face_id.bytes)
    right = sorted(after.faces, key=lambda face: face.face_id.bytes)
    if len({face.face_id for face in left}) != len(left) or len({face.face_id for face in right}) != len(right):
        raise ValueError('DUPLICATE_FACE_ID')
    if not left and not right:
        return IdentityMetrics('not_measurable', ())
    if not left or not right:
        return IdentityMetrics('review', tuple(FaceMetric(face.face_id, None, None, None)
                                               for face in left))
    n, m = len(left), len(right)
    cost = np.full((n, m + n), 10.0, dtype=np.float64)
    cost[:, :m] = 1e6
    for i, face in enumerate(left):
        ax0, ay0, ax1, ay1 = _box(face)
        center_a = ((ax0 + ax1) / 2, (ay0 + ay1) / 2)
        for j, other in enumerate(right):
            if _iou(face, other) >= .3:
                bx0, by0, bx1, by1 = _box(other)
                distance = hypot(center_a[0] - (bx0 + bx1) / 2,
                                 center_a[1] - (by0 + by1) / 2)
                cost[i, j] = distance + (i * (m + 1) + j) * 1e-12
    rows, columns = linear_sum_assignment(cost)
    chosen = dict(zip(rows.tolist(), columns.tolist(), strict=True))
    metrics: list[FaceMetric] = []
    failed = unmeasurable = unmatched = False
    for i, face in enumerate(left):
        j = chosen[i]
        if j >= m or cost[i, j] >= 10:
            unmatched = True
            metrics.append(FaceMetric(face.face_id, None, None, None))
            continue
        other = right[j]
        similarity = _similarity(face, other) if consent_valid else None
        drift = _drift(face, other)
        width, height = canvas_size
        def short_edge(value: FaceObservation) -> float:
            x0, y0, x1, y1 = _box(value)
            return min((x1-x0)*width, (y1-y0)*height)
        if (similarity is None or face.quality <= 0 or other.quality <= 0 or
                short_edge(face) < calibration.min_face_edge_px or
                short_edge(other) < calibration.min_face_edge_px):
            unmeasurable = True
        elif (similarity < calibration.identity_similarity_min or
              drift > calibration.landmark_drift_max):
            failed = True
        metrics.append(FaceMetric(face.face_id, other.face_id,
                                  similarity, drift))
    if failed:
        status = 'fail'
    elif unmatched or n != m:
        status = 'review'
    elif unmeasurable:
        status = 'not_measurable'
    else:
        status = 'pass'
    return IdentityMetrics(status, tuple(metrics))
