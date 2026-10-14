from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.background import (alpha_u16, asset_geometry, contact_shadow,
                                                effective_alpha, render_asset_tile, solid, transparent)
from photo_worker.contracts.background import ComposeParams
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.background.compose import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_image_color_adapter import Artifacts as BaseArtifacts, context as base_context


def test_linear_solid_composite_and_alpha_math():
    rgb = np.array([[[.5, .25, 1.0], [.2, .4, .6]]], dtype=np.float32)
    mask = np.array([[.5, 1.0]], dtype=np.float32)
    source = np.array([[32768, 0]], dtype=np.uint16)
    alpha = effective_alpha(mask, source)
    assert alpha[0, 0] == pytest.approx(.2500038, abs=1e-7)
    assert alpha[0, 1] == 0
    result = solid(rgb, alpha, [0., 0., 0.])
    expected = rgb * np.power(alpha[..., None], np.float32(1 / 2.4), dtype=np.float32)
    assert np.allclose(result, expected, atol=2e-7)
    assert np.array_equal(alpha_u16(np.array([[0., .5, 1.]], np.float32)), np.array([[0, 32768, 65535]], np.uint16))


def test_transparent_zeroes_only_fully_hidden_rgb():
    rgb = np.array([[[.2, .3, .4], [.7, .8, .9]]], np.float32)
    alpha = np.array([[0., 1e-6]], np.float32)
    result = transparent(rgb, alpha)
    assert np.array_equal(result[0, 0], np.zeros(3, np.float32))
    assert np.array_equal(result[0, 1], rgb[0, 1])


def test_asset_cover_sampling_and_contact_shadow():
    background = np.zeros((2, 4, 3), np.float32)
    background[:, :, 0] = np.arange(4, dtype=np.float32) / 3
    geometry = asset_geometry(4, 2, 4, 4, 1, .5, .5)
    rendered = render_asset_tile(background, (4, 4), (0, 4, 0, 4), geometry)
    assert rendered.shape == (4, 4, 3) and np.isfinite(rendered).all()
    assert np.all(np.diff(rendered[2, :, 0]) >= 0)
    mask = np.zeros((17, 17), np.float32); mask[5:10, 5:10] = 1
    shadow = contact_shadow(mask, 3, 2, 1, .3, (slice(None), slice(None)))
    assert shadow.max() <= .3 and np.all(shadow[5:10, 5:10] == 0)
    assert np.count_nonzero(shadow) > 0


class Artifacts(BaseArtifacts):
    def __init__(self, root, mode="transparent", confidence=1.0, source_alpha=True):
        super().__init__(root, False)
        if not source_alpha:
            self.image = self.image.model_copy(update={"alpha": None})
            self.image_ref = self.add(self.image)
        self.subject_array = np.linspace(0, 1, self.rgb.shape[0] * self.rgb.shape[1], dtype=np.float32).reshape(self.rgb.shape[:2])
        subject_ref = self.array(root, "subject", self.subject_array)
        self.subject = MaskRef(artifact=subject_ref, canvas=self.image.canvas, transform_chain=self.image.transform_chain,
                               selector="subject", confidence=confidence, model_lock_id=None, protected=False)
        self.raw = ComposeParams(image=self.image_ref, subject=self.subject, mode=mode,
                                 solid_rgb=[.1, .2, .3] if mode == "solid" else None, tile=256).model_dump(mode="json")
        self.outputs = {}

    def params(self, model):
        return model.model_validate_json(json.dumps(self.raw))

    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path)
        assert role in ("working_rgb", "working_alpha") and media_type == "application/octet-stream"
        self.outputs[role] = np.load(path, allow_pickle=False)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        self.uploads.append(ref)
        return ref

    def stage_contract(self, value, role):
        assert not list(self.scratch.directory.iterdir())
        self.staged.append((role, value))
        return self.add(value)


def context(artifacts):
    ctx = base_context(artifacts)
    ctx.lease.command.operation = "background.compose"
    ctx.lease.command.inputs = [artifacts.image.pixels, artifacts.subject.artifact]
    if artifacts.image.alpha:
        ctx.lease.command.inputs.append(artifacts.image.alpha)
    return ctx


@pytest.mark.parametrize("mode", ["transparent", "solid"])
def test_adapter_composes_and_publishes_bound_contracts(tmp_path, mode):
    artifacts = Artifacts(tmp_path, mode)
    ctx = context(artifacts)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts)
    effective = effective_alpha(artifacts.subject_array, np.full(artifacts.rgb.shape[:2], 10000, np.uint16))
    if mode == "transparent":
        assert np.array_equal(artifacts.outputs["working_rgb"], transparent(artifacts.rgb, effective))
        assert np.array_equal(artifacts.outputs["working_alpha"], alpha_u16(effective))
        assert artifacts.staged[0][1].alpha == result.outputs[1]
        assert len(result.outputs) == 4
    else:
        assert np.allclose(artifacts.outputs["working_rgb"], solid(artifacts.rgb, effective, [.1, .2, .3]), atol=2e-7)
        assert artifacts.staged[0][1].alpha is None
        assert len(result.outputs) == 3
    report = artifacts.staged[1][1]
    assert report.linear_light and report.source_alpha_combined and not result.review_required
    assert ctx.snapshot_progress() == {"done": 5, "total": 5}


def test_asset_adapter_uses_declared_background_and_shadow(tmp_path):
    artifacts = Artifacts(tmp_path, "solid", source_alpha=False)
    background_rgb = np.full_like(artifacts.rgb, [.1, .3, .7])
    background_pixels = artifacts.array(tmp_path, "asset", background_rgb)
    background = artifacts.image.model_copy(update={"pixels": background_pixels, "alpha": None})
    background_ref = artifacts.add(background)
    artifacts.raw.update(mode="asset", solid_rgb=None, background=background_ref.model_dump(mode="json"),
                         asset_scale=1., position_x=.5, position_y=.5, shadow_opacity=.2,
                         shadow_blur_fraction=.01, shadow_offset_x=.01, shadow_offset_y=.01)
    ctx = context(artifacts); ctx.lease.command.inputs.extend([background_ref, background_pixels])
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        result = execute(ctx, artifacts)
    report = artifacts.staged[1][1]
    assert len(result.outputs) == 3 and report.asset_scale == 1 and report.contact_shadow
    fully_background = artifacts.subject_array == 0
    assert np.mean(artifacts.outputs["working_rgb"][..., 2][fully_background]) < .7


def test_asset_contract_or_coverage_failure_prevents_pixel_upload(tmp_path):
    artifacts = Artifacts(tmp_path, "solid")
    artifacts.raw.update(mode="asset", solid_rgb=None, background=artifacts.image_ref.model_dump(mode="json"),
                         asset_scale=.5)
    with pytest.raises(OperationFailure) as error:
        execute(context(artifacts), artifacts)
    assert error.value.error.code == "INPUT_INVALID" and not artifacts.uploads


def test_unreliable_mask_rejected_before_materialization(tmp_path):
    artifacts = Artifacts(tmp_path, confidence=.74)
    with pytest.raises(OperationFailure) as error:
        execute(context(artifacts), artifacts)
    assert error.value.error.code == "MASK_UNRELIABLE"
    assert not artifacts.uploads


def test_geometry_mismatch_and_undeclared_inputs_are_rejected(tmp_path):
    artifacts = Artifacts(tmp_path)
    artifacts.raw["subject"]["canvas"]["space_id"] = "other"
    with pytest.raises(OperationFailure) as error:
        execute(context(artifacts), artifacts)
    assert error.value.error.code == "INPUT_INVALID"
    second = tmp_path / "second"; second.mkdir()
    artifacts = Artifacts(second)
    ctx = context(artifacts); ctx.lease.command.inputs.remove(artifacts.subject.artifact)
    with pytest.raises(OperationFailure) as error:
        execute(ctx, artifacts)
    assert error.value.error.code == "INPUT_INVALID"


def test_cancel_cleans_unpublished_output(tmp_path):
    artifacts = Artifacts(tmp_path, "solid", source_alpha=False)
    ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 9:
            raise RuntimeError("cancelled")
    ctx.check_active = active
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch
        with pytest.raises(RuntimeError, match="cancelled"):
            execute(ctx, artifacts)
        assert not list(scratch.directory.iterdir()) and not artifacts.uploads


def test_large_asset_sampler_matches_native_subpixel_and_border():
    import cv2
    from photo_worker.algorithms.background import _large_asset_linear_tile
    background=np.random.default_rng(49).random((9,13,3),dtype=np.float32)
    xs=np.array([-2,-.5,-.015625,.015625,.046875,2.3,7.71,12.2,14],np.float32)
    ys=np.array([-1,-.015625,.015625,.046875,3.31,8.7,10],np.float32)
    mx,my=np.meshgrid(xs,ys)
    expected=cv2.remap(background,mx,my,cv2.INTER_LINEAR,borderMode=cv2.BORDER_REPLICATE)
    np.testing.assert_allclose(_large_asset_linear_tile(background,xs,ys),expected,rtol=0,atol=2e-7)


@pytest.mark.parametrize('vertical',[False,True])
def test_asset_panorama_exceeding_native_dimension_limit(vertical):
    import cv2
    # Small memory but >SHRT_MAX: reproduce the previous native assertion and
    # compare the valid fallback tile against a local native reference.
    background=np.random.default_rng(51).random((2,40000,3),dtype=np.float32)
    if vertical:background=background.transpose(1,0,2).copy()
    h,w=background.shape[:2]
    bounds=(32760,32790,0,2) if vertical else (0,2,32760,32790)
    y0,y1,x0,x1=bounds
    geometry=asset_geometry(w,h,w,h,1,.5,.5)
    actual=render_asset_tile(background,(h,w),bounds,geometry)
    np.testing.assert_array_equal(actual,background[y0:y1,x0:x1])
    # Fractional shifted sampling, including the replicated last source edge.
    bounds=(39970,40000,0,2) if vertical else (0,2,39970,40000)
    y0,y1,x0,x1=bounds
    geometry=(1.,-.3 if not vertical else 0.,-.3 if vertical else 0.)
    actual=render_asset_tile(background,(h,w),bounds,geometry)
    local=background[y0:h,x0:w]
    mx,my=np.meshgrid(np.arange(x1-x0,dtype=np.float32)+(.3 if not vertical else 0),
                      np.arange(y1-y0,dtype=np.float32)+(.3 if vertical else 0))
    expected=cv2.remap(local,mx,my,cv2.INTER_LINEAR,borderMode=cv2.BORDER_REPLICATE)
    np.testing.assert_allclose(actual,expected,rtol=0,atol=2e-7)
