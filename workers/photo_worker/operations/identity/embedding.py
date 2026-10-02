"""Pinned 68-point alignment for qualified identity512 inference."""
from __future__ import annotations

import cv2
import numpy as np

from photo_worker.operations.session.contracts import FaceObservation


def align_identity_face(srgb: np.ndarray, face: FaceObservation,
                        size: int = 112) -> np.ndarray:
    if (size != 112 or srgb.ndim != 3 or srgb.shape[2] != 3 or
            min(srgb.shape[:2]) < 1 or
            srgb.shape[0] * srgb.shape[1] > 200_000_000 or
            not np.isfinite(srgb).all() or
            np.any(srgb < 0) or np.any(srgb > 1) or
            len(face.landmarks) != 68):
        raise ValueError('IDENTITY_ALIGNMENT_INVALID')
    height, width = srgb.shape[:2]
    points = np.asarray([(point.x * width, point.y * height)
                         for point in face.landmarks], dtype=np.float64)
    if (not np.isfinite(points).all() or np.any(points[:, 0] < 0) or
            np.any(points[:, 0] >= width) or np.any(points[:, 1] < 0) or
            np.any(points[:, 1] >= height)):
        raise ValueError('IDENTITY_ALIGNMENT_INVALID')
    eyes = sorted((points[36:42].mean(axis=0), points[42:48].mean(axis=0)),
                  key=lambda point: point[0])
    anchors = np.asarray([eyes[0], eyes[1], points[30]], dtype=np.float32)
    target = np.asarray([(.32 * size, .38 * size),
                         (.68 * size, .38 * size),
                         (.50 * size, .58 * size)], dtype=np.float32)
    if abs(float(np.linalg.det(np.c_[anchors, np.ones(3)]))) < 1e-3:
        raise ValueError('IDENTITY_ALIGNMENT_INVALID')
    transform = cv2.getAffineTransform(anchors, target)
    inverse = cv2.invertAffineTransform(transform)
    corners = np.asarray([[0, 0, 1], [size - 1, 0, 1],
                          [0, size - 1, 1], [size - 1, size - 1, 1]],
                         dtype=np.float64)
    source = corners @ inverse.T
    if (not np.isfinite(source).all() or np.any(source[:, 0] < 0) or
            np.any(source[:, 0] > width - 1) or np.any(source[:, 1] < 0) or
            np.any(source[:, 1] > height - 1)):
        raise ValueError('IDENTITY_ALIGNMENT_OUTSIDE_IMAGE')
    aligned = cv2.warpAffine(np.asarray(srgb, dtype=np.float32), transform,
                             (size, size), flags=cv2.INTER_LINEAR,
                             borderMode=cv2.BORDER_CONSTANT, borderValue=0)
    if aligned.shape != (size, size, 3) or not np.isfinite(aligned).all():
        raise ValueError('IDENTITY_ALIGNMENT_INVALID')
    return np.clip(aligned, 0, 1).astype(np.float32)
