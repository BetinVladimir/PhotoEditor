from __future__ import annotations

import json
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.background import blur_background_tile, depth_layer_radii
from photo_worker.contracts.background import BlurParams
from photo_worker.contracts.images import RelativeDepth
from photo_worker.operations.background.blur import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_background_compose import Artifacts as ComposeArtifacts, context as compose_context


def test_layer_radii_are_bounded_and_relative_to_subject():
    radii = depth_layer_radii(1000, 1.0, .5)
    assert len(radii) == 8 and max(radii) <= 20
    assert radii[3] < radii[0] and radii[4] < radii[7]
    assert depth_layer_radii(1000, 0.0, .5) == (0,) * 8


def test_blur_preserves_foreground_and_does_not_sample_it_as_background():
    rgb = np.zeros((41, 41, 3), np.float32)
    rgb[..., 1] = .25
    rgb[14:27, 14:27] = [1., 0., 0.]
    subject = np.zeros((41, 41), np.float32); subject[14:27, 14:27] = 1.
    depth = np.ones((41, 41), np.float32)
    out = blur_background_tile(rgb, subject, depth, (0, 0, 0, 0, 0, 0, 0, 8), (slice(None), slice(None)))
    assert np.allclose(out[16:25, 16:25], rgb[16:25, 16:25], atol=2e-7)
    assert np.max(out[:10, :, 0]) < 1e-5
    assert np.allclose(out[:10, :, 1], .25, atol=2e-6)


class Artifacts(ComposeArtifacts):
    def __init__(self, root, confidence=1.0, empty=False):
        root.mkdir(parents=True, exist_ok=True)
        super().__init__(root, "solid", confidence)
        if empty:
            self.subject_array.fill(0)
            np.save(self.paths[self.subject.artifact.artifact_id], self.subject_array)
        depth_array = np.broadcast_to(np.linspace(0, 1, self.rgb.shape[1], dtype=np.float32), self.rgb.shape[:2]).copy()
        depth_ref = self.array(root, "depth", depth_array)
        self.depth_array = depth_array
        self.depth = RelativeDepth(artifact=depth_ref, float32_shape=[self.rgb.shape[0], self.rgb.shape[1]], invalid_mask=None,
                                   canvas=self.image.canvas, transform_chain=self.image.transform_chain, normalization="minmax_valid")
        self.raw = BlurParams(image=self.image_ref, subject=self.subject, depth=self.depth, strength=.8, tile=256).model_dump(mode="json")

    def params(self, model):
        return model.model_validate_json(json.dumps(self.raw))

    def stage(self, path, reservation, role, media_type):
        assert role == "working_rgb"
        return super().stage(path, reservation, role, media_type)


def context(artifacts):
    ctx = compose_context(artifacts)
    ctx.lease.command.operation = "background.blur"
    ctx.lease.command.inputs.append(artifacts.depth.artifact)
    ctx.lease.execution_profile["resource_estimate"]["ram_bytes"] = 1_073_741_824
    return ctx


def test_blur_adapter_publishes_image_and_evidence(tmp_path):
    artifacts = Artifacts(tmp_path)
    ctx = context(artifacts); root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts)
    assert len(result.outputs) == 3 and not result.review_required
    image, report = artifacts.staged[0][1], artifacts.staged[1][1]
    assert image.alpha == artifacts.image.alpha
    assert report.layers == 8 and report.linear_light and report.maximum_radius <= 13
    assert ctx.snapshot_progress() == {"done": 6, "total": 6}
    # Exact subject pixels are protected even though nearby background changes.
    exact = artifacts.subject_array == 1
    assert np.array_equal(artifacts.outputs["working_rgb"][exact], artifacts.rgb[exact])


def test_low_confidence_empty_and_incomplete_depth_fail_without_upload(tmp_path):
    low = Artifacts(tmp_path / "low", .7)
    with pytest.raises(OperationFailure) as error:
        execute(context(low), low)
    assert error.value.error.code == "INPUT_INVALID" and not low.uploads

    empty = Artifacts(tmp_path / "empty", empty=True)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        empty.scratch = scratch
        with pytest.raises(OperationFailure) as error:
            execute(context(empty), empty)
    assert error.value.error.code == "MASK_EMPTY" and not empty.uploads

    incomplete = Artifacts(tmp_path / "incomplete")
    invalid = incomplete.array(tmp_path / "incomplete", "invalid", np.zeros(incomplete.rgb.shape[:2], np.float32))
    incomplete.raw["depth"]["invalid_mask"] = invalid.model_dump(mode="json")
    ctx = context(incomplete); ctx.lease.command.inputs.append(invalid)
    with pytest.raises(OperationFailure) as error:
        execute(ctx, incomplete)
    assert error.value.error.code == "INPUT_INVALID" and not incomplete.uploads


def test_cancel_removes_blur_output(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 12:
            raise RuntimeError("cancelled")
    ctx.check_active = active
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(RuntimeError, match="cancelled"):
            execute(ctx, artifacts)
        assert not list(scratch.directory.iterdir()) and not artifacts.uploads


def test_prepared_blur_matches_full_frame_across_tile_boundaries(tmp_path):
    from photo_worker.operations.background.prepare_blur import prepare_blur
    height,width=273,519
    rng=np.random.default_rng(17)
    rgb=rng.random((height,width,3),dtype=np.float32)
    subject=np.zeros((height,width),np.float32);subject[90:190,230:290]=1
    depth=np.broadcast_to(np.linspace(0,1,width,dtype=np.float32),(height,width)).copy()
    subject_depth=float(np.sum(depth[subject>=.5],dtype=np.float64))/int(np.count_nonzero(subject>=.5))
    radii=depth_layer_radii(max(height,width),.7,subject_depth)
    expected=blur_background_tile(rgb,subject,depth,radii,(slice(None),slice(None)))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4_000_000,min_free_bytes=0) as scratch:
        with prepare_blur(rgb,subject,depth,scratch,strength=.7,tile=256) as prepared:
            path=prepared.rgb
            np.testing.assert_allclose(np.load(path),expected,atol=3e-6,rtol=0)
            assert prepared.radii==radii
            assert prepared.subject_depth==subject_depth
        assert not path.exists()
