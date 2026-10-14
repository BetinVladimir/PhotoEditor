"""V23 must compare all holdout arms with the same region probabilities."""

import numpy as np
import pytest
from types import SimpleNamespace

from photo_worker.operations.learning.workspace import (
    _region_mask_fingerprints,
    _require_region_mask_fingerprints,
    _require_independent_holdout_buffers,
)


def test_borrowed_region_mask_mutation_refuses_next_holdout_arm():
    mask = np.zeros((513, 517), dtype=np.float32)
    masks = {'face': mask}
    original = _region_mask_fingerprints(masks, mask.shape, lambda: None)
    _require_region_mask_fingerprints(masks, mask.shape, original, lambda: None)
    mask[512, 516] = np.float32(0.25)
    with pytest.raises(ValueError, match='HOLDOUT_REGION_BINDING_MISMATCH'):
        _require_region_mask_fingerprints(masks, mask.shape, original, lambda: None)


def test_region_mask_fingerprint_refuses_invalid_values_and_cancellation():
    mask = np.zeros((2, 3), dtype=np.float32)
    for changed in (np.float32(np.nan), np.float32(-0.1), np.float32(1.1)):
        mask[1, 2] = changed
        with pytest.raises(ValueError, match='INVALID_HOLDOUT_MASK'):
            _region_mask_fingerprints({'face': mask}, mask.shape, lambda: None)
    mask[1, 2] = 0

    class Cancelled(Exception):
        pass

    def cancelled():
        raise Cancelled()

    with pytest.raises(Cancelled):
        _require_region_mask_fingerprints(
            {'face': mask}, mask.shape,
            _region_mask_fingerprints({'face': mask}, mask.shape, lambda: None),
            cancelled)


def test_holdout_buffers_refuse_shared_comparison_or_input_storage():
    rgb = np.zeros((4, 5, 3), dtype=np.float32)
    protected = np.zeros((4, 5), dtype=np.float32)
    source = SimpleNamespace(rgb=rgb, alpha=None, protected=protected,
                             effective_mask=None, region_masks={})
    workspace = SimpleNamespace(source=source,
        baseline=np.empty_like(rgb), candidate=np.empty_like(rgb),
        approved=np.empty_like(rgb), alpha=np.empty_like(protected))
    _require_independent_holdout_buffers(workspace)
    workspace.candidate = workspace.baseline
    with pytest.raises(ValueError, match='HOLDOUT_WORKSPACE_BUFFER_ALIAS'):
        _require_independent_holdout_buffers(workspace)
    workspace.candidate = np.empty_like(rgb)
    workspace.approved = rgb
    with pytest.raises(ValueError, match='HOLDOUT_WORKSPACE_BUFFER_ALIAS'):
        _require_independent_holdout_buffers(workspace)
    workspace.approved = np.empty_like(rgb)
    workspace.alpha = np.empty((4, 4), dtype=np.float32)
    with pytest.raises(ValueError, match='HOLDOUT_WORKSPACE_BUFFER_INVALID'):
        _require_independent_holdout_buffers(workspace)
