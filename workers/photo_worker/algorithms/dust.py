"""Repeated sensor-dust detection; candidates always require review."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Callable
import cv2
import numpy as np


@dataclass(frozen=True)
class FrameDust:
    x: float
    y: float
    radius: float


def detect_frame_dust(rgb: np.ndarray, check_active: Callable[[], None] = lambda: None) -> list[FrameDust]:
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
            or min(rgb.shape[:2]) < 8 or not np.isfinite(rgb).all() or np.any(rgb < 0) or np.any(rgb > 1)):
        raise ValueError("DUST_IMAGE_INVALID")
    check_active()
    linear = np.power(rgb, np.float32(2.4), dtype=np.float32)
    luma = np.sum(linear * np.array([.2627, .6780, .0593], np.float32), axis=2)
    smooth = cv2.GaussianBlur(luma, (0, 0), 4, borderType=cv2.BORDER_REFLECT_101)
    residual = np.maximum(smooth - luma, 0)
    gx = cv2.Sobel(smooth, cv2.CV_32F, 1, 0, ksize=3)
    gy = cv2.Sobel(smooth, cv2.CV_32F, 0, 1, ksize=3)
    # Gradient is measured on the broad low-frequency field. The .05 bound
    # admits the compact dark core while rejecting ordinary textured edges.
    low_gradient = cv2.magnitude(gx, gy) <= .05
    values = residual[low_gradient]
    if values.size == 0: return []
    median = float(np.median(values)); mad = float(np.median(np.abs(values - median)))
    threshold = max(.01, median + 4 * 1.4826 * mad)
    binary = ((residual >= threshold) & low_gradient).astype(np.uint8)
    count, labels, stats, centroids = cv2.connectedComponentsWithStats(binary, 8)
    result = []
    for label in range(1, count):
        check_active()
        x, y, width, height, area = (int(v) for v in stats[label])
        if area < 3 or area > 512 or x == 0 or y == 0 or x + width == rgb.shape[1] or y + height == rgb.shape[0]: continue
        compactness = area / (width * height)
        radius = float(np.sqrt(area / np.pi))
        if compactness < .35 or not .75 <= radius <= 8: continue
        result.append(FrameDust(float(centroids[label, 0]), float(centroids[label, 1]), radius))
    return sorted(result, key=lambda item: (item.y, item.x, item.radius))


def map_to_sensor(candidate: FrameDust, frame_shape: tuple[int, int], crop: tuple[int, int, int, int]) -> FrameDust:
    height, width = frame_shape; x, y, crop_width, crop_height = crop
    if min(height, width, crop_width, crop_height) < 1: raise ValueError("DUST_MAPPING_INVALID")
    return FrameDust(x + (candidate.x + .5) * crop_width / width - .5,
                     y + (candidate.y + .5) * crop_height / height - .5,
                     candidate.radius * .5 * (crop_width / width + crop_height / height))


def repeated_dust(frames: list[list[FrameDust]], minimum_fraction: float = .8) -> list[tuple[FrameDust, int, int]]:
    if len(frames) < 5 or not .8 <= minimum_fraction <= 1: raise ValueError("DUST_SERIES_INVALID")
    clusters: list[list[tuple[int, FrameDust]]] = []
    for frame_index, candidates in enumerate(frames):
        for candidate in candidates:
            best = None; best_distance = float("inf")
            for index, cluster in enumerate(clusters):
                if any(seen_frame == frame_index for seen_frame, _ in cluster): continue
                centre_x = np.mean([item.x for _, item in cluster]); centre_y = np.mean([item.y for _, item in cluster])
                distance = float(np.hypot(candidate.x - centre_x, candidate.y - centre_y))
                if distance <= 2 and distance < best_distance: best, best_distance = index, distance
            if best is None: clusters.append([(frame_index, candidate)])
            else: clusters[best].append((frame_index, candidate))
    required = int(np.ceil(minimum_fraction * len(frames)))
    output = []
    for cluster in clusters:
        observed = len({frame for frame, _ in cluster})
        if observed < required: continue
        output.append((FrameDust(float(np.mean([item.x for _, item in cluster])),
                                 float(np.mean([item.y for _, item in cluster])),
                                 float(np.mean([item.radius for _, item in cluster]))), observed, len(frames)))
    return sorted(output, key=lambda value: (value[0].y, value[0].x))
