"""Attempt-scoped disk workspace for three V23 holdout renders."""
from __future__ import annotations

from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
from hashlib import sha256
from typing import Callable, Iterator

import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.series import ItemCorrection
from photo_worker.runtime.artifacts import TaskArtifacts

from .contracts import RenderCase
from .recipe import FrozenHoldoutStyles
from .source import HoldoutSource, open_holdout_source


@dataclass(frozen=True)
class HoldoutWorkspace:
    source: HoldoutSource
    baseline: np.memmap
    candidate: np.memmap
    approved: np.memmap
    alpha: np.memmap


def _region_mask_fingerprints(region_masks: dict[str, np.ndarray],
                              shape: tuple[int, int],
                              check_active: Callable[[], None],
                              ) -> dict[str, bytes]:
    """Validate and hash borrowed masks in bounded tiles before each arm."""
    fingerprints: dict[str, bytes] = {}
    for region_id, mask in region_masks.items():
        if (not isinstance(mask, np.ndarray) or mask.shape != shape or
                mask.dtype != np.float32):
            raise ValueError('INVALID_HOLDOUT_MASK')
        digest = sha256()
        for y in range(0, shape[0], 512):
            for x in range(0, shape[1], 512):
                check_active()
                values = mask[y:y + 512, x:x + 512]
                if (not np.isfinite(values).all() or np.any(values < 0)
                        or np.any(values > 1)):
                    raise ValueError('INVALID_HOLDOUT_MASK')
                digest.update(np.ascontiguousarray(values).tobytes())
        fingerprints[region_id] = digest.digest()
    return fingerprints


def _require_region_mask_fingerprints(region_masks: dict[str, np.ndarray],
                                      shape: tuple[int, int],
                                      expected: dict[str, bytes],
                                      check_active: Callable[[], None]) -> None:
    if _region_mask_fingerprints(region_masks, shape, check_active) != expected:
        raise ValueError('HOLDOUT_REGION_BINDING_MISMATCH')


def _reviewed_rgb_fingerprint(rgb: np.ndarray, shape: tuple[int, int, int],
                              check_active: Callable[[], None]) -> bytes:
    """Hash a borrowed repair buffer in tiles without copying a full frame."""
    if (not isinstance(rgb, np.ndarray) or rgb.shape != shape or
            rgb.dtype != np.float32):
        raise ValueError('HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH')
    digest = sha256()
    for y in range(0, shape[0], 256):
        for x in range(0, shape[1], 256):
            check_active()
            digest.update(np.ascontiguousarray(rgb[y:y + 256, x:x + 256]).tobytes())
    check_active()
    return digest.digest()


def _require_independent_holdout_buffers(workspace: HoldoutWorkspace) -> None:
    source = workspace.source
    rgb = source.rgb
    if (not isinstance(rgb, np.ndarray) or rgb.ndim != 3 or
            rgb.shape[2] != 3 or rgb.dtype != np.float32):
        raise ValueError('HOLDOUT_WORKSPACE_BUFFER_INVALID')
    outputs = (workspace.baseline, workspace.candidate,
               workspace.approved, workspace.alpha)
    if (any(not isinstance(array, np.ndarray) or
            array.dtype != np.float32 or
            array.shape != (rgb.shape[:2] if index == 3 else rgb.shape)
            for index, array in enumerate(outputs))):
        raise ValueError('HOLDOUT_WORKSPACE_BUFFER_INVALID')
    inputs = (rgb, source.alpha, source.protected, source.effective_mask,
              *(array for _, array in source.region_masks.values()))
    if any(array is not None and not isinstance(array, np.ndarray)
           for array in inputs):
        raise ValueError('HOLDOUT_WORKSPACE_BUFFER_INVALID')
    for index, output in enumerate(outputs):
        for other in (*inputs, *outputs[:index]):
            if other is None:
                continue
            try:
                overlap = np.shares_memory(output, other)
            except Exception:
                raise ValueError('HOLDOUT_WORKSPACE_BUFFER_INVALID') from None
            if overlap:
                raise ValueError('HOLDOUT_WORKSPACE_BUFFER_ALIAS')


def render_holdout_triplet(workspace: HoldoutWorkspace,
                           styles: FrozenHoldoutStyles, *,
                           person_reliable: bool,
                           artifacts=None,
                           observations=None,
                           binding=None,
                           skin_probability=None,
                           landmarks=(),
                           prepare_source=None,
                           background_subject=None,
                           background_confidence=0.0,
                           background_depth=None,
                           background_asset=None,
                           background_pin=None,
                           background_style_ref=None,
                           work_icc_sha=None,
                           features: np.ndarray | None = None,
                           coverage_counts: np.ndarray | None = None,
                           person_mask: np.ndarray | None = None,
                           region_masks: dict[str, np.ndarray] | None = None,
                           reviewed_source_rgb: np.ndarray | None = None,
                           reviewed_dust=None,
                           reviewed_detail=None,
                           series_correction=None,
                           series_item_id=None,
                           check_active: Callable[[], None] = lambda: None,
                           ) -> None:
    """Render baseline, candidate and approved through the same V2 source."""
    from .color_render import render_materialized_style
    from .beauty_render import render_holdout_beauty

    # Freeze every comparison arm before any callback can mutate caller data.
    styles = FrozenHoldoutStyles(
        styles.baseline.model_copy(deep=True),
        styles.candidate.model_copy(deep=True),
        styles.approved.model_copy(deep=True))
    backgrounds = [style.extensions.background for style in
                   (styles.baseline, styles.candidate, styles.approved)]
    if any(background != backgrounds[0] for background in backgrounds[1:]):
        raise ValueError('HOLDOUT_BACKGROUND_BINDING_MISMATCH')
    _require_independent_holdout_buffers(workspace)

    check_active()
    from .pipeline_admission import validate_holdout_pipeline
    validate_holdout_pipeline(styles,
        reviewed_defect=reviewed_source_rgb is not None,
        reviewed_dust=reviewed_dust is not None,
        reviewed_detail=reviewed_detail is not None,
        reviewed_series=series_correction is not None)
    if (series_correction is None) != (series_item_id is None):
        raise ValueError('HOLDOUT_SERIES_EVIDENCE_REQUIRED')
    if series_correction is not None:
        series_correction = ItemCorrection.model_validate_json(
            canonical_bytes(series_correction))
        series_fingerprint = canonical_bytes(series_correction)
    if reviewed_dust is not None and not callable(reviewed_dust):
        raise ValueError('HOLDOUT_DUST_REPLAY_BINDING_MISMATCH')
    if reviewed_detail is not None and not callable(reviewed_detail):
        raise ValueError('HOLDOUT_DETAIL_REPLAY_BINDING_MISMATCH')
    if reviewed_source_rgb is not None and (
            reviewed_source_rgb.shape != workspace.source.rgb.shape or
            reviewed_source_rgb.dtype != np.float32 or
            np.shares_memory(reviewed_source_rgb, workspace.source.rgb) or
            any(np.shares_memory(reviewed_source_rgb, target) for target in
                (workspace.baseline, workspace.candidate, workspace.approved))):
        raise ValueError('HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH')
    if reviewed_source_rgb is not None:
        from .defect_replay import validate_repaired_holdout_pixels
        validate_repaired_holdout_pixels(workspace.source.rgb,
            reviewed_source_rgb, workspace.source.protected, check_active)
        reviewed_fingerprint = _reviewed_rgb_fingerprint(
            reviewed_source_rgb, workspace.source.rgb.shape, check_active)

        def require_same_repair() -> None:
            if _reviewed_rgb_fingerprint(reviewed_source_rgb,
                    workspace.source.rgb.shape, check_active) != reviewed_fingerprint:
                raise ValueError('HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH')
    else:
        def require_same_repair() -> None:
            return None

    rules = styles.candidate.extensions.region_adjustments
    # A learned scalar correction cannot change region selection between arms.
    # Freeze the mapping itself before source/model callbacks can mutate it.
    region_masks = dict(region_masks or {})
    if (any(style.extensions.region_adjustments != rules
            for style in (styles.baseline, styles.approved))
            or len({rule.region_id for rule in rules}) != len(rules)
            or set(region_masks) != {rule.region_id for rule in rules}):
        raise ValueError('HOLDOUT_REGION_BINDING_MISMATCH')
    # The caller owns these arrays. A preparation/model callback must not
    # change the selection between baseline, candidate and approved renders.
    mask_fingerprints = _region_mask_fingerprints(
        region_masks, workspace.source.rgb.shape[:2], check_active)

    def require_same_masks() -> None:
        _require_region_mask_fingerprints(region_masks,
                                          workspace.source.rgb.shape[:2],
                                          mask_fingerprints, check_active)

    def require_same_series() -> None:
        if series_correction is not None and (
                canonical_bytes(series_correction) != series_fingerprint or
                series_correction.item_id != series_item_id):
            raise ValueError('HOLDOUT_SERIES_CORRECTION_CHANGED')

    if any(background is not None and background.mode == 'asset' for background in backgrounds):
        from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
        from photo_worker.v2.render_background_decode import NormalizedBackground
        from photo_worker.contracts.base import ArtifactRef
        if (not isinstance(background_pin, FrozenBackgroundAsset)
                or not isinstance(background_asset, NormalizedBackground)
                or not isinstance(background_style_ref, ArtifactRef)
                or background_pin.style != background_style_ref
                or background_pin.work_icc_sha != work_icc_sha
                or background_asset.image.profile_sha256 != work_icc_sha
                or backgrounds[0].asset_id != background_pin.portable_asset_id):
            raise ValueError('HOLDOUT_BACKGROUND_ASSET_BINDING_MISMATCH')

    needs_beauty = any(style.beauty.enabled for style in
                       (styles.baseline, styles.candidate, styles.approved))
    if needs_beauty:
        from photo_worker.operations.session.contracts import FaceObservations
        if (artifacts is None or binding is None or
                not isinstance(observations, FaceObservations) or
                observations.binding != binding):
            raise ValueError('HOLDOUT_BEAUTY_FACE_BINDING')
        observations = observations.model_copy(deep=True)

    for style, target in ((styles.baseline, workspace.baseline),
                          (styles.candidate, workspace.candidate),
                          (styles.approved, workspace.approved)):
        check_active()
        require_same_masks()
        require_same_repair()
        require_same_series()
        from contextlib import nullcontext
        restoration = style.extensions.restoration
        data = style.model_dump(mode='python', by_alias=True)
        if style.beauty.enabled:
            data['beauty'] = {'enabled': False}
        if reviewed_source_rgb is not None:
            data['defects'] = {'enabled': False}
        if restoration is not None:
            if reviewed_dust is None and prepare_source is None:
                raise ValueError('HOLDOUT_DENOISE_INSTALLATION_REQUIRED')
            data['extensions']['restoration'] = None
        if style.extensions.background is not None:
            data['extensions']['background'] = None
        if reviewed_detail is not None:
            data['extensions']['portrait_detail'] = None
        color_style = type(style).model_validate(data)
        source_scope = (prepare_source(style) if restoration is not None and
                        reviewed_dust is None
                        else nullcontext(reviewed_source_rgb if reviewed_source_rgb is not None
                                         else workspace.source.rgb))
        with source_scope as color_source:
            check_active()
            render_materialized_style(
                color_source, target, color_style,
                person_reliable=person_reliable, features=features,
                coverage_counts=coverage_counts, person_mask=person_mask,
                region_masks=region_masks,
                series_correction=series_correction,
                series_item_id=series_item_id, check_active=check_active)
        if reviewed_dust is not None:
            reviewed_dust(target)
        check_active()
        if style.beauty.enabled:
            render_holdout_beauty(artifacts, workspace.source, target, style,
                                  observations, skin_probability=skin_probability, landmarks=landmarks,
                                  check_active=check_active)
        check_active()
        if reviewed_detail is not None:
            reviewed_detail(target)
        check_active()
        if style.extensions.background is not None:
            from .background import render_holdout_background
            if style.extensions.background.mode == 'asset':
                from .background_asset import render_holdout_asset_background
                render_holdout_asset_background(workspace.source, target, workspace.alpha,
                    style, background_style_ref, background_pin, background_asset,
                    subject=background_subject, confidence=background_confidence,
                    work_icc_sha=work_icc_sha, check_active=check_active)
            else:
                render_holdout_background(workspace.source, target, workspace.alpha, style,
                    subject=background_subject, confidence=background_confidence,
                    depth=background_depth, artifacts=artifacts,
                    check_active=check_active)
            workspace.alpha.flush()
        target.flush()
        check_active()
        require_same_masks()
        require_same_repair()
        require_same_series()


@contextmanager
def open_holdout_workspace(artifacts: TaskArtifacts, case: RenderCase,
                           expected_work_icc_sha: str,
                           check_active: Callable[[], None],
                           ) -> Iterator[HoldoutWorkspace]:
    """Reserve all frame buffers before rendering and keep them mapped.

    The caller must fill all three RGB outputs before yielding their views to
    evaluation. Alpha is copied tilewise from the pinned source once.
    """
    with open_holdout_source(artifacts, case, expected_work_icc_sha,
                             check_active) as source:
        height, width = source.rgb.shape[:2]
        pixels = height * width
        # Three RGB32 frames plus one alpha32 frame and NPY headers.
        with artifacts.scratch.reserve(pixels * 40 + 16384) as reservation:
            mapped: list[np.memmap] = []
            with ExitStack() as buffers:
                def release(buffer):
                    try:
                        buffer.flush()
                    finally:
                        buffer._mmap.close()

                for name, shape in (
                    ('holdout-baseline', (height, width, 3)),
                    ('holdout-candidate', (height, width, 3)),
                    ('holdout-approved', (height, width, 3)),
                    ('holdout-alpha', (height, width)),
                ):
                    check_active()
                    path = reservation.path(name, '.npy')
                    buffer = np.lib.format.open_memmap(
                        path, mode='w+', dtype='<f4', shape=shape)
                    buffers.callback(release, buffer)
                    mapped.append(buffer)
                alpha = mapped[3]
                for y in range(0, height, 512):
                    check_active()
                    sl = np.s_[y:y + 512, :]
                    if source.alpha is None:
                        alpha[sl] = 1
                    else:
                        alpha[sl] = (source.alpha[sl].astype(np.float32) /
                                     np.float32(65535)) if source.alpha.dtype==np.uint16 else source.alpha[sl]
                check_active()
                reservation.validate()
                yield HoldoutWorkspace(source, mapped[0], mapped[1],
                                       mapped[2], alpha)
                check_active()
                reservation.validate()
