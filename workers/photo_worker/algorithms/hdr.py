"""Deterministic CPU HDR helpers operating in the pinned gamma-2.4 space."""
from __future__ import annotations

import numpy as np
import cv2


def window_pull(source: np.ndarray, dark: np.ndarray, mask: np.ndarray,
                reference_exposure: float, dark_exposure: float) -> tuple[np.ndarray, int, int]:
    if (not isinstance(source, np.ndarray) or source.dtype != np.float32 or source.ndim != 3 or source.shape[2] != 3
            or not isinstance(dark, np.ndarray) or dark.dtype != np.float32 or dark.shape != source.shape
            or not isinstance(mask, np.ndarray) or mask.dtype != np.float32 or mask.shape != source.shape[:2]
            or not np.isfinite(source).all() or not np.isfinite(dark).all() or not np.isfinite(mask).all()
            or np.any(source < 0) or np.any(source > 1) or np.any(dark < 0) or np.any(dark > 1)
            or np.any(mask < 0) or np.any(mask > 1)
            or type(reference_exposure) not in (int, float) or type(dark_exposure) not in (int, float)
            or dark_exposure <= 0 or not 1 <= reference_exposure / dark_exposure <= 64):
        raise ValueError("WINDOW_INPUT_INVALID")
    ratio = np.float32(reference_exposure / dark_exposure)
    source_linear = np.power(source, np.float32(2.4), dtype=np.float32)
    dark_linear_raw = np.power(dark, np.float32(2.4), dtype=np.float32) * ratio
    dark_linear = np.clip(dark_linear_raw, 0, 1)
    effective = mask > 0
    clipped = effective & np.all(source_linear >= .999, axis=2) & np.all(dark_linear_raw >= .999, axis=2)
    mixed = source_linear * (1 - mask[..., None]) + dark_linear * mask[..., None]
    encoded = np.power(np.clip(mixed, 0, 1), np.float32(1 / 2.4), dtype=np.float32)
    # Avoid gamma round-trip changes outside the approved window.
    encoded = np.where(effective[..., None], encoded, source).astype(np.float32)
    return encoded, int(np.count_nonzero(effective)), int(np.count_nonzero(clipped))


def motion_residual(source: np.ndarray, reference: np.ndarray, source_to_reference: np.ndarray,
                    source_exposure: float, reference_exposure: float) -> float:
    if (source.dtype != np.float32 or reference.dtype != np.float32 or source.ndim != 3 or reference.ndim != 3
            or source.shape[2] != 3 or reference.shape[2] != 3 or min(*source.shape[:2], *reference.shape[:2]) < 8
            or not np.isfinite(source).all() or not np.isfinite(reference).all()
            or source_exposure <= 0 or reference_exposure <= 0):
        raise ValueError("HDR_MOTION_INPUT_INVALID")
    matrix = np.asarray(source_to_reference, np.float64)
    if matrix.shape != (3, 3) or not np.isfinite(matrix).all():
        raise ValueError("HDR_MOTION_MATRIX_INVALID")
    ref_h, ref_w = reference.shape[:2]
    scale = min(1., 1024 / max(ref_h, ref_w))
    width, height = max(8, round(ref_w * scale)), max(8, round(ref_h * scale))
    source_scale = min(1., 1024 / max(source.shape[:2]))
    sw, sh = max(8, round(source.shape[1] * source_scale)), max(8, round(source.shape[0] * source_scale))
    src = cv2.resize(source, (sw, sh), interpolation=cv2.INTER_AREA) if (sh, sw) != source.shape[:2] else source
    ref = cv2.resize(reference, (width, height), interpolation=cv2.INTER_AREA) if (height, width) != reference.shape[:2] else reference
    ss = np.array([[sw / source.shape[1], 0, (sw / source.shape[1] - 1) / 2],
                   [0, sh / source.shape[0], (sh / source.shape[0] - 1) / 2], [0, 0, 1]], np.float64)
    rs = np.array([[width / ref_w, 0, (width / ref_w - 1) / 2],
                   [0, height / ref_h, (height / ref_h - 1) / 2], [0, 0, 1]], np.float64)
    preview_matrix = rs @ matrix @ np.linalg.inv(ss)
    src_linear = np.power(src, np.float32(2.4), dtype=np.float32) * np.float32(reference_exposure / source_exposure)
    ref_linear = np.power(ref, np.float32(2.4), dtype=np.float32)
    warped = cv2.warpAffine(src_linear, preview_matrix[:2], (width, height), flags=cv2.INTER_LINEAR,
                            borderMode=cv2.BORDER_CONSTANT, borderValue=0)
    valid = cv2.warpAffine(np.ones((sh, sw), np.uint8), preview_matrix[:2], (width, height),
                           flags=cv2.INTER_NEAREST, borderMode=cv2.BORDER_CONSTANT, borderValue=0).astype(bool)
    if not np.any(valid):
        raise ValueError("HDR_MOTION_NO_OVERLAP")
    weights = np.array([.2627, .6780, .0593], np.float32)
    residual = np.abs(np.sum(warped * weights, axis=2) - np.sum(ref_linear * weights, axis=2))
    return float(np.count_nonzero((residual > .05) & valid) / np.count_nonzero(valid))


def calibrate_debevec(frames: list[np.ndarray], transforms: list[np.ndarray], target_shape: tuple[int, int],
                      exposures: list[float], max_edge: int = 512) -> np.ndarray:
    if len(frames) < 3 or len(frames) != len(transforms) or len(frames) != len(exposures):
        raise ValueError("HDR_CALIBRATION_INPUT_INVALID")
    th, tw = target_shape; scale = min(1., max_edge / max(th, tw))
    width, height = max(8, round(tw * scale)), max(8, round(th * scale))
    target_scale = np.array([[width / tw, 0, (width / tw - 1) / 2],
                             [0, height / th, (height / th - 1) / 2], [0, 0, 1]], np.float64)
    previews = []; valids = []
    for frame, transform in zip(frames, transforms):
        sh, sw = frame.shape[:2]; source_scale = min(1., max_edge / max(sh, sw))
        pw, ph = max(8, round(sw * source_scale)), max(8, round(sh * source_scale))
        preview = cv2.resize(frame, (pw, ph), interpolation=cv2.INTER_AREA) if (ph, pw) != (sh, sw) else frame
        scale_matrix = np.array([[pw / sw, 0, (pw / sw - 1) / 2],
                                 [0, ph / sh, (ph / sh - 1) / 2], [0, 0, 1]], np.float64)
        matrix = target_scale @ np.asarray(transform, np.float64) @ np.linalg.inv(scale_matrix)
        warped = cv2.warpAffine(preview, matrix[:2], (width, height), flags=cv2.INTER_LINEAR,
                                borderMode=cv2.BORDER_CONSTANT, borderValue=0)
        valid = cv2.warpAffine(np.ones((ph, pw), np.uint8), matrix[:2], (width, height),
                               flags=cv2.INTER_NEAREST, borderMode=cv2.BORDER_CONSTANT, borderValue=0).astype(bool)
        previews.append(np.round(np.clip(warped, 0, 1) * 255).astype(np.uint8)); valids.append(valid)
    common = np.logical_and.reduce(valids)
    ys, xs = np.nonzero(common)
    if len(xs) < 1024: raise ValueError("HDR_CALIBRATION_OVERLAP_INVALID")
    x0, x1, y0, y1 = xs.min(), xs.max() + 1, ys.min(), ys.max() + 1
    cropped = [item[y0:y1, x0:x1] for item in previews]
    response = cv2.createCalibrateDebevec(samples=70, lambda_=10., random=False).process(
        cropped, np.asarray(exposures, np.float32))
    if response.shape != (256, 1, 3) or response.dtype != np.float32 or not np.isfinite(response).all() or np.any(response < 0):
        raise ValueError("HDR_RESPONSE_INVALID")
    return response[:, 0, :]


def merge_debevec_tile(frames: list[np.ndarray], valids: list[np.ndarray], exposures: list[float],
                       response: np.ndarray, reference_index: int) -> tuple[np.ndarray, np.ndarray, np.ndarray]:
    if len(frames) < 3 or len(frames) != len(valids) or len(frames) != len(exposures) or response.shape != (256, 3):
        raise ValueError("HDR_MERGE_INPUT_INVALID")
    shape = frames[reference_index].shape; numerator = np.zeros(shape, np.float64); denominator = np.zeros(shape, np.float64)
    ref_linear = np.power(frames[reference_index], np.float32(2.4), dtype=np.float32)
    ref_time = exposures[reference_index]; motion = np.zeros(shape[:2], bool); clipped_all = np.ones(shape[:2], bool)
    for frame, valid, exposure in zip(frames, valids, exposures):
        if frame.dtype != np.float32 or frame.shape != shape or valid.shape != shape[:2]: raise ValueError("HDR_TILE_INVALID")
        z = np.round(np.clip(frame, 0, 1) * 255).astype(np.uint8)
        weights = (128 - np.abs(z.astype(np.int16) - 128)).astype(np.float32) * valid[..., None]
        radiance = response[z, np.arange(3)] / np.float32(exposure)
        numerator += radiance * weights; denominator += weights
        normalized = np.power(frame, np.float32(2.4), dtype=np.float32) * np.float32(ref_time / exposure)
        delta = np.abs(np.sum(normalized * np.array([.2627, .678, .0593], np.float32), axis=2) -
                       np.sum(ref_linear * np.array([.2627, .678, .0593], np.float32), axis=2))
        motion |= (delta > .05) & valid
        clipped_all &= ((z <= 1) | (z >= 254)).all(axis=2) | ~valid
    reference_radiance = response[np.round(np.clip(frames[reference_index], 0, 1) * 255).astype(np.uint8), np.arange(3)] / np.float32(ref_time)
    merged = np.where(denominator > 0, numerator / np.maximum(denominator, 1e-12), reference_radiance).astype(np.float32)
    merged[motion] = reference_radiance[motion]
    return merged, motion, clipped_all


def reinhard_global_tile(radiance: np.ndarray, log_average: float) -> np.ndarray:
    if radiance.dtype != np.float32 or radiance.ndim != 3 or radiance.shape[2] != 3 or log_average <= 0:
        raise ValueError("HDR_TONEMAP_INPUT_INVALID")
    weights = np.array([.2627, .678, .0593], np.float32)
    luma = np.sum(radiance * weights, axis=2)
    scaled_luma = luma / np.float32(log_average)
    mapped_luma = scaled_luma / (1 + scaled_luma)
    linear = radiance * (mapped_luma / np.maximum(luma, np.float32(1e-8)))[..., None]
    return np.power(np.clip(linear, 0, 1), np.float32(1 / 2.4), dtype=np.float32)
