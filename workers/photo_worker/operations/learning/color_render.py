"""Exact V2 color stages for a frozen learning holdout.

The caller resolves StyleV2 and pins every source/mask before invoking this
function. Unsupported modules fail explicitly; a partial render must never
be compared with an approved image as if it were the complete pipeline.
"""
from __future__ import annotations

from collections.abc import Callable, Mapping
from uuid import UUID

import numpy as np

from photo_worker.algorithms.color import (
    apply_global, apply_local, apply_series, apply_strength, trilinear,
)
from photo_worker.algorithms.regions import apply_regions
from photo_worker.algorithms.style import ResolvedColor, resolve_style
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.series import ItemCorrection


def render_resolved_color(source: np.ndarray, target: np.ndarray,
                          resolved: ResolvedStyle | ResolvedColor, *,
                          style: StyleV2 | None = None,
                          absolute_lut: np.ndarray | None = None,
                          series_correction: ItemCorrection | None = None,
                          series_item_id: UUID | None = None,
                          person_mask: np.ndarray | None = None,
                          region_masks: Mapping[str, np.ndarray] | None = None,
                          tile: int = 512,
                          check_active: Callable[[], None] = lambda: None) -> None:
    """Write global → series → local → regions → one strength blend, tile by tile.

    A series correction is accepted only for the exact frozen item. The
    outer holdout pipeline still requires its own SQL-authorized evidence.
    """
    if (not isinstance(resolved, (ResolvedStyle, ResolvedColor)) or
            not isinstance(source, np.ndarray) or
            not isinstance(target, np.ndarray) or
            source.ndim != 3 or source.shape[-1] != 3 or
            source.shape != target.shape or
            source.shape[0] < 1 or source.shape[1] < 1 or
            source.dtype != np.float32 or target.dtype != np.float32 or
            source.shape[0] * source.shape[1] > 200_000_000 or
            tile not in (256, 512, 1024) or
            np.shares_memory(source, target) or
            (isinstance(resolved, ResolvedStyle) and resolved.mode == 'skip')):
        raise ValueError('INVALID_HOLDOUT_COLOR_INPUT')
    if isinstance(resolved, ResolvedStyle):
        if style is not None:
            raise ValueError('INVALID_HOLDOUT_COLOR_INPUT')
        beauty, defects, geometry = (resolved.modules.beauty,
                                     resolved.modules.defects,
                                     resolved.modules.geometry)
        extension = resolved.modules.extensions
        lut = absolute_lut
        lut_size = (resolved.absolute_lut.size
                    if resolved.absolute_lut is not None else None)
    else:
        if not isinstance(style, StyleV2) or absolute_lut is not None:
            raise ValueError('INVALID_HOLDOUT_COLOR_INPUT')
        beauty, defects, geometry = style.beauty, style.defects, style.geometry
        extension = style.extensions
        lut = resolved.absolute_lut
        lut_size = lut.shape[0] if isinstance(lut, np.ndarray) else None
    series_enabled = extension.series is not None and extension.series.enabled
    if series_enabled:
        if (not isinstance(series_correction, ItemCorrection) or
                type(series_item_id) is not UUID or series_item_id.int == 0 or
                series_correction.item_id != series_item_id):
            raise ValueError('HOLDOUT_SERIES_EVIDENCE_REQUIRED')
    elif series_correction is not None or series_item_id is not None:
        raise ValueError('HOLDOUT_SERIES_EVIDENCE_UNEXPECTED')
    unsupported = (beauty.enabled or defects.enabled or
                   geometry.enabled or extension.restoration is not None or
                   extension.background is not None or
                   any(value is not None and getattr(value, 'enabled', False)
                       for value in (extension.culling,
                                     extension.composition, extension.portrait_detail,
                                     extension.inpainting, extension.generation)))
    if unsupported:
        raise ValueError('HOLDOUT_RENDER_REQUIRES_EFFECT_PIPELINE')
    regions = extension.region_adjustments
    masks = dict(region_masks or {})
    if set(masks) != {rule.region_id for rule in regions}:
        raise ValueError('HOLDOUT_REGION_MASK_MISSING')
    shape = source.shape[:2]
    if resolved.local.enabled and person_mask is None:
        raise ValueError('HOLDOUT_PERSON_MASK_MISSING')
    if not resolved.local.enabled and person_mask is not None:
        raise ValueError('INVALID_HOLDOUT_MASK')
    for mask in [*masks.values(), *([person_mask] if person_mask is not None else [])]:
        if (not isinstance(mask, np.ndarray) or mask.shape != shape or
                mask.dtype != np.float32):
            raise ValueError('INVALID_HOLDOUT_MASK')
    if resolved.fixed is None:
        if (lut_size not in (33, 65) or
                not isinstance(lut, np.ndarray) or
                lut.shape != (lut_size,) * 3 + (3,) or
                lut.dtype != np.float32 or
                not np.isfinite(lut).all()):
            raise ValueError('INVALID_HOLDOUT_LUT')
    elif lut is not None:
        raise ValueError('INVALID_HOLDOUT_LUT')
    height, width = shape
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active()
            sl = np.s_[y:y + tile, x:x + tile]
            base = source[sl]
            colored = (apply_global(base, resolved.fixed) if resolved.fixed is not None
                       else np.clip(trilinear(lut, base), 0, 1).astype(np.float32))
            if series_enabled:
                colored = apply_series(colored, series_correction.applied_ev,
                                       series_correction.applied_gains)
            local = apply_local(colored,
                                person_mask[sl] if resolved.local.enabled else None,
                                resolved.local)
            if regions:
                local = apply_regions(local, [(rule, masks[rule.region_id][sl])
                                              for rule in regions],
                                      check_active=check_active).rgb
            target[sl] = apply_strength(base, local, resolved.strength)
    check_active()


def render_materialized_style(source: np.ndarray, target: np.ndarray,
                              style: StyleV2, *,
                              person_reliable: bool,
                              features: np.ndarray | None = None,
                              coverage_counts: np.ndarray | None = None,
                              person_mask: np.ndarray | None = None,
                              series_correction: ItemCorrection | None = None,
                              series_item_id: UUID | None = None,
                              region_masks: Mapping[str, np.ndarray] | None = None,
                              tile: int = 512,
                              check_active: Callable[[], None] = lambda: None,
                              ) -> ResolvedColor:
    """Resolve a frozen StyleV2 exactly as style.resolve, then render color."""
    if type(person_reliable) is not bool:
        raise ValueError('INVALID_HOLDOUT_COLOR_INPUT')
    if style.mode == 'adaptive' and (features is None or coverage_counts is None):
        raise ValueError('ADAPTIVE_FEATURES_REQUIRED')
    result = resolve_style(style, features=features,
                           coverage_counts=coverage_counts,
                           person_reliable=person_reliable,
                           strict_ood=False)
    render_resolved_color(source, target, result, style=style,
                          series_correction=series_correction,
                          series_item_id=series_item_id,
                          person_mask=person_mask if result.local.enabled else None,
                          region_masks=region_masks, tile=tile,
                          check_active=check_active)
    return result
