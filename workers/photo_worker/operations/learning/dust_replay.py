"""Reviewed sensor-dust replay on each independently rendered V23 arm.

The Evaluate lease must authorize the evidence and each referenced object
before this module is called. The same sensor coordinates are repaired on the
baseline, candidate and approved color outputs; source pixels are never used
as a substitute for an independently rendered arm.
"""
from __future__ import annotations

from contextlib import contextmanager
from copy import deepcopy

import numpy as np

from photo_worker.contracts.dust import DustCandidates, DustRepairParams, accepted_dust_candidates
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure

from .dust_evidence import (FrozenDustHoldoutEvidence,
                            validate_frozen_dust_material,
                            validate_holdout_dust_binding)


def _reviewed_dust_styles(styles) -> None:
    modules = []
    for style in (styles.baseline, styles.candidate, styles.approved):
        restoration = style.extensions.restoration
        if (restoration is None or restoration.sensor_dust != 'review' or
                restoration.denoise != 'off' or restoration.upscale != 'off'):
            raise ValueError('HOLDOUT_DUST_STYLE_MISMATCH')
        modules.append(restoration.model_dump() if hasattr(restoration, 'model_dump')
                       else vars(restoration))
    if any(module != modules[0] for module in modules[1:]):
        raise ValueError('HOLDOUT_DUST_STYLE_MISMATCH')


def _admit_holdout_dust(profile, pixels: int) -> None:
    """Check a sequential dust peak inside Evaluate's retained workspace.

    Evaluate seals the sum of all holdout pixel counts, whereas render job
    admission seals one image and reserves an image output. Reusing the latter
    would reject every multi-image learning run.
    """
    estimate = profile.get('resource_estimate') if isinstance(profile, dict) else None
    if (not isinstance(profile, dict) or
            profile.get('backend') != 'cuda' or
            profile.get('renderer_version') != 'v2.2' or
            type(pixels) is not int or not 1 <= pixels <= 200_000_000 or
            not isinstance(estimate, dict) or
            any(type(estimate.get(key)) is not int or
                not 0 <= estimate[key] <= 2**53-1 for key in
                ('pixel_count', 'ram_bytes', 'scratch_bytes',
                 'vram_bytes', 'output_bytes')) or
            estimate['pixel_count'] < pixels or
            estimate['vram_bytes'] > 10_000_000_000):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['ram_bytes'] < 128*pixels + 512*1_048_576:
        raise OperationFailure('RAM_ADMISSION_EXCEEDED')
    # The three RGB arms and alpha map stay reserved while Telea writes one
    # additional RGB32 NPY result. Both reservations overlap for each arm.
    if estimate['scratch_bytes'] < 52*pixels + 32768:
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')


def _copy_measurable_dust(target: np.ndarray, repaired: np.ndarray,
                          source: np.ndarray, protected: np.ndarray,
                          check_active=lambda: None) -> None:
    """Keep original protection bits and require an observable edit outside it."""
    if (not all(isinstance(value, np.ndarray) and value.dtype == np.float32
                for value in (target, repaired, source, protected)) or
            target.shape != repaired.shape or target.shape != source.shape or
            target.ndim != 3 or target.shape[2] != 3 or
            protected.shape != target.shape[:2] or
            np.shares_memory(target, source) or
            np.shares_memory(target, repaired)):
        raise ValueError('HOLDOUT_DUST_ARM_INVALID')
    h, w = target.shape[:2]
    changed = False
    for y in range(0, h, 256):
        for x in range(0, w, 256):
            check_active()
            part = np.s_[y:y+256, x:x+256]
            guard = protected[part] > 0
            before, after = target[part], repaired[part]
            editable = ~guard
            if np.any(editable) and not np.array_equal(
                    before[editable].view(np.uint32),
                    after[editable].view(np.uint32)):
                changed = True
            target[part] = after
            target[part][guard] = source[part][guard]
            if not np.array_equal(target[part][guard].view(np.uint32),
                                  source[part][guard].view(np.uint32)):
                raise ValueError('HOLDOUT_DUST_PROTECTION_CHANGED')
    if not changed:
        raise ValueError('HOLDOUT_DUST_REPAIR_NOT_MEASURABLE')


@contextmanager
def open_holdout_reviewed_dust(artifacts, item, workspace, styles, *,
                               execution_profile, check_active=lambda: None):
    """Yield a bounded repair callback for the three color-stage outputs."""
    check_active()
    case = item.render_case
    if case.dust_evidence is None:
        raise ValueError('HOLDOUT_DUST_EVIDENCE_REQUIRED')
    evidence = artifacts.contract(case.dust_evidence,
                                  FrozenDustHoldoutEvidence).model_copy(deep=True)
    validate_holdout_dust_binding(case, item.event_id, evidence)
    _reviewed_dust_styles(styles)
    params = artifacts.contract(evidence.params, DustRepairParams).model_copy(deep=True)
    proposal = artifacts.contract(evidence.proposal, DustCandidates).model_copy(deep=True)
    stage = artifacts.contract(evidence.stage_image, WorkingImage).model_copy(deep=True)
    selected_ids = validate_frozen_dust_material(evidence, params, proposal)
    selected = tuple(accepted_dust_candidates(params, proposal, evidence.selection))
    source = workspace.source
    if (stage.canvas != source.image.canvas or
            stage.transform_chain != source.image.transform_chain or
            stage.color != source.image.color or stage.dtype != 'float32' or
            tuple(candidate.id for candidate in selected) != selected_ids or
            source.protected.shape != source.rgb.shape[:2]):
        raise ValueError('HOLDOUT_DUST_SOURCE_CHANGED')
    h, w = source.rgb.shape[:2]
    pixels = h * w
    profile = deepcopy(execution_profile)
    _admit_holdout_dust(profile, pixels)
    allowed = {id(workspace.baseline), id(workspace.candidate),
               id(workspace.approved)}
    if len(allowed) != 3:
        raise ValueError('HOLDOUT_DUST_ARM_INVALID')
    used: set[int] = set()

    def repair(target: np.ndarray) -> None:
        check_active()
        if (not isinstance(target, np.ndarray) or target.dtype != np.float32 or
                target.shape != source.rgb.shape or
                np.shares_memory(target, source.rgb) or
                id(target) not in allowed or id(target) in used):
            raise ValueError('HOLDOUT_DUST_ARM_INVALID')
        used.add(id(target))
        with prepare_dust_repair(target, proposal.raw_crop, selected,
                                 artifacts.scratch, check_active=check_active) as prepared:
            if (tuple(candidate.candidate_id for candidate in prepared.candidates) != selected_ids or
                    any(candidate.applied_pixels < 1 for candidate in prepared.candidates)):
                raise ValueError('HOLDOUT_DUST_REPAIR_NOT_MEASURABLE')
            with read_array(prepared.path, (h, w, 3), 'float32', check_active,
                            unit_interval=True) as repaired:
                _copy_measurable_dust(target, repaired, source.rgb,
                                      source.protected, check_active)
            if isinstance(target, np.memmap):
                target.flush()
        check_active()

    yield repair
    if len(used) != 3:
        raise ValueError('HOLDOUT_DUST_ARMS_INCOMPLETE')
    check_active()
