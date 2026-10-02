from __future__ import annotations

from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import hashlib
import json
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.model_delta import transfer_model_delta, work_to_model
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.contracts.restoration import DenoiseParams, DenoiseReport
from photo_worker.operations.image.denoise import execute
from photo_worker.operations.image.upscale import execute as execute_upscale
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext, OperationFailure
from photo_worker.runtime.scratch import ScratchSpace


LOCK = "a" * 64


def test_model_delta_identity_strength_and_unsafe_gamut():
    source = np.full((12, 13, 3), .45, np.float32)
    model, clipped = work_to_model(source)
    identity, evidence = transfer_model_delta(source, model, model, .8)
    assert not clipped.any() and not evidence.disabled.any()
    assert np.array_equal(identity, source)
    changed, evidence = transfer_model_delta(source, model, np.clip(model + .03, 0, 1), 1)
    assert np.isfinite(changed).all() and changed.mean() > source.mean() and not evidence.disabled.any()
    zero, _ = transfer_model_delta(source, model, np.clip(model + .03, 0, 1), 0)
    assert np.array_equal(zero, source)
    saturated = source.copy(); saturated[..., 0] = 1
    model, _ = work_to_model(saturated)
    protected, evidence = transfer_model_delta(saturated, model, np.zeros_like(model), 1)
    assert evidence.source_clipped.all() and np.array_equal(protected, saturated)


class Engine:
    def __init__(self, delta=.02): self.delta = delta; self.calls = []
    def infer(self, rgb):
        self.calls.append(rgb.shape)
        return np.clip(rgb + self.delta, 0, 1)


class Pool:
    def __init__(self, lock=LOCK, model="scunet_color_real_psnr"): self.lock = SimpleNamespace(lock_id=lock); self.engine = Engine(); self.model=model
    @contextmanager
    def acquire(self, model_id, backend):
        assert model_id == self.model and backend == "cuda"
        yield self.engine


class Artifacts:
    def __init__(self, root, *, strength=.7, lock=LOCK, alpha=False):
        self.source = np.full((20, 24, 3), .4, np.float32)
        source_path = root / "source.npy"; np.save(source_path, self.source, allow_pickle=False)
        self.pixel_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(source_path.read_bytes()).hexdigest())
        self.paths = {self.pixel_ref.artifact_id: source_path}; self.uploads = []; self.staged = []
        canvas = Canvas(width=24, height=20, orientation=1, space_id="source", source_sha256="b" * 64)
        color = ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="c" * 64, encoding="gamma24")
        alpha_ref = ArtifactRef(artifact_id=uuid4(), sha256="d" * 64) if alpha else None
        self.image = WorkingImage(pixels=self.pixel_ref, canvas=canvas, color=color, dtype="float32",
                                  alpha=alpha_ref, transform_chain=TransformChain.create([]))
        self.image_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.manifests = {self.image_ref.artifact_id: self.image}
        self.raw = DenoiseParams(image=self.image_ref, strength=strength, model_lock_id=lock, tile=256).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path); self.output = np.load(path, allow_pickle=False)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        self.uploads.append(ref); return ref
    def stage_contract(self, value, role):
        self.staged.append((role, value)); return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def context(artifacts):
    now = datetime.now(timezone.utc)
    command = SimpleNamespace(inputs=[artifacts.image_ref, artifacts.pixel_ref] + ([artifacts.image.alpha] if artifacts.image.alpha else []))
    lease = Lease(command=command, worker_id=uuid4(), fence=1, until=now + timedelta(minutes=5),
                  deadline=now + timedelta(minutes=5), execution_profile={"backend":"cuda", "resource_estimate":{"ram_bytes":1073741824}}, input_digest="e"*64)
    return ExecutionContext(lease)


def test_denoise_adapter_tiles_preserves_metadata_and_reports(tmp_path):
    artifacts = Artifacts(tmp_path); pool = Pool(); ctx = context(artifacts)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 1_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute(ctx, artifacts, pool, "cuda")
    assert pool.engine.calls == [(20, 24, 3)]
    assert artifacts.output.shape == artifacts.source.shape and artifacts.output.mean() > artifacts.source.mean()
    image = artifacts.staged[0][1]; report = artifacts.staged[1][1]
    assert image.canvas == artifacts.image.canvas and image.color == artifacts.image.color and image.alpha == artifacts.image.alpha
    assert report.backend == "cuda" and report.model_lock_id == LOCK and report.alpha_preserved is False
    DenoiseReport.model_validate_json(canonical_bytes(report)); assert len(result.outputs) == 3


def test_denoise_rejects_lock_before_materializing(tmp_path):
    artifacts = Artifacts(tmp_path, lock="f" * 64); pool = Pool(); ctx = context(artifacts)
    with pytest.raises(OperationFailure) as error: execute(ctx, artifacts, pool, "cuda")
    assert error.value.error.code == "MODEL_LOCK_MISMATCH" and not artifacts.uploads


def test_upscale_adapter_builds_new_canvas_and_transform(tmp_path):
    artifacts = Artifacts(tmp_path)
    artifacts.raw = {"image": artifacts.image_ref.model_dump(mode="json"), "scale": 2,
                     "model_id": "realesrgan_x2", "model_lock_id": LOCK,
                     "color_adapter_id": "work_srgb_delta_v1", "tile": 256, "overlap": 32}
    pool = Pool(model="realesrgan_x2")
    def infer(rgb):
        import cv2
        return cv2.resize(rgb, (rgb.shape[1] * 2, rgb.shape[0] * 2), interpolation=cv2.INTER_CUBIC).clip(0, 1)
    pool.engine.infer = infer
    ctx = context(artifacts); root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute_upscale(ctx, artifacts, pool, "cuda")
    image = artifacts.staged[0][1]; report = artifacts.staged[1][1]
    assert artifacts.output.shape == (40, 48, 3) and image.canvas.width == 48 and image.canvas.height == 40
    assert len(image.transform_chain.steps) == 1 and image.transform_chain.steps[0].kind == "resize"
    assert report.scale == 2 and report.output_pixel_count == 1920 and len(result.outputs) == 3
