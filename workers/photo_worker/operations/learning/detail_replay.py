"""Replay frozen Apply portrait detail on independent Evaluate color arms.

The scheduler must authorize the evidence document and every nested artifact
version before this module is entered. This code does not admit the effect.
"""
from __future__ import annotations

from contextlib import ExitStack, contextmanager
from copy import deepcopy

import numpy as np

from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_detail import PortraitParams
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.operations.portrait.prepare_detail import prepare_detail, detail_peak
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure

from .detail_evidence import (FrozenPortraitDetailHoldoutEvidence,
                              validate_frozen_detail_material,
                              validate_holdout_detail_binding)


def _same_detail_styles(styles):
    modules = [style.extensions.portrait_detail for style in
               (styles.baseline, styles.candidate, styles.approved)]
    if (any(module is None or not module.enabled for module in modules) or
            any(module != modules[0] for module in modules[1:])):
        raise ValueError('HOLDOUT_DETAIL_STYLE_MISMATCH')
    return modules[0]


def _admit_detail(profile, width: int, height: int, mask_count: int) -> None:
    estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
    peak = detail_peak(width, height)
    pixels = width * height
    if (not isinstance(profile, dict) or
            profile.get('backend') != 'cuda' or
            profile.get('renderer_version') != 'v2.2' or
            not isinstance(estimate, dict) or
            any(type(estimate.get(key)) is not int or
                not 0 <= estimate[key] <= 2**53-1 for key in
                ('pixel_count', 'ram_bytes', 'scratch_bytes',
                 'vram_bytes', 'output_bytes')) or
            estimate['pixel_count'] < pixels or
            type(mask_count) is not int or not 1 <= mask_count <= 160 or
            estimate['vram_bytes'] > 10_000_000_000):
        raise OperationFailure('CONFIG_INVALID')
    # The Evaluate closure includes its retained frame/metric buffers and
    # reserves one sequential detail peak on top of those buffers.
    if estimate['ram_bytes'] < 128*pixels + peak['ram_bytes']:
        raise OperationFailure('RAM_ADMISSION_EXCEEDED')
    # Every immutable NPY mask remains mapped until all three arms finish.
    if estimate['scratch_bytes'] < (144*pixels + peak['scratch_bytes'] +
                                    mask_count*(4*pixels + 8192)):
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')


def _copy_detail_arm(target, prepared, protected, check_active):
    if (not isinstance(target, np.ndarray) or target.dtype != np.float32 or
            prepared.shape != target.shape or
            protected.shape != target.shape[:2] or
            np.shares_memory(target, prepared)):
        raise ValueError('HOLDOUT_DETAIL_ARM_INVALID')
    for y in range(0, target.shape[0], 256):
        check_active()
        part = np.s_[y:y+256]
        guard = protected[part] > 0
        unchanged = target[part][guard].copy()
        if not np.array_equal(prepared[part][guard].view(np.uint32),
                              unchanged.view(np.uint32)):
            raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')
        target[part] = prepared[part]
        if not np.array_equal(target[part][guard].view(np.uint32),
                              unchanged.view(np.uint32)):
            raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')


def _detail_protection(masks, params, protection, source_protected,
                       check_active):
    """Match the frozen source guard, then protect every detail guard."""
    if (not isinstance(source_protected, np.ndarray) or
            source_protected.dtype != np.float32 or
            source_protected.ndim != 2):
        raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')
    h, w = source_protected.shape
    source_mask = masks.get(protection.artifact.artifact_id)
    if (not isinstance(source_mask, np.ndarray) or
            source_mask.shape != (h, w) or source_mask.dtype != np.float32):
        raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')
    for y in range(0, h, 256):
        check_active()
        if not np.array_equal(source_protected[y:y+256].view(np.uint32),
                              source_mask[y:y+256].view(np.uint32)):
            raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')
    protected = np.zeros((h, w), dtype=np.float32)
    for mask in params.protected:
        ref = mask.artifact.artifact_id
        current = masks.get(ref)
        if (not isinstance(current, np.ndarray) or
                current.shape != (h, w) or current.dtype != np.float32):
            raise ValueError('HOLDOUT_DETAIL_PROTECTION_CHANGED')
        check_active()
        np.maximum(protected, current, out=protected)
    return protected


@contextmanager
def open_holdout_portrait_detail(artifacts, item, workspace, styles, *,
                                training_run_id, tenant_id,
                                execution_profile, check_active=lambda: None):
    """Yield an exactly-once callback for baseline, candidate, approved."""
    check_active()
    case = item.render_case
    if case.detail_evidence is None:
        raise ValueError('HOLDOUT_DETAIL_EVIDENCE_REQUIRED')
    style_detail = _same_detail_styles(styles)
    artifacts.require_declared_contract(case.detail_evidence,
        'FrozenPortraitDetailHoldoutEvidence')
    evidence = artifacts.contract(case.detail_evidence,
        FrozenPortraitDetailHoldoutEvidence).model_copy(deep=True)
    validate_holdout_detail_binding(case, item.event_id, training_run_id,
        tenant_id, case.detail_evidence, evidence)
    for ref, contract in ((evidence.image, 'WorkingImage'),
                          (evidence.params, 'PortraitParams'),
                          (evidence.resolved, 'ResolvedStyle'),
                          (evidence.protection, 'MaskRef')):
        artifacts.require_declared_contract(ref, contract)
    params = artifacts.contract(evidence.params, PortraitParams).model_copy(deep=True)
    image = artifacts.contract(evidence.image, WorkingImage).model_copy(deep=True)
    protection = artifacts.contract(evidence.protection, MaskRef).model_copy(deep=True)
    resolved = artifacts.contract(evidence.resolved, ResolvedStyle).model_copy(deep=True)
    validate_frozen_detail_material(evidence, params, image, protection, resolved)
    if (style_detail != resolved.modules.extensions.portrait_detail or
            image.canvas != workspace.source.image.canvas or
            image.transform_chain != workspace.source.image.transform_chain or
            image.color != workspace.source.image.color or
            image.dtype != 'float32'):
        raise ValueError('HOLDOUT_DETAIL_SOURCE_CHANGED')
    source = workspace.source.rgb
    h, w = source.shape[:2]
    _admit_detail(deepcopy(execution_profile), w, h,
                  len(evidence.mask_artifacts))
    allowed = {id(workspace.baseline), id(workspace.candidate),
               id(workspace.approved)}
    if len(allowed) != 3:
        raise ValueError('HOLDOUT_DETAIL_ARM_INVALID')
    used: set[int] = set()
    with ExitStack() as stack:
        masks = {}
        for ref in evidence.mask_artifacts:
            artifacts.require_declared_contract(ref, 'NpyArray')
            path = stack.enter_context(artifacts.input(ref, suffix='.npy')).path
            masks[ref.artifact_id] = stack.enter_context(read_array(
                path, (h, w), 'float32', check_active, unit_interval=True))
        protected = _detail_protection(masks, params, protection,
                                       workspace.source.protected, check_active)

        def replay(target: np.ndarray) -> None:
            check_active()
            if (not isinstance(target, np.ndarray) or target.dtype != np.float32 or
                    target.shape != source.shape or id(target) not in allowed or
                    id(target) in used or np.shares_memory(target, source)):
                raise ValueError('HOLDOUT_DETAIL_ARM_INVALID')
            used.add(id(target))
            with prepare_detail(target, params.regions, style_detail,
                                lambda ref: masks[ref.artifact_id], protected,
                                check_active=check_active) as prepared:
                _copy_detail_arm(target, prepared.rgb, protected, check_active)
            if isinstance(target, np.memmap):
                target.flush()
            check_active()

        yield replay
        if len(used) != 3:
            raise ValueError('HOLDOUT_DETAIL_ARMS_INCOMPLETE')
        check_active()
