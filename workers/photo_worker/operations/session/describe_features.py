"""Deterministic V22 scene statistics over a bounded linear Rec.2020 proxy."""
from __future__ import annotations

from dataclasses import dataclass

import cv2
import numpy as np


@dataclass(frozen=True)
class FrameStatistics:
    luma_quantiles: tuple[float, float, float, float, float]
    neutral_rgb: tuple[float, float, float] | None
    sharpness: float
    clipping_fraction: float


def _resize(rgb: np.ndarray, *, longest: int | None = None,
            shortest: int | None = None) -> np.ndarray:
    height, width = rgb.shape[:2]
    if longest is not None:
        scale = min(1.0, longest / max(height, width))
    elif shortest is not None:
        scale = shortest / min(height, width)
    else:
        raise ValueError('INVALID_PROXY_SIZE')
    target = (max(1, round(width * scale)), max(1, round(height * scale)))
    if target[0] * target[1] > 16_000_000:
        raise ValueError('PROXY_RESOURCE_LIMIT')
    if target == (width, height):
        return rgb
    return cv2.resize(rgb, target,
                      interpolation=cv2.INTER_AREA if scale < 1 else cv2.INTER_CUBIC)


def frame_statistics(linear_rgb: np.ndarray) -> FrameStatistics:
    """Compute schema-bounded statistics without face or identity features.

    Input is a decoded, linear-light Rec.2020 array. Its longest edge is
    capped before statistics, and sharpness uses a 512 px shortest edge.
    """
    source = np.asarray(linear_rgb)
    if (source.ndim != 3 or source.shape[2] != 3 or min(source.shape[:2]) < 1
            or source.shape[0] * source.shape[1] > 200_000_000):
        raise ValueError('INVALID_LINEAR_PROXY')
    rgb = np.asarray(source, np.float64)
    if not np.isfinite(rgb).all():
        raise ValueError('INVALID_LINEAR_PROXY')
    if max(rgb.shape[:2]) > 2048:
        rgb = _resize(rgb, longest=2048)
    clipped = np.any((rgb < 0) | (rgb >= 1), axis=2)
    luma = rgb @ np.array([.2126, .7152, .0722], np.float64)
    quantiles = tuple(float(np.clip(v, 0, 1)) for v in
                      np.quantile(luma, [.1, .25, .5, .75, .9]))
    neutral = ((np.max(rgb, axis=2) - np.min(rgb, axis=2) <= .05)
               & (luma >= .1) & (luma <= .9) & ~clipped)
    neutral_rgb = None
    if np.count_nonzero(neutral) >= 1024:
        neutral_rgb = tuple(float(np.clip(v, 0, 1)) for v in
                            np.median(rgb[neutral], axis=0))
    focus = _resize(rgb, shortest=512)
    focus_luma = focus @ np.array([.2126, .7152, .0722], np.float64)
    laplacian = cv2.Laplacian(focus_luma, cv2.CV_64F, ksize=3)
    sharpness = float(np.clip(np.mean(np.square(laplacian), dtype=np.float64),
                              0, 1_000_000))
    return FrameStatistics(quantiles, neutral_rgb, sharpness,
                           float(np.mean(clipped, dtype=np.float64)))


def normalize_scene_embedding(value: np.ndarray) -> tuple[float, ...]:
    vector = np.asarray(value, np.float64).reshape(-1)
    if vector.shape != (384,) or not np.isfinite(vector).all():
        raise ValueError('MODEL_OUTPUT_INVALID')
    norm = float(np.linalg.norm(vector))
    if norm <= 1e-12:
        raise ValueError('MODEL_OUTPUT_INVALID')
    normalized = vector / norm
    if np.any(normalized < -1) or np.any(normalized > 1):
        raise ValueError('MODEL_OUTPUT_INVALID')
    return tuple(float(v) for v in normalized)
