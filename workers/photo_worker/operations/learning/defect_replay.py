"""Pinned reviewed-defect preparation for a V23 holdout source."""
from __future__ import annotations

from contextlib import contextmanager
from typing import Iterator

import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.defects import DefectRepairParams, DefectReview
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import StyleV2
from photo_worker.algorithms.effective import (effective_defect_params,
                                               resolve_effective_params)
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_defects import prepare_pinned_defect_repair

from .contracts import TrainingExample
from .defect_evidence import (FrozenDefectHoldoutEvidence,
                              validate_holdout_defect_binding)
from .workspace import HoldoutWorkspace


def validate_repaired_holdout_pixels(original: np.ndarray,
                                     repaired: np.ndarray,
                                     protected: np.ndarray,
                                     check_active=lambda: None) -> None:
    if (not isinstance(original, np.ndarray) or
            not isinstance(repaired, np.ndarray) or
            not isinstance(protected, np.ndarray) or
            original.dtype != np.float32 or repaired.dtype != np.float32 or
            protected.dtype != np.float32 or original.ndim != 3 or
            original.shape[2] != 3 or not all(original.shape[:2]) or
            repaired.shape != original.shape or
            protected.shape != original.shape[:2] or
            np.shares_memory(repaired, original)):
        raise ValueError('HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE')
    changed = False
    for y in range(0, original.shape[0], 512):
        for x in range(0, original.shape[1], 512):
            check_active()
            part = np.s_[y:y + 512, x:x + 512]
            before, after = original[part], repaired[part]
            if (not np.isfinite(after).all() or
                    np.any(after < 0) or np.any(after > 1) or
                    not np.array_equal(
                        after[protected[part] > 0].view(np.uint32),
                        before[protected[part] > 0].view(np.uint32))):
                raise ValueError('HOLDOUT_DEFECT_PROTECTION_CHANGED')
            editable = protected[part] <= 0
            if np.any(editable) and not np.array_equal(
                    after[editable].view(np.uint32),
                    before[editable].view(np.uint32)):
                changed = True
    check_active()
    if not changed:
        raise ValueError('HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE')


@contextmanager
def open_holdout_reviewed_defect(artifacts, item: TrainingExample,
                                workspace: HoldoutWorkspace, style: StyleV2, *,
                                execution_profile,
                                check_active=lambda: None,
                                ) -> Iterator[np.ndarray]:
    """Retain the qualified repair source without mutating the pinned original.

    The scheduler must already have authorized the evidence through the live
    Evaluate lease. This context verifies its physical task inputs and leaves
    review-required or partially applied repairs ineligible for measurement.
    """
    check_active()
    case = item.render_case
    if case.defect_evidence is None:
        raise ValueError('HOLDOUT_DEFECT_EVIDENCE_REQUIRED')
    evidence = artifacts.contract(case.defect_evidence,
                                  FrozenDefectHoldoutEvidence).model_copy(deep=True)
    validate_holdout_defect_binding(case, item.event_id, evidence)
    params = artifacts.contract(evidence.params, DefectRepairParams).model_copy(deep=True)
    review = artifacts.contract(params.review, DefectReview).model_copy(deep=True)
    protection = artifacts.contract(case.protected, MaskRef).model_copy(deep=True)
    resolved = artifacts.contract(params.resolved, ResolvedStyle).model_copy(deep=True)
    if (not isinstance(style, StyleV2) or not style.defects.enabled or
            resolve_effective_params(resolved).modules.defects !=
            effective_defect_params(style.defects, style.strength) or
            params.image != evidence.image or
            canonical_bytes(review) != canonical_bytes(evidence.selection) or
            canonical_bytes(protection) != canonical_bytes(params.protected) or
            workspace.source.image != artifacts.contract(case.working_image,
                                                        type(workspace.source.image))):
        raise ValueError('HOLDOUT_DEFECT_SOURCE_CHANGED')
    current = AdjustmentPixels(workspace.source.rgb,
        workspace.alpha if workspace.source.alpha is not None else None)
    with prepare_pinned_defect_repair(artifacts, evidence.params, current,
            protection_ref=case.protected, execution_profile=execution_profile,
            expected_params=params, check_active=check_active) as stage:
        check_active()
        accepted = set(evidence.selection.accepted_candidate_ids)
        observed = {candidate.candidate_id for candidate in stage.candidates}
        if (stage.review_required or not accepted or observed != accepted or
                any(candidate.reason is not None for candidate in stage.candidates)):
            raise ValueError('HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE')
        validate_repaired_holdout_pixels(workspace.source.rgb,
            stage.pixels.rgb, workspace.source.protected, check_active)
        yield stage.pixels.rgb
        check_active()
