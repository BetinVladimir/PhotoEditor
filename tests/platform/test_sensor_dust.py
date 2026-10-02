from __future__ import annotations

from contextlib import contextmanager
import hashlib
import io
import json
from types import SimpleNamespace
from uuid import uuid4

import cv2
import numpy as np
import pytest

from photo_worker.algorithms.dust import FrameDust, detect_frame_dust, map_to_sensor, repeated_dust
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.dust import (DustCandidates, DustParams, DustRepairParams,
                                         DustReview, accepted_dust_candidates)
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.sensor.dust import execute
from photo_worker.operations.sensor.repair import execute_repair
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for as base_context


def dust_frame(point=(64, 48), include=True):
    image = np.full((96, 128, 3), .72, np.float32)
    if include: cv2.circle(image, point, 3, (.42, .42, .42), -1)
    return image


def test_detector_and_sensor_mapping_find_compact_dark_region():
    found = detect_frame_dust(dust_frame())
    assert found and min(np.hypot(item.x - 64, item.y - 48) for item in found) < 1
    mapped = map_to_sensor(found[0], (96, 128), (100, 200, 256, 192))
    assert 100 <= mapped.x < 356 and 200 <= mapped.y < 392


def test_recurrence_requires_eighty_percent_and_is_ordered():
    common = FrameDust(100., 80., 2.)
    frames = [[common], [FrameDust(100.5, 79.7, 2.1)], [common], [common], [], [FrameDust(10, 10, 1)]]
    assert repeated_dust(frames, .8) == []
    repeated = repeated_dust(frames[:5], .8)
    assert len(repeated) == 1 and repeated[0][1:] == (4, 5)


class Artifacts:
    def __init__(self, root, repeated=True):
        root.mkdir(parents=True, exist_ok=True)
        self.paths = {}; self.manifests = {}; self.staged = []; refs = []
        for index in range(5):
            image_array = dust_frame(include=repeated or index == 0)
            path = root / f"dust-{index}.npy"; np.save(path, image_array)
            pixels = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
            image = WorkingImage(pixels=pixels,
                canvas=Canvas(width=128, height=96, orientation=1, space_id=f"dust:{index}", source_sha256=pixels.sha256),
                color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="a" * 64, encoding="gamma24"),
                dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
            ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(image)).hexdigest())
            self.paths[pixels.artifact_id] = path; self.manifests[ref.artifact_id] = image; refs.append(ref)
        self.refs = refs
        self.raw = DustParams(frames=refs, camera_id="camera-1", sensor_width=256, sensor_height=192,
                              raw_crop={"x": 0, "y": 0, "width": 256, "height": 192},
                              orientation=1, recurrence=.8).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage_contract(self, manifest, role):
        self.staged.append((role, manifest))
        return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(manifest)).hexdigest())


def context(artifacts):
    ctx = base_context(artifacts.refs[0], "sensor.dust")
    ctx.lease.command.inputs = [*artifacts.refs, *[artifacts.manifests[ref.artifact_id].pixels for ref in artifacts.refs]]
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 536_870_912}
    return ctx


def test_sensor_adapter_emits_review_only_repeated_candidates(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); result = execute(ctx, artifacts)
    role, manifest = artifacts.staged[0]
    assert role == "dust_candidates" and len(manifest.candidates) >= 1 and result.review_required
    candidate = min(manifest.candidates, key=lambda item: abs(item.sensor_x - 129))
    assert candidate.observed_frames == candidate.eligible_frames == 5 and candidate.recurrence == 1
    assert candidate.decision == "pending" and ctx.snapshot_progress() == {"done": 6, "total": 6}
    DustCandidates.model_validate_json(canonical_bytes(manifest))


def test_one_off_dust_is_not_promoted_and_invalid_series_fails(tmp_path):
    one = Artifacts(tmp_path / "one", repeated=False); result = execute(context(one), one)
    assert not result.review_required and not one.staged[0][1].candidates
    assert one.staged[0][1].warnings == ["no_repeated_dust"]

    missing = Artifacts(tmp_path / "missing"); ctx = context(missing)
    ctx.lease.command.inputs.remove(missing.manifests[missing.refs[0].artifact_id].pixels)
    with pytest.raises(OperationFailure) as error: execute(ctx, missing)
    assert error.value.error.code == "INPUT_INVALID" and not missing.staged

    bad = Artifacts(tmp_path / "bad"); bad.raw["raw_crop"]["width"] = 300
    with pytest.raises(OperationFailure) as error: execute(context(bad), bad)
    assert error.value.error.code == "INPUT_INVALID" and not bad.staged


def test_sensor_dust_cancellation_does_not_stage(tmp_path):
    artifacts = Artifacts(tmp_path); ctx = context(artifacts); calls = 0
    def active():
        nonlocal calls
        calls += 1
        if calls == 12: raise RuntimeError("cancelled")
    ctx.check_active = active
    with pytest.raises(RuntimeError, match="cancelled"): execute(ctx, artifacts)
    assert not artifacts.staged


def test_dust_review_selection_is_bound_to_proposal_and_frame(tmp_path):
    artifacts = Artifacts(tmp_path); execute(context(artifacts), artifacts)
    proposal = artifacts.staged[0][1]; candidate = proposal.candidates[0]
    proposal_ref = ArtifactRef(artifact_id=uuid4(), sha256="d" * 64)
    review = DustReview(version="sensor_dust_review_v1", proposal=proposal_ref,
                        base=proposal.frames[0], revision=2,
                        accepted_candidate_ids=[candidate.id], rejected_candidate_ids=[])
    params = DustRepairParams(image=proposal.frames[1], proposal=proposal_ref, review=review)
    assert accepted_dust_candidates(params, proposal, review) == [candidate]
    with pytest.raises(ValueError, match="STALE_DUST_REVIEW"):
        accepted_dust_candidates(params.model_copy(update={"proposal": ArtifactRef(artifact_id=uuid4(), sha256="f" * 64)}), proposal, review)


class RepairArtifacts:
    def __init__(self, root, source, image, image_ref, proposal, proposal_ref, review):
        self.params_value = DustRepairParams(image=image_ref, proposal=proposal_ref, review=review)
        self.manifests = {image_ref.artifact_id: image, proposal_ref.artifact_id: proposal}
        self.paths = {image.pixels.artifact_id: source}; self.staged = []
        root.mkdir(parents=True)
        self.scratch = ScratchSpace(root, uuid4(), uuid4(), 1, 8_000_000, min_free_bytes=0)

    def params(self, model): return self.params_value
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self, path, reservation, role, media_type):
        payload = path.read_bytes(); ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(payload).hexdigest())
        self.staged.append((role, payload, ref)); return ref
    def stage_contract(self, value, role):
        payload = canonical_bytes(value); ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(payload).hexdigest())
        self.staged.append((role, payload, ref)); return ref


def test_dust_repair_applies_only_authorized_sensor_candidate(tmp_path):
    source = tmp_path / "source.npy"; rgb = dust_frame(); np.save(source, rgb)
    pixels = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(source.read_bytes()).hexdigest())
    image = WorkingImage(pixels=pixels, canvas=Canvas(width=128, height=96, orientation=1,
        space_id="dust:repair", source_sha256="a" * 64),
        color=ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="b" * 64, encoding="gamma24"),
        dtype="float32", alpha=None, transform_chain=TransformChain.create([]))
    image_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(image)).hexdigest())
    proposal_ref = ArtifactRef(artifact_id=uuid4(), sha256="c" * 64)
    candidate_id = hashlib.sha256(b"candidate").hexdigest()
    proposal = DustCandidates(frames=[image_ref] * 5, camera_id="camera-1", sensor_width=256,
        sensor_height=192, raw_crop={"x": 0, "y": 0, "width": 256, "height": 192},
        candidates=[{"id": candidate_id, "sensor_x": 129, "sensor_y": 97, "radius_sensor_px": 6,
                     "observed_frames": 5, "eligible_frames": 5, "recurrence": 1., "decision": "pending"}], warnings=[])
    # Runtime proposals require unique frames; use the target plus four unrelated refs.
    proposal.frames = [image_ref] + [ArtifactRef(artifact_id=uuid4(), sha256=f"{i+1:064x}") for i in range(4)]
    review = DustReview(version="sensor_dust_review_v1", proposal=proposal_ref, base=image_ref, revision=2,
                        accepted_candidate_ids=[candidate_id], rejected_candidate_ids=[])
    artifacts = RepairArtifacts(tmp_path / "scratch", source, image, image_ref, proposal, proposal_ref, review)
    ctx = base_context(image_ref, "sensor.dust_repair"); ctx.lease.command.params = ArtifactRef(artifact_id=uuid4(), sha256="e" * 64)
    ctx.lease.command.inputs = [image_ref, pixels, proposal_ref, ctx.lease.command.params]
    ctx.lease.execution_profile["resource_estimate"] = {"ram_bytes": 536_870_912}
    try:
        result = execute_repair(ctx, artifacts)
    finally:
        artifacts.scratch.cleanup()
    repaired = np.load(io.BytesIO(next(payload for role, payload, _ in artifacts.staged if role == "working_rgb")), allow_pickle=False)
    assert repaired.shape == rgb.shape and np.mean(repaired[45:52, 61:68]) > np.mean(rgb[45:52, 61:68])
    assert [role for role, _, _ in artifacts.staged] == ["working_rgb", "working_image", "dust_repair_report"]
    assert len(result.outputs) == 3 and ctx.snapshot_progress() == {"done": 3, "total": 3}
