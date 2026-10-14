"""Pinned holdout image and protected-mask materialization for V23 evaluate."""
from __future__ import annotations

from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
from typing import Callable, Iterator

import numpy as np

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import TaskArtifacts

from .contracts import RenderCase


@dataclass(frozen=True)
class HoldoutSource:
    image: WorkingImage
    rgb: np.ndarray
    alpha: np.ndarray | None
    protected: np.ndarray
    effective_mask: np.ndarray | None
    region_masks: dict[str, tuple[MaskRef, np.ndarray]]


@contextmanager
def open_holdout_source(artifacts: TaskArtifacts, case: RenderCase,
                        expected_work_icc_sha: str,
                        check_active: Callable[[], None],
                        ) -> Iterator[HoldoutSource]:
    """Map only the declared, geometry-matched source and mask children.

    TaskArtifacts verifies artifact identity and SHA before read_array checks
    exact NPY layout and bounded values. Mappings cannot escape this scope.
    """
    check_active()
    if case.binding.source != case.working_image:
        raise ValueError('HOLDOUT_SOURCE_BINDING_MISMATCH')
    def contract(ref, model):
        check_active()
        result = artifacts.contract(ref, model)
        check_active()
        return result

    image = contract(case.working_image, WorkingImage)
    protected = contract(case.protected, MaskRef)
    effective = (contract(case.effective_mask, MaskRef)
                 if case.effective_mask is not None else None)
    regions = {binding.region_id: contract(binding.mask, MaskRef)
               for binding in case.region_masks}
    if len(regions) != len(case.region_masks):
        raise ValueError('DUPLICATE_HOLDOUT_REGION_MASK')
    if (image.dtype != 'float32' or
            image.color.profile_id != 'work_rec2020_gamma24_v1' or
            image.color.encoding != 'gamma24' or
            image.color.icc_sha256 != expected_work_icc_sha or
            canonical_hash(image.canvas) != case.binding.canvas_hash or
            image.canvas.width * image.canvas.height > 200_000_000 or
            not protected.protected or
            protected.canvas != image.canvas or
            protected.transform_chain != image.transform_chain):
        raise ValueError('HOLDOUT_SOURCE_GEOMETRY_INVALID')
    if (effective is not None and
            (effective.protected or effective.canvas != image.canvas or
             effective.transform_chain != image.transform_chain)):
        raise ValueError('HOLDOUT_EFFECTIVE_MASK_GEOMETRY_INVALID')
    if any(mask.protected or mask.canvas != image.canvas or
           mask.transform_chain != image.transform_chain
           for mask in regions.values()):
        raise ValueError('HOLDOUT_REGION_MASK_GEOMETRY_INVALID')
    height, width = image.canvas.height, image.canvas.width
    with ExitStack() as stack:
        def mapped(ref, shape, dtype, unit):
            check_active()
            local = stack.enter_context(artifacts.input(ref, suffix='.npy'))
            check_active()
            result = stack.enter_context(read_array(local.path, shape, dtype,
                                                    check_active,
                                                    unit_interval=unit))
            check_active()
            return result
        rgb = mapped(image.pixels, (height, width, 3), 'float32', True)
        alpha=None
        if image.alpha is not None:
            check_active()
            local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
            check_active()
            alpha=stack.enter_context(read_alpha_array(local.path,(height,width),check_active))
            check_active()
        guard = mapped(protected.artifact, (height, width), 'float32', True)
        editable = (mapped(effective.artifact, (height, width), 'float32', True)
                    if effective is not None else None)
        region_pixels = {name: (mask, mapped(mask.artifact,
                         (height, width), 'float32', True))
                         for name, mask in regions.items()}
        check_active()
        yield HoldoutSource(image, rgb, alpha, guard, editable, region_pixels)
        check_active()
