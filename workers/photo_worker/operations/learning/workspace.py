"""Attempt-scoped disk workspace for three V23 holdout renders."""
from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
from typing import Callable, Iterator

import numpy as np

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


def render_holdout_triplet(workspace: HoldoutWorkspace,
                           styles: FrozenHoldoutStyles, *,
                           person_reliable: bool,
                           features: np.ndarray | None = None,
                           coverage_counts: np.ndarray | None = None,
                           person_mask: np.ndarray | None = None,
                           region_masks: dict[str, np.ndarray] | None = None,
                           check_active: Callable[[], None] = lambda: None,
                           ) -> None:
    """Render baseline, candidate and approved through the same V2 source."""
    from .color_render import render_materialized_style

    for style, target in ((styles.baseline, workspace.baseline),
                          (styles.candidate, workspace.candidate),
                          (styles.approved, workspace.approved)):
        check_active()
        render_materialized_style(
            workspace.source.rgb, target, style,
            person_reliable=person_reliable, features=features,
            coverage_counts=coverage_counts, person_mask=person_mask,
            region_masks=region_masks, check_active=check_active)
        target.flush()


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
            try:
                for name, shape in (
                    ('holdout-baseline', (height, width, 3)),
                    ('holdout-candidate', (height, width, 3)),
                    ('holdout-approved', (height, width, 3)),
                    ('holdout-alpha', (height, width)),
                ):
                    path = reservation.path(name, '.npy')
                    mapped.append(np.lib.format.open_memmap(
                        path, mode='w+', dtype='<f4', shape=shape))
                alpha = mapped[3]
                for y in range(0, height, 512):
                    check_active()
                    sl = np.s_[y:y + 512, :]
                    if source.alpha is None:
                        alpha[sl] = 1
                    else:
                        alpha[sl] = (source.alpha[sl].astype(np.float32) /
                                     np.float32(65535)) if source.alpha.dtype==np.uint16 else source.alpha[sl]
                reservation.validate()
                yield HoldoutWorkspace(source, mapped[0], mapped[1],
                                       mapped[2], alpha)
                check_active()
                reservation.validate()
            finally:
                for buffer in mapped:
                    buffer.flush()
                    buffer._mmap.close()
