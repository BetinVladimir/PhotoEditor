"""Wide-gamut working-space adapter for RGB restoration models trained in sRGB."""
from __future__ import annotations

from dataclasses import dataclass
import cv2
import numpy as np


# Linear-light matrices, D65. Values are fixed as part of work_srgb_delta_v1.
_REC2020_TO_XYZ = np.array([[.6369580483, .1446169036, .1688809752],
                            [.2627002120, .6779980715, .0593017165],
                            [0., .0280726930, 1.0609850577]], np.float64)
_SRGB_TO_XYZ = np.array([[.4124564, .3575761, .1804375],
                         [.2126729, .7151522, .0721750],
                         [.0193339, .1191920, .9503041]], np.float64)
_REC2020_TO_SRGB = np.linalg.solve(_SRGB_TO_XYZ, _REC2020_TO_XYZ).astype(np.float32)
_SRGB_TO_REC2020 = np.linalg.solve(_REC2020_TO_XYZ, _SRGB_TO_XYZ).astype(np.float32)


@dataclass(frozen=True)
class DeltaEvidence:
    source_clipped: np.ndarray
    gamut_excess: np.ndarray
    disabled: np.ndarray


def _gamma24_to_linear(value: np.ndarray) -> np.ndarray:
    return np.power(np.maximum(value, 0), np.float32(2.4), dtype=np.float32)


def _linear_to_gamma24(value: np.ndarray) -> np.ndarray:
    return np.power(np.maximum(value, 0), np.float32(1 / 2.4), dtype=np.float32)


def _linear_to_srgb(value: np.ndarray) -> np.ndarray:
    value = np.asarray(value, np.float32)
    return np.where(value <= .0031308, 12.92 * value,
                    1.055 * np.power(np.maximum(value, 0), np.float32(1 / 2.4)) - .055).astype(np.float32)


def _srgb_to_linear(value: np.ndarray) -> np.ndarray:
    value = np.asarray(value, np.float32)
    return np.where(value <= .04045, value / 12.92,
                    np.power((value + .055) / 1.055, np.float32(2.4))).astype(np.float32)


def work_to_model(work_gamma24: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    """Return clipped nonlinear sRGB and pixels that needed gamut clipping."""
    work = np.asarray(work_gamma24, np.float32)
    if work.ndim != 3 or work.shape[2] != 3 or not np.isfinite(work).all() or np.any(work < 0) or np.any(work > 1):
        raise ValueError("MODEL_ADAPTER_INPUT_INVALID")
    linear_srgb = _gamma24_to_linear(work) @ _REC2020_TO_SRGB.T
    clipped = np.any((linear_srgb < 0) | (linear_srgb > 1), axis=2)
    return _linear_to_srgb(np.clip(linear_srgb, 0, 1)), clipped


def transfer_model_delta(original_work: np.ndarray, model_input: np.ndarray,
                         model_output: np.ndarray, strength: float,
                         *, gamut_error_limit: float = .02,
                         feather_pixels: int = 4) -> tuple[np.ndarray, DeltaEvidence]:
    """Move an sRGB model correction into linear Rec.2020 and mask unsafe pixels."""
    original = np.asarray(original_work, np.float32)
    before = np.asarray(model_input, np.float32); after = np.asarray(model_output, np.float32)
    if original.shape != before.shape or before.shape != after.shape or original.ndim != 3 or original.shape[2] != 3:
        raise ValueError("MODEL_DELTA_SHAPE_INVALID")
    if not 0 <= strength <= 1 or not np.isfinite(before).all() or not np.isfinite(after).all():
        raise ValueError("MODEL_DELTA_INVALID")
    if np.any(before < 0) or np.any(before > 1) or np.any(after < 0) or np.any(after > 1):
        raise ValueError("MODEL_DELTA_RANGE_INVALID")
    _, source_clipped = work_to_model(original)
    original_linear = _gamma24_to_linear(original)
    before_work = _srgb_to_linear(before) @ _SRGB_TO_REC2020.T
    after_work = _srgb_to_linear(after) @ _SRGB_TO_REC2020.T
    candidate = original_linear + (after_work - before_work)
    excess = np.maximum(-candidate, candidate - 1)
    gamut_excess = np.any(excess > gamut_error_limit, axis=2)
    disabled = source_clipped | gamut_excess
    if strength == 0:
        return original.copy(), DeltaEvidence(source_clipped, gamut_excess, disabled)
    valid = (~disabled).astype(np.uint8)
    weight = np.minimum(cv2.distanceTransform(valid, cv2.DIST_L2, 3) / max(1, feather_pixels), 1).astype(np.float32)
    weight *= np.float32(strength)
    blended = original_linear + (candidate - original_linear) * weight[..., None]
    output = _linear_to_gamma24(np.clip(blended, 0, 1))
    return output.astype(np.float32), DeltaEvidence(source_clipped, gamut_excess, disabled)


def resize_work_linear(work_gamma24: np.ndarray, size: tuple[int, int], interpolation: int = cv2.INTER_CUBIC) -> np.ndarray:
    """Resize Rec.2020 work RGB in linear light; size is (width,height)."""
    work = np.asarray(work_gamma24, np.float32)
    if work.ndim != 3 or work.shape[2] != 3 or size[0] < 1 or size[1] < 1:
        raise ValueError("MODEL_RESIZE_INVALID")
    linear = _gamma24_to_linear(work)
    resized = cv2.resize(linear, size, interpolation=interpolation)
    return _linear_to_gamma24(np.clip(resized, 0, 1)).astype(np.float32)
