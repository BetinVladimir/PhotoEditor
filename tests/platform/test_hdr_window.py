from __future__ import annotations

import json
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.hdr import window_pull
from photo_worker.contracts.hdr import WindowParams
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.hdr.window_pull import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_background_compose import Artifacts as ComposeArtifacts, context as compose_context


def test_window_pull_matches_exposure_in_linear_light_and_preserves_outside():
    source = np.full((2, 3, 3), .8, np.float32)
    ratio = 4.
    dark = np.power(np.power(source, np.float32(2.4)) / ratio, np.float32(1 / 2.4), dtype=np.float32)
    mask = np.array([[0., .5, 1.], [0., 0., 0.]], np.float32)
    output, changed, clipped = window_pull(source, dark, mask, .04, .01)
    assert np.array_equal(output[mask == 0], source[mask == 0])
    assert np.allclose(output[mask > 0], source[mask > 0], atol=2e-7)
    assert changed == 2 and clipped == 0


def test_all_exposures_clipped_is_counted():
    source = np.ones((2, 2, 3), np.float32)
    output, changed, clipped = window_pull(source, source, np.ones((2, 2), np.float32), 2., 1.)
    assert changed == clipped == 4 and np.all(output == 1)


class Artifacts(ComposeArtifacts):
    def __init__(self, root, empty=False, clipped=False):
        root.mkdir(parents=True, exist_ok=True)
        super().__init__(root, "solid", source_alpha=False)
        if clipped:
            self.rgb.fill(1)
            source_pixels = self.array(root, "clipped-source", self.rgb)
            self.image = self.image.model_copy(update={"pixels": source_pixels, "alpha": None})
            self.image_ref = self.add(self.image)
            dark_array = np.ones_like(self.rgb)
        else:
            dark_array = np.power(np.power(self.rgb, np.float32(2.4)) / np.float32(4),
                                  np.float32(1 / 2.4), dtype=np.float32)
        dark_pixels = self.array(root, "dark", dark_array)
        dark = self.image.model_copy(update={"pixels": dark_pixels, "alpha": None})
        self.dark_ref = self.add(dark); self.dark_pixels = dark_pixels; self.dark_array = dark_array
        mask_array = np.zeros(self.rgb.shape[:2], np.float32)
        if not empty: mask_array[5:25, 50:250] = 1
        mask_ref = self.array(root, "window-mask", mask_array)
        self.mask = MaskRef(artifact=mask_ref, canvas=self.image.canvas, transform_chain=self.image.transform_chain,
                            selector="window", confidence=1., model_lock_id=None, protected=False)
        self.mask_array = mask_array
        self.raw = WindowParams(image=self.image_ref, dark=self.dark_ref, mask=self.mask,
                                reference_exposure_seconds=.04, dark_exposure_seconds=.01,
                                confirmed=True, tile=256).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))

    def stage(self, path, reservation, role, media_type):
        assert role == "working_rgb"
        return super().stage(path, reservation, role, media_type)


def context(artifacts):
    ctx = compose_context(artifacts); ctx.lease.command.operation = "hdr.window_pull"
    ctx.lease.command.inputs.extend([artifacts.dark_ref, artifacts.dark_pixels, artifacts.mask.artifact])
    return ctx


def test_window_adapter_binds_output_and_report(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute(ctx, artifacts)
    output = artifacts.outputs["working_rgb"]
    assert np.array_equal(output[artifacts.mask_array == 0], artifacts.rgb[artifacts.mask_array == 0])
    assert np.allclose(output[artifacts.mask_array > 0], artifacts.rgb[artifacts.mask_array > 0], atol=2e-7)
    image, report = artifacts.staged[0][1], artifacts.staged[1][1]
    assert image.alpha is None and report.exposure_ratio == 4 and not report.warnings
    assert report.changed_pixels == int(np.count_nonzero(artifacts.mask_array))
    assert len(result.outputs) == 3 and not result.review_required


def test_clipping_requires_review_and_empty_fails_before_upload(tmp_path):
    clipped = Artifacts(tmp_path / "clipped", clipped=True); root = tmp_path / "scratch1"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        clipped.scratch = scratch; result = execute(context(clipped), clipped)
    assert result.review_required and clipped.staged[1][1].warnings == ["all_exposures_clipped"]
    assert clipped.staged[1][1].unrecoverable_fraction == 1

    empty = Artifacts(tmp_path / "empty", empty=True); root = tmp_path / "scratch2"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        empty.scratch = scratch
        with pytest.raises(OperationFailure) as error: execute(context(empty), empty)
    assert error.value.error.code == "MASK_EMPTY" and not empty.uploads


def test_invalid_ratio_missing_input_and_cancel_do_not_publish(tmp_path):
    invalid = Artifacts(tmp_path / "invalid"); invalid.raw["dark_exposure_seconds"] = .08
    with pytest.raises(OperationFailure) as error: execute(context(invalid), invalid)
    assert error.value.error.code == "INPUT_INVALID" and not invalid.uploads

    missing = Artifacts(tmp_path / "missing"); ctx = context(missing); ctx.lease.command.inputs.remove(missing.mask.artifact)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.uploads

    cancelled = Artifacts(tmp_path / "cancelled"); ctx = context(cancelled); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 15: raise RuntimeError("cancelled")
    ctx.check_active = active; root = tmp_path / "scratch3"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        cancelled.scratch = scratch
        with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, cancelled)
        assert not list(scratch.directory.iterdir()) and not cancelled.uploads
