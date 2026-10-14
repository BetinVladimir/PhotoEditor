"""Pinned background import for learning; no portable-ID lookup at evaluation."""
from contextlib import contextmanager
from copy import deepcopy

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.style import StyleV2
from photo_worker.v2.render_background_binding import FrozenBackgroundAsset, decode_frozen_background
from photo_worker.v2.render_background_decode import normalize_frozen_background


@contextmanager
def open_holdout_background_asset(artifacts, style, style_ref, binding, registry, *,
                                  work_icc_sha, execution_profile, retained_resources,
                                  check_active=lambda: None):
    """Normalize the declared encoded source, owning its mapping until scope exit.

    Caller must install the manifest/SQL-authorized pin and source input before
    invoking this helper. Neither StyleV2.asset_id nor a matching ICC alone
    establishes source authority. This helper never discovers an asset by ID.
    """
    check_active()
    if (not isinstance(style, StyleV2) or not isinstance(style_ref, ArtifactRef)
            or style_ref.artifact_id.int == 0 or not isinstance(binding, FrozenBackgroundAsset)):
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_PIN_REQUIRED')
    style = style.model_copy(deep=True)
    style_ref = style_ref.model_copy(deep=True)
    binding = decode_frozen_background(canonical_bytes(binding))
    background = style.extensions.background
    if (background is None or background.mode != 'asset'
            or background.asset_id != binding.portable_asset_id
            or binding.style != style_ref or binding.work_icc_sha != work_icc_sha):
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH')
    # Do not allow decoder/provider callbacks to alter retained-budget evidence.
    execution_profile = deepcopy(execution_profile)
    retained_resources = deepcopy(retained_resources)
    check_active()
    with normalize_frozen_background(artifacts, binding, registry,
            execution_profile=execution_profile, retained_resources=retained_resources,
            check_active=check_active) as normalized:
        check_active()
        if normalized.image.profile_sha256 != work_icc_sha:
            raise ValueError('HOLDOUT_BACKGROUND_ASSET_ICC_CHANGED')
        yield normalized
        check_active()


def render_holdout_asset_background(source, target, alpha_out, style, style_ref,
                                    binding, normalized, *, subject, confidence,
                                    work_icc_sha, check_active=lambda: None):
    """Compose a pinned, scoped normalized asset using production tile math.

    The caller keeps open_holdout_background_asset alive throughout rendering.
    This stage does not resolve assets or authorize undeclared task inputs.
    """
    import numpy as np
    from photo_worker.v2.render_background_decode import NormalizedBackground
    from photo_worker.algorithms.background import effective_alpha, asset_geometry, render_asset_tile, solid
    check_active()
    if (not isinstance(style, StyleV2) or not isinstance(style_ref, ArtifactRef)
            or not isinstance(binding, FrozenBackgroundAsset)
            or not isinstance(normalized, NormalizedBackground)):
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_PIN_REQUIRED')
    style = style.model_copy(deep=True)
    binding = decode_frozen_background(canonical_bytes(binding))
    background = style.extensions.background
    if (background is None or background.mode != 'asset'
            or background.asset_id != binding.portable_asset_id
            or style_ref != binding.style or work_icc_sha != binding.work_icc_sha
            or normalized.image.profile_sha256 != work_icc_sha):
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH')
    if (not isinstance(target, np.ndarray) or target.dtype != np.float32
            or target.shape != source.rgb.shape or target.ndim != 3 or target.shape[2] != 3
            or not isinstance(alpha_out, np.ndarray) or alpha_out.dtype != np.float32
            or alpha_out.shape != target.shape[:2] or np.shares_memory(target, source.rgb)
            or not isinstance(subject, np.ndarray) or subject.dtype != np.float32
            or subject.shape != target.shape[:2] or type(confidence) not in (float, int)
            or not .75 <= confidence <= 1):
        raise ValueError('HOLDOUT_BACKGROUND_SUBJECT_REQUIRED')
    rgb = normalized.rgb
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32
            or rgb.ndim != 3 or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1
            or rgb.shape[0]*rgb.shape[1] > 200_000_000
            or (normalized.image.height, normalized.image.width) != rgb.shape[:2]
            or np.shares_memory(rgb, target) or np.shares_memory(rgb, alpha_out)):
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_PIXELS_INVALID')
    h, w = target.shape[:2]
    if h*w > 200_000_000 or h < 1 or w < 1:
        raise ValueError('HOLDOUT_BACKGROUND_ASSET_PIXELS_INVALID')
    geometry = asset_geometry(rgb.shape[1], rgb.shape[0], w, h, 1, .5, .5)
    for y in range(0, h, 512):
        for x in range(0, w, 512):
            check_active()
            part = np.s_[y:y+512, x:x+512]
            coverage = effective_alpha(subject[part])
            original_alpha = (np.ones(coverage.shape, np.float32) if source.alpha is None else
                source.alpha[part].astype(np.float32)/np.float32(65535)
                if source.alpha.dtype == np.uint16 else source.alpha[part])
            if (not np.isfinite(original_alpha).all() or np.any(original_alpha < 0)
                    or np.any(original_alpha > 1)):
                raise ValueError('HOLDOUT_BACKGROUND_ALPHA_INVALID')
            backdrop = render_asset_tile(rgb, (h, w), (y, min(y+512, h), x, min(x+512, w)), geometry)
            target[part] = solid(target[part], coverage*original_alpha, backdrop)
            alpha_out[part] = 1
            protected = source.protected[part] > 0
            target[part][protected] = source.rgb[part][protected]
            alpha_out[part][protected] = original_alpha[protected]
    check_active()
