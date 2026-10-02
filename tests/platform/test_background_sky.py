from __future__ import annotations

import json
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.background import replace_sky_tile, sky_gains
from photo_worker.contracts.background import SkyParams
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.background.sky import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_background_compose import Artifacts as ComposeArtifacts, context as compose_context


def test_sky_gain_is_bounded_and_protection_is_exact():
    gains = sky_gains(np.array([10., 1., 4.]), np.array([1., 10., 4.]), 10., .25)
    assert np.array_equal(gains, np.array([1.25, .75, 1.], np.float32))
    source = np.full((5, 5, 3), .4, np.float32)
    asset = np.full((5, 5, 3), .8, np.float32)
    sky = np.ones((5, 5), np.float32)
    protected = np.zeros((5, 5), np.float32); protected[:, 2] = 1
    output, effective = replace_sky_tile(source, asset, sky, protected, np.ones(3, np.float32))
    assert np.array_equal(output[:, 2], source[:, 2])
    assert np.all(effective[:, 2] == 0) and np.all(output[:, :2] > source[:, :2])


class Artifacts(ComposeArtifacts):
    def __init__(self, root, confidence=1.0, empty=False):
        root.mkdir(parents=True, exist_ok=True)
        super().__init__(root, "solid", confidence, source_alpha=False)
        sky_array = np.zeros(self.rgb.shape[:2], np.float32)
        if not empty: sky_array[:20] = 1
        protected_array = np.zeros_like(sky_array); protected_array[:, 100:103] = 1
        sky_ref = self.array(root, "sky-mask", sky_array)
        protected_ref = self.array(root, "sky-protected", protected_array)
        self.sky_mask = MaskRef(artifact=sky_ref, canvas=self.image.canvas, transform_chain=self.image.transform_chain,
                                selector="sky", confidence=confidence, model_lock_id=None, protected=False)
        self.protected = MaskRef(artifact=protected_ref, canvas=self.image.canvas, transform_chain=self.image.transform_chain,
                                 selector="sky_structures", confidence=1., model_lock_id=None, protected=True)
        self.sky_asset_array = np.full_like(self.rgb, [.2, .45, .9])
        asset_pixels = self.array(root, "sky-asset", self.sky_asset_array)
        asset = self.image.model_copy(update={"pixels": asset_pixels, "alpha": None})
        self.asset_ref = self.add(asset); self.asset_pixels = asset_pixels
        self.raw = SkyParams(image=self.image_ref, sky_mask=self.sky_mask, protected=self.protected, sky=self.asset_ref,
                             match_limit=.25, asset_scale=1., position_x=.5, position_y=.5, tile=256).model_dump(mode="json")
        self.sky_array, self.protected_array = sky_array, protected_array

    def params(self, model):
        return model.model_validate_json(json.dumps(self.raw))

    def stage(self, path, reservation, role, media_type):
        assert role == "working_rgb"
        return super().stage(path, reservation, role, media_type)


def context(artifacts):
    ctx = compose_context(artifacts)
    ctx.lease.command.operation = "background.sky"
    ctx.lease.command.inputs.extend([artifacts.sky_mask.artifact, artifacts.protected.artifact,
                                     artifacts.asset_ref, artifacts.asset_pixels])
    ctx.lease.execution_profile["resource_estimate"]["ram_bytes"] = 1_073_741_824
    return ctx


def test_sky_adapter_changes_only_unprotected_sky_and_reports(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts)
    output = artifacts.outputs["working_rgb"]
    unchanged = (artifacts.sky_array == 0) | (artifacts.protected_array == 1)
    assert np.array_equal(output[unchanged], artifacts.rgb[unchanged])
    assert np.any(output[~unchanged] != artifacts.rgb[~unchanged])
    image, report = artifacts.staged[0][1], artifacts.staged[1][1]
    assert image.alpha == artifacts.image.alpha and report.changed_pixels == int(np.count_nonzero(~unchanged))
    assert all(.75 <= gain <= 1.25 for gain in report.gains_rgb)
    assert len(result.outputs) == 3 and not result.review_required
    assert ctx.snapshot_progress() == {"done": 8, "total": 8}


def test_empty_low_confidence_and_missing_protection_fail_before_upload(tmp_path):
    empty = Artifacts(tmp_path / "empty", empty=True); root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        empty.scratch = scratch
        with pytest.raises(OperationFailure) as error: execute(context(empty), empty)
    assert error.value.error.code == "MASK_EMPTY" and not empty.uploads

    low = Artifacts(tmp_path / "low", confidence=.7)
    with pytest.raises(OperationFailure) as error: execute(context(low), low)
    assert error.value.error.code == "INPUT_INVALID" and not low.uploads

    missing = Artifacts(tmp_path / "missing"); ctx = context(missing)
    ctx.lease.command.inputs.remove(missing.protected.artifact)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.uploads


def test_cancelled_sky_removes_scratch_output(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 20: raise RuntimeError("cancelled")
    ctx.check_active = active
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, artifacts)
        assert not list(scratch.directory.iterdir()) and not artifacts.uploads
