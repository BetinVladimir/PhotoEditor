"""Pinned-profile bridge between WorkingImage RGB and reflection_rgb sRGB."""

import numpy as np

from photo_worker.algorithms.model_delta import (
    _SRGB_TO_REC2020, _gamma24_to_linear, _linear_to_srgb,
    _srgb_to_linear, work_to_model,
)
from photo_worker.algorithms.safe_delta import compose_safe_delta

_PROFILES = frozenset({'linear_srgb_v1', 'work_rec2020_gamma24_v1'})


def _source(value, profile_id):
    value = np.asarray(value)
    if (profile_id not in _PROFILES or value.dtype != np.float32
            or value.ndim != 3 or value.shape[2] != 3
            or min(value.shape[:2]) < 1 or value.shape[0] * value.shape[1] > 16_000_000
            or not np.isfinite(value).all() or np.any(value < 0)
            or np.any(value > 1)):
        raise ValueError('REFLECTION_COLOR_INPUT_INVALID')
    return value


def reflection_model_input(source, profile_id):
    """Return sRGB model input and source pixels clipped at the model gamut."""
    source = _source(source, profile_id)
    if profile_id == 'linear_srgb_v1':
        return _linear_to_srgb(source), np.zeros(source.shape[:2], bool)
    return work_to_model(source)


def reflection_color_compose(source, model_output, target, protected, strength,
                             profile_id, source_clipped):
    """Compose in linear light, retaining exact source bits wherever E is zero."""
    source = _source(source, profile_id)
    if max(source.shape[:2]) > 1024:
        raise ValueError('REFLECTION_COLOR_INPUT_INVALID')
    model_output = np.asarray(model_output)
    source_clipped = np.asarray(source_clipped)
    if (model_output.dtype != np.float32 or model_output.shape != source.shape
            or not np.isfinite(model_output).all() or np.any(model_output < 0)
            or np.any(model_output > 1) or source_clipped.dtype != np.bool_
            or source_clipped.shape != source.shape[:2]):
        raise ValueError('REFLECTION_COLOR_INPUT_INVALID')
    source_linear = (source if profile_id == 'linear_srgb_v1'
                     else _gamma24_to_linear(source))
    proposal_linear = _srgb_to_linear(model_output)
    if profile_id == 'work_rec2020_gamma24_v1':
        proposal_linear = proposal_linear @ _SRGB_TO_REC2020.T
    guard = np.asarray(protected)
    if guard.dtype != np.float32 or guard.shape != source.shape[:2]:
        raise ValueError('REFLECTION_COLOR_INPUT_INVALID')
    effective_guard = guard.copy()
    effective_guard[source_clipped] = 1
    linear, changed, generated = compose_safe_delta(
        source_linear, proposal_linear.astype(np.float32), target,
        effective_guard, strength)
    if profile_id == 'linear_srgb_v1':
        result = linear
    else:
        result = source.copy()
        selected = generated > 0
        if selected.any():
            value = linear[selected]
            encoded = np.sign(value) * np.power(np.abs(value), np.float32(1/2.4))
            if not np.isfinite(encoded).all() or np.any(np.abs(encoded) > np.finfo(np.float32).max):
                raise ValueError('REFLECTION_COLOR_OUTPUT_INVALID')
            result[selected] = encoded.astype(np.float32)
        changed[selected] = np.any(result[selected].view(np.uint32) !=
                                   source[selected].view(np.uint32), axis=1).astype(np.uint8)
    return result, changed, generated
