"""Deterministic background composition in the working gamma-2.4 space."""
from __future__ import annotations

import numpy as np
import cv2


def effective_alpha(subject: np.ndarray, source_alpha: np.ndarray | None = None) -> np.ndarray:
    if not isinstance(subject, np.ndarray) or subject.dtype != np.float32 or subject.ndim != 2:
        raise ValueError("SUBJECT_MASK_INVALID")
    if not np.isfinite(subject).all() or np.any(subject < 0) or np.any(subject > 1):
        raise ValueError("SUBJECT_MASK_INVALID")
    result = subject
    if source_alpha is not None:
        if not isinstance(source_alpha, np.ndarray) or source_alpha.dtype != np.uint16 or source_alpha.shape != subject.shape:
            raise ValueError("SOURCE_ALPHA_INVALID")
        result = subject * (source_alpha.astype(np.float32) / np.float32(65535.0))
    return np.clip(result, 0.0, 1.0).astype(np.float32)


def alpha_u16(alpha: np.ndarray) -> np.ndarray:
    if not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.ndim != 2:
        raise ValueError("ALPHA_INVALID")
    return np.rint(np.clip(alpha, 0.0, 1.0) * np.float32(65535.0)).astype(np.uint16)


def transparent(rgb: np.ndarray, alpha: np.ndarray) -> np.ndarray:
    _validate_rgb_alpha(rgb, alpha)
    # Straight RGB is retained wherever alpha carries any coverage. Fully hidden
    # pixels are canonical zero so later encoders cannot leak their old colour.
    return np.where((alpha > 0)[..., None], rgb, np.float32(0.0)).astype(np.float32)


def solid(rgb: np.ndarray, alpha: np.ndarray, background: tuple[float, float, float] | list[float] | np.ndarray) -> np.ndarray:
    _validate_rgb_alpha(rgb, alpha)
    if isinstance(background, np.ndarray):
        if background.dtype != np.float32 or background.shape != rgb.shape or not np.isfinite(background).all() or np.any(background < 0) or np.any(background > 1):
            raise ValueError("BACKGROUND_INVALID")
        background_array = background
    else:
        if len(background) != 3 or any(type(value) not in (int, float) or not 0 <= value <= 1 for value in background):
            raise ValueError("BACKGROUND_INVALID")
        background_array = np.asarray(background, dtype=np.float32)
    # The working encoding is gamma 2.4. Premultiplication and composition must
    # happen after decoding, then return to the same encoding.
    foreground_linear = np.power(rgb, np.float32(2.4), dtype=np.float32)
    background_linear = np.power(background_array, np.float32(2.4), dtype=np.float32)
    mixed = foreground_linear * alpha[..., None] + background_linear * (np.float32(1.0) - alpha[..., None])
    return np.power(np.clip(mixed, 0.0, 1.0), np.float32(1.0 / 2.4), dtype=np.float32)


def asset_geometry(source_width: int, source_height: int, output_width: int, output_height: int,
                   scale: float, position_x: float, position_y: float) -> tuple[float, float, float]:
    if any(type(value) is not int or value < 1 for value in (source_width, source_height, output_width, output_height)):
        raise ValueError("ASSET_GEOMETRY_INVALID")
    if type(scale) not in (int, float) or not 1 <= scale <= 10 or not 0 <= position_x <= 1 or not 0 <= position_y <= 1:
        raise ValueError("ASSET_GEOMETRY_INVALID")
    factor = max(output_width / source_width, output_height / source_height) * scale
    rendered_width, rendered_height = source_width * factor, source_height * factor
    left = (output_width - rendered_width) * position_x
    top = (output_height - rendered_height) * position_y
    # Cover geometry plus constrained positioning must include every output pixel centre.
    if left > 1e-6 or top > 1e-6 or left + rendered_width < output_width - 1e-6 or top + rendered_height < output_height - 1e-6:
        raise ValueError("ASSET_DOES_NOT_COVER_CANVAS")
    return factor, left, top


def render_asset_tile(background: np.ndarray, output_shape: tuple[int, int], bounds: tuple[int, int, int, int],
                      geometry: tuple[float, float, float]) -> np.ndarray:
    if not isinstance(background, np.ndarray) or background.dtype != np.float32 or background.ndim != 3 or background.shape[2] != 3:
        raise ValueError("BACKGROUND_INVALID")
    output_height, output_width = output_shape
    y0, y1, x0, x1 = bounds
    if not (0 <= y0 < y1 <= output_height and 0 <= x0 < x1 <= output_width):
        raise ValueError("ASSET_BOUNDS_INVALID")
    factor, left, top = geometry
    xs = (np.arange(x0, x1, dtype=np.float32) + .5 - left) / factor - .5
    ys = (np.arange(y0, y1, dtype=np.float32) + .5 - top) / factor - .5
    map_x, map_y = np.meshgrid(xs, ys)
    return cv2.remap(background, map_x, map_y, interpolation=cv2.INTER_LINEAR,
                     borderMode=cv2.BORDER_REPLICATE).astype(np.float32)


def contact_shadow(mask: np.ndarray, blur_radius: int, offset_x: int, offset_y: int,
                   opacity: float, crop: tuple[slice, slice]) -> np.ndarray:
    if (not isinstance(mask, np.ndarray) or mask.dtype != np.float32 or mask.ndim != 2
            or not np.isfinite(mask).all() or np.any(mask < 0) or np.any(mask > 1)
            or type(blur_radius) is not int or blur_radius < 1 or type(offset_x) is not int or type(offset_y) is not int
            or type(opacity) not in (int, float) or not 0 <= opacity <= .3):
        raise ValueError("SHADOW_INPUT_INVALID")
    matrix = np.array([[1., 0., float(offset_x)], [0., 1., float(offset_y)]], np.float32)
    shifted = cv2.warpAffine(mask, matrix, (mask.shape[1], mask.shape[0]), flags=cv2.INTER_LINEAR,
                             borderMode=cv2.BORDER_CONSTANT, borderValue=0)
    sigma = max(blur_radius / 3.0, .5)
    blurred = cv2.GaussianBlur(shifted, (0, 0), sigma, borderType=cv2.BORDER_CONSTANT)
    # Never darken the protected subject itself.
    return np.clip(blurred[crop] * (1 - mask[crop]) * opacity, 0, .3).astype(np.float32)


def sky_gains(source_sum: np.ndarray, asset_sum: np.ndarray, weight: float, limit: float) -> np.ndarray:
    if (not isinstance(source_sum, np.ndarray) or source_sum.shape != (3,) or
            not isinstance(asset_sum, np.ndarray) or asset_sum.shape != (3,) or
            not np.isfinite(source_sum).all() or not np.isfinite(asset_sum).all() or
            type(weight) not in (int, float) or weight <= 0 or type(limit) not in (int, float) or not 0 <= limit <= .25):
        raise ValueError("SKY_STATS_INVALID")
    source_mean = source_sum.astype(np.float64) / weight
    asset_mean = asset_sum.astype(np.float64) / weight
    raw = source_mean / np.maximum(asset_mean, 1e-8)
    return np.clip(raw, 1 - limit, 1 + limit).astype(np.float32)


def replace_sky_tile(source: np.ndarray, asset: np.ndarray, sky: np.ndarray,
                     protected: np.ndarray, gains: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    _validate_rgb_alpha(source, sky)
    if (not isinstance(asset, np.ndarray) or asset.dtype != np.float32 or asset.shape != source.shape
            or not np.isfinite(asset).all() or np.any(asset < 0) or np.any(asset > 1)
            or not isinstance(protected, np.ndarray) or protected.dtype != np.float32 or protected.shape != sky.shape
            or not np.isfinite(protected).all() or np.any(protected < 0) or np.any(protected > 1)
            or not isinstance(gains, np.ndarray) or gains.dtype != np.float32 or gains.shape != (3,)
            or np.any(gains < .75) or np.any(gains > 1.25)):
        raise ValueError("SKY_INPUT_INVALID")
    effective = np.clip(sky * (1 - protected), 0, 1).astype(np.float32)
    source_linear = np.power(source, np.float32(2.4), dtype=np.float32)
    asset_linear = np.clip(np.power(asset, np.float32(2.4), dtype=np.float32) * gains, 0, 1)
    mixed = source_linear * (1 - effective[..., None]) + asset_linear * effective[..., None]
    encoded = np.power(np.clip(mixed, 0, 1), np.float32(1 / 2.4), dtype=np.float32)
    encoded = np.where((effective > 0)[..., None], encoded, source).astype(np.float32)
    return encoded, effective


def _validate_rgb_alpha(rgb: np.ndarray, alpha: np.ndarray) -> None:
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]
            or not np.isfinite(rgb).all() or not np.isfinite(alpha).all()
            or np.any(rgb < 0) or np.any(rgb > 1) or np.any(alpha < 0) or np.any(alpha > 1)):
        raise ValueError("COMPOSITE_INPUT_INVALID")


def depth_layer_radii(long_edge: int, strength: float, subject_depth: float) -> tuple[int, ...]:
    if type(long_edge) is not int or long_edge < 1 or type(strength) not in (int, float) or not 0 <= strength <= 1 or not 0 <= subject_depth <= 1:
        raise ValueError("BLUR_CONFIG_INVALID")
    maximum = int(np.ceil(.02 * long_edge * strength))
    return tuple(int(round(maximum * abs(layer / 7.0 - subject_depth))) for layer in range(8))


def blur_background_tile(rgb: np.ndarray, subject: np.ndarray, depth: np.ndarray,
                         radii: tuple[int, ...], crop: tuple[slice, slice]) -> np.ndarray:
    """Blur one halo-expanded tile and return its core in gamma-2.4 encoding."""
    _validate_rgb_alpha(rgb, subject)
    if (not isinstance(depth, np.ndarray) or depth.dtype != np.float32 or depth.shape != subject.shape
            or not np.isfinite(depth).all() or np.any(depth < 0) or np.any(depth > 1)
            or len(radii) != 8 or any(type(radius) is not int or radius < 0 for radius in radii)):
        raise ValueError("BLUR_INPUT_INVALID")
    linear = np.power(rgb, np.float32(2.4), dtype=np.float32)
    background_weight = np.float32(1.0) - subject
    layer_index = np.minimum(7, np.floor(depth * np.float32(8.0)).astype(np.int16))
    output = np.empty((*subject[crop].shape, 3), dtype=np.float32)
    source_core = linear[crop]
    subject_core = subject[crop]
    for layer, radius in enumerate(radii):
        selected = layer_index[crop] == layer
        if not np.any(selected):
            continue
        if radius == 0:
            reconstructed = source_core
        else:
            sigma = max(radius / 3.0, .5)
            numerator = cv2.GaussianBlur(linear * background_weight[..., None], (0, 0), sigma,
                                          borderType=cv2.BORDER_REFLECT_101)
            denominator = cv2.GaussianBlur(background_weight, (0, 0), sigma,
                                            borderType=cv2.BORDER_REFLECT_101)
            reconstructed = numerator[crop] / np.maximum(denominator[crop][..., None], np.float32(1e-6))
            # A tile containing no known background cannot invent texture.
            reconstructed = np.where((denominator[crop] > 1e-6)[..., None], reconstructed, source_core)
        mixed = source_core * subject_core[..., None] + reconstructed * (np.float32(1.0) - subject_core[..., None])
        output[selected] = mixed[selected]
    return np.power(np.clip(output, 0.0, 1.0), np.float32(1.0 / 2.4), dtype=np.float32)
