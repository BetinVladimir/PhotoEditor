"""Reviewed repair keeps protected original bits before holdout rendering."""
from copy import deepcopy
from dataclasses import dataclass
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.operations.learning.defect_replay import (
    validate_repaired_holdout_pixels,
)
from photo_worker.operations.learning.recipe import FrozenHoldoutStyles
from photo_worker.operations.learning.workspace import (
    HoldoutWorkspace, _reviewed_rgb_fingerprint, render_holdout_triplet,
)


def test_reviewed_holdout_repair_preserves_protected_source_bits():
    original = np.full((2, 3, 3), np.float32(.5), dtype=np.float32)
    repaired = original.copy()
    repaired[0, 0] = .75
    protected = np.zeros((2, 3), dtype=np.float32)
    protected[1, 1] = 1
    validate_repaired_holdout_pixels(original, repaired, protected)
    repaired[1, 1, 0] = np.nextafter(repaired[1, 1, 0], np.float32(1))
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_PROTECTION_CHANGED'):
        validate_repaired_holdout_pixels(original, repaired, protected)


def test_reviewed_holdout_repair_rejects_alias_and_invalid_range():
    original = np.zeros((1, 2, 3), dtype=np.float32)
    protected = np.zeros((1, 2), dtype=np.float32)
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE'):
        validate_repaired_holdout_pixels(original, original, protected)
    repaired = original.copy()
    repaired[0, 0, 0] = np.nan
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_PROTECTION_CHANGED'):
        validate_repaired_holdout_pixels(original, repaired, protected)


def test_reviewed_holdout_repair_requires_real_unprotected_pixel_change():
    original = np.full((2, 3, 3), np.float32(.5), dtype=np.float32)
    repaired = original.copy()
    protected = np.zeros((2, 3), dtype=np.float32)
    protected[0, 0] = 1
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE'):
        validate_repaired_holdout_pixels(original, repaired, protected)
    repaired[0, 0, 0] = .75
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_PROTECTION_CHANGED'):
        validate_repaired_holdout_pixels(original, repaired, protected)
    repaired[0, 0, 0] = .5
    repaired[1, 2, 0] = np.nextafter(np.float32(.5), np.float32(1))
    validate_repaired_holdout_pixels(original, repaired, protected)


def test_reviewed_holdout_repair_rejects_empty_frame():
    original = np.empty((0, 2, 3), dtype=np.float32)
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPAIR_NOT_MEASURABLE'):
        validate_repaired_holdout_pixels(
            original, original.copy(), np.empty((0, 2), dtype=np.float32))


def test_reviewed_holdout_repair_fingerprint_detects_mutation_across_tiles():
    repaired = np.full((257, 257, 3), np.float32(.5), dtype=np.float32)
    shape = repaired.shape
    before = _reviewed_rgb_fingerprint(repaired, shape, lambda: None)
    repaired[256, 256, 2] = np.nextafter(np.float32(.5), np.float32(1))
    assert _reviewed_rgb_fingerprint(repaired, shape, lambda: None) != before
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH'):
        _reviewed_rgb_fingerprint(repaired[:, :256], shape, lambda: None)


def test_triplet_stops_when_color_stage_mutates_reviewed_repair(monkeypatch, tmp_path):
    @dataclass
    class Defects:
        enabled: bool = True

    class Style:
        def __init__(self):
            self.defects = Defects()
            self.geometry = SimpleNamespace(enabled=False)
            self.beauty = SimpleNamespace(enabled=False)
            self.strength = .7
            self.extensions = SimpleNamespace(
                culling=None, series=None, composition=None,
                portrait_detail=None, inpainting=None, generation=None,
                restoration=None, background=None, region_adjustments=[])

        def model_copy(self, *, deep=False):
            return deepcopy(self)

        def model_dump(self, **_kwargs):
            return {'extensions': {}}

        @classmethod
        def model_validate(cls, _value):
            return cls()

    def mapped(name, shape):
        return np.lib.format.open_memmap(
            tmp_path / f'{name}.npy', mode='w+', dtype='float32', shape=shape)

    rgb = np.full((2, 2, 3), np.float32(.5), dtype=np.float32)
    reviewed = rgb.copy()
    reviewed[0, 0, 0] = .75
    source = SimpleNamespace(rgb=rgb, alpha=np.ones((2, 2), np.float32),
                             protected=np.zeros((2, 2), np.float32),
                             effective_mask=None, region_masks={})
    workspace = HoldoutWorkspace(source, mapped('baseline', rgb.shape),
                                 mapped('candidate', rgb.shape),
                                 mapped('approved', rgb.shape),
                                 mapped('alpha', rgb.shape[:2]))
    calls = []

    def color_stage(input_rgb, output, *_args, **_kwargs):
        calls.append(input_rgb)
        output[:] = input_rgb
        input_rgb[1, 1, 0] = .25

    monkeypatch.setattr('photo_worker.operations.learning.color_render.render_materialized_style',
                        color_stage)
    styles = FrozenHoldoutStyles(Style(), Style(), Style())
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH'):
        render_holdout_triplet(workspace, styles, person_reliable=False,
                               reviewed_source_rgb=reviewed)
    assert len(calls) == 1
