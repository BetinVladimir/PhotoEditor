from __future__ import annotations

from contextlib import contextmanager
import hashlib
import json
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.hdr import motion_residual
from photo_worker.algorithms.registration import register_mtb_translation
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.hdr import BracketTransforms, HDRRegisterParams
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.hdr.register import execute
from photo_worker.runtime.executor import OperationFailure
from test_asset_adapters import context_for as base_context
from test_registration import scene


def test_motion_residual_normalizes_exposure_and_detects_motion():
    reference = scene() * np.float32(.35)
    source = np.power(np.power(reference, np.float32(2.4)) / np.float32(2),
                      np.float32(1 / 2.4), dtype=np.float32)
    assert motion_residual(source, reference, np.eye(3), .01, .02) < 1e-4
    moved = source.copy(); moved[80:140, 100:180] = 1
    assert motion_residual(moved, reference, np.eye(3), .01, .02) > .02


def test_mtb_fallback_is_exposure_invariant_translation():
    reference=scene()
    source=np.zeros_like(reference);source[3:,5:]=np.power(np.power(reference[:-3,:-5],np.float32(2.4))*.5,np.float32(1/2.4))
    result=register_mtb_translation(source,reference)
    matrix=result.before_to_after
    assert result.warnings==('mtb_fallback',) and result.overlap>=.9
    assert abs(matrix[0,2]+5)<=1 and abs(matrix[1,2]+3)<=1


class Artifacts:
    def __init__(self, root, moving=False):
        root.mkdir(parents=True, exist_ok=True)
        self.paths = {}; self.manifests = {}; self.staged = []
        reference = scene() * np.float32(.35)
        exposures = [.01, .02, .04]
        refs = []
        for index, exposure in enumerate(exposures):
            linear = np.power(reference, np.float32(2.4)) * np.float32(exposure / .02)
            rgb = np.power(np.clip(linear, 0, 1), np.float32(1 / 2.4), dtype=np.float32)
            if moving and index == 2: rgb[80:140, 100:180] = 1
            path = root / f"frame-{index}.npy"; np.save(path, rgb)
            pixels = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            image = WorkingImage(pixels=pixels,
                canvas=Canvas(width=384, height=256, orientation=1, space_id=f"bracket:{index}", source_sha256=pixels.sha256),
                color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="a" * 64, encoding="gamma24"),
                dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
            ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(image)).hexdigest())
            self.paths[pixels.artifact_id] = path; self.manifests[ref.artifact_id] = image; refs.append(ref)
        self.refs = refs
        self.raw = HDRRegisterParams(frames=[{"image": ref, "exposure_seconds": exposure} for ref, exposure in zip(refs, exposures)],
                                     reference_index=1, wb_recipe="camera_common_v1", seed=19).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self, manifest, role):
        self.staged.append((role, manifest))
        return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())


def context(artifacts):
    ctx = base_context(artifacts.refs[0], "hdr.register")
    ctx.lease.command.inputs = [*artifacts.refs, *[artifacts.manifests[ref.artifact_id].pixels for ref in artifacts.refs]]
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 536_870_912}
    return ctx


def test_register_adapter_publishes_ordered_bracket_evidence(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts)
    result = execute(ctx, artifacts)
    role, manifest = artifacts.staged[0]
    assert role == "bracket_transforms" and len(result.outputs) == 1
    assert manifest.reference_index == 1 and manifest.transforms[1].reference
    assert [item.frame_index for item in manifest.transforms] == [0, 1, 2]
    assert np.allclose(np.array(manifest.transforms[1].to_reference).reshape(3, 3), np.eye(3))
    assert all(item.overlap >= .9 for item in manifest.transforms)
    assert max(item.motion_fraction for item in manifest.transforms) < .01
    assert not result.review_required and ctx.snapshot_progress() == {"done": 7, "total": 7}
    BracketTransforms.model_validate_json(canonical_bytes(manifest))


def test_motion_sets_review_and_invalid_metadata_fails_before_stage(tmp_path):
    moving = Artifacts(tmp_path / "moving", moving=True)
    result = execute(context(moving), moving)
    assert result.review_required and "motion_detected" in moving.staged[0][1].warnings

    duplicate = Artifacts(tmp_path / "duplicate")
    duplicate.raw["frames"][2]["image"] = duplicate.raw["frames"][0]["image"]
    with pytest.raises(OperationFailure) as error: execute(context(duplicate), duplicate)
    assert error.value.error.code == "INPUT_INVALID" and not duplicate.staged

    missing = Artifacts(tmp_path / "missing"); ctx = context(missing)
    ctx.lease.command.inputs.remove(missing.manifests[missing.refs[0].artifact_id].pixels)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.staged


def test_registration_cancellation_does_not_stage(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 20: raise RuntimeError("cancelled")
    ctx.check_active = active
    with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, artifacts)
    assert not artifacts.staged
