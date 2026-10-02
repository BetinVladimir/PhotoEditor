from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.hdr import merge_debevec_tile, reinhard_global_tile
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.hdr import (BracketFrame, BracketTransform, BracketTransforms,
                                        HDRMergeParams, HDRReport)
from photo_worker.operations.hdr.merge import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for as base_context
from test_hdr_register import Artifacts as RegisterArtifacts


def test_debevec_tile_deghost_and_reinhard_are_finite():
    reference = np.full((8, 8, 3), .4, np.float32)
    base_linear = np.power(reference, np.float32(2.4))
    frames = [np.power(base_linear * np.float32(exposure / .02), np.float32(1 / 2.4), dtype=np.float32)
              for exposure in (.01, .02, .04)]
    frames[2][2:5, 2:5] = 1
    response = np.repeat(np.linspace(0, 1, 256, dtype=np.float32)[:, None], 3, axis=1)
    merged, motion, clipped = merge_debevec_tile(frames, [np.ones((8, 8), bool)] * 3,
                                                  [.01, .02, .04], response, 1)
    assert motion[3, 3] and not motion[0, 0]
    assert np.isfinite(merged).all() and not clipped[0, 0]
    mapped = reinhard_global_tile(merged, .2)
    assert mapped.dtype == np.float32 and np.isfinite(mapped).all() and np.all((0 <= mapped) & (mapped <= 1))


class Artifacts:
    def __init__(self, root, mode="debevec"):
        root.mkdir(parents=True, exist_ok=True)
        source = RegisterArtifacts(root)
        self.paths = source.paths; self.manifests = source.manifests; self.staged = []; self.uploads = []
        self.refs = source.refs
        frames = [BracketFrame(image=ref, exposure_seconds=exposure)
                  for ref, exposure in zip(self.refs, [.01, .02, .04])]
        canvas = self.manifests[self.refs[1].artifact_id].canvas
        transforms = [BracketTransform(frame_index=index, image=ref, to_reference=np.eye(3).ravel().tolist(),
                                       overlap=1., median_reprojection=None if index == 1 else 0.,
                                       inlier_count=0 if index == 1 else 20, motion_fraction=0., reference=index == 1)
                      for index, ref in enumerate(self.refs)]
        registration = BracketTransforms(frames=frames, reference_index=1, reference_canvas=canvas,
                                         wb_recipe="camera_common_v1", transforms=transforms, warnings=[])
        self.registration_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(registration)).hexdigest())
        self.manifests[self.registration_ref.artifact_id] = registration
        self.raw = HDRMergeParams(registration=self.registration_ref, mode=mode, tile=256).model_dump(mode="json")
        self.reference = self.manifests[self.refs[1].artifact_id]

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path); assert role == "working_rgb"
        self.output = np.load(path, allow_pickle=False)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        self.uploads.append(ref); return ref
    def stage_contract(self, manifest, role):
        assert not list(self.scratch.directory.iterdir())
        self.staged.append((role, manifest))
        ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())
        self.manifests[ref.artifact_id] = manifest; return ref


def context(artifacts):
    ctx = base_context(artifacts.registration_ref, "hdr.merge")
    ctx.lease.command.inputs = [artifacts.registration_ref, *artifacts.refs,
                                *[artifacts.manifests[ref.artifact_id].pixels for ref in artifacts.refs]]
    ctx.lease.execution_profile.update(resource_estimate={"ram_bytes": 1_073_741_824}, tile_size=256)
    return ctx


@pytest.mark.parametrize("mode", ["debevec", "mertens"])
def test_merge_adapter_publishes_mode_specific_provenance(tmp_path, mode):
    artifacts = Artifacts(tmp_path, mode); ctx = context(artifacts); root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 10_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute(ctx, artifacts)
    assert artifacts.output.shape == (256, 384, 3) and artifacts.output.dtype == np.float32
    assert np.isfinite(artifacts.output).all() and np.all((0 <= artifacts.output) & (artifacts.output <= 1))
    image, report = artifacts.staged[0][1], artifacts.staged[1][1]
    assert image.canvas == artifacts.reference.canvas and image.alpha is None
    assert report.mode == mode and report.radiometric == (mode == "debevec")
    assert report.response_calibrated == (mode == "debevec")
    assert ("non_radiometric_mertens" in report.warnings) == (mode == "mertens")
    assert len(result.outputs) == 3
    HDRReport.model_validate_json(canonical_bytes(report))


def test_missing_input_and_cancel_do_not_publish(tmp_path):
    missing = Artifacts(tmp_path / "missing"); ctx = context(missing)
    ctx.lease.command.inputs.remove(missing.manifests[missing.refs[0].artifact_id].pixels)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.uploads

    cancelled = Artifacts(tmp_path / "cancelled"); ctx = context(cancelled); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 20: raise RuntimeError("cancelled")
    ctx.check_active = active; root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 10_000_000, min_free_bytes=0) as scratch:
        cancelled.scratch = scratch
        with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, cancelled)
        assert not list(scratch.directory.iterdir()) and not cancelled.uploads
