from contextlib import contextmanager
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID, uuid5

import numpy as np
import pytest
from jsonschema import Draft202012Validator, FormatChecker

from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.operations.session.contracts import (Calibration, ConsentSnapshot,
    InputSnapshot, SessionDescribeParams)
from photo_worker.operations.session.describe import execute
from photo_worker.runtime.executor import OperationFailure


def ref(n):
    return ArtifactRef(artifact_id=UUID(int=n), sha256=f'{n:064x}')


def calibration():
    return Calibration.model_validate(dict(schema_version=1, model_lock_sha='a'*64,
        dataset_sha='b'*64, qualified=True, scene_cosine_min=.9,
        scene_log_luma_max=1, cluster_diameter_max=.1,
        sharpness_scale=100, clipping_penalty=1, match_inlier_min=.5,
        match_error_px_max=2, identity_similarity_min=.5, landmark_drift_max=.5,
        normalization_scales=[1]*6, loss_weights=[.4,.1,.1,.1,.2,.1], focus_warning=.5,
        focus_critical=.2, eye_open_min=.5, glare_warning_fraction=.5,
        clipping_warning_fraction=.5, clipping_critical_fraction=.9,
        min_face_edge_px=32, depth_sigma=.1, registration_error_px=1,
        motion_threshold=.1))


class FakePool:
    def __init__(self, faces=False, identity=False):
        self.faces = faces
        self.lock = SimpleNamespace(lock_id='a'*64, models=[
            SimpleNamespace(id=name, files=[SimpleNamespace(sha256='f'*64)])
            for name in ('dinov2_vits14', 'sfd', 'fan2d', 'lraspp_mobilenet_v3_large',
                         *(['identity512'] if identity else []))])

    @contextmanager
    def acquire(self, model_id, backend):
        def infer(value):
            if model_id == 'dinov2_vits14':
                vector = np.zeros(384, np.float32)
                vector[0] = 1
                return vector
            if model_id == 'sfd':
                return np.array([[2, 2, 30, 30, .9]], np.float32) if self.faces else np.empty((0, 5), np.float32)
            if model_id == 'fan2d':
                return np.tile(np.array([[128, 128, .9]], np.float32), (68, 1))
            if model_id == 'lraspp_mobilenet_v3_large':
                return np.zeros((21, *value.shape[:2]), np.float32)
            if model_id == 'identity512':
                vector = np.zeros(512, np.float32)
                vector[0] = 1
                return vector
            raise AssertionError('No faces need landmark inference')
        yield SimpleNamespace(infer=infer)


class FakeArtifacts:
    def __init__(self, path: Path, *, confirmed=False):
        self.path = path
        canvas = Canvas(width=32, height=32, orientation=1,
                        space_id='source', source_sha256='1'*64)
        self.image = WorkingImage(pixels=ref(6), canvas=canvas,
            color=ColorSpace(profile_id='work_rec2020_gamma24_v1',
                             icc_sha256='2'*64, encoding='gamma24'),
            dtype='float32', alpha=None, transform_chain=TransformChain.create([]))
        binding = dict(asset_version_id=UUID(int=7), source=ref(5),
                       canvas_hash=canonical_hash(canvas))
        raw = dict(schema_version=1, snapshot_id=UUID(int=8),
                   snapshot_digest='0'*64, items=[dict(ordinal=0, binding=binding,
                   working_image=ref(5), captured_at=None,
                   camera_hash=None, recipe=ref(9))])
        preliminary = InputSnapshot.model_validate(raw)
        raw['snapshot_digest'] = canonical_hash(
            preliminary.model_dump(mode='json', exclude={'snapshot_digest'}))
        self.snapshot = InputSnapshot.model_validate(raw)
        self.params_value = SessionDescribeParams(schema_version=1, snapshot=ref(1),
            ordinals=[0], model_lock_sha='a'*64, calibration=ref(2), consents=ref(3))
        face_id = uuid5(UUID(int=7), f"{canonical_hash(['f'*64])}:0")
        self.consents = ConsentSnapshot.model_validate(dict(schema_version=1,
            grants=[dict(consent_id=UUID(int=12), epoch='1',
                         person_id=UUID(int=11), purpose='identity_matching')]
                   if confirmed else [],
            confirmed_faces=[dict(asset_version_id=UUID(int=7),
            face_id=face_id, person_id=UUID(int=11), consent_id=UUID(int=12),
            epoch='1')] if confirmed else []))
        self.staged = []

    def params(self, model):
        return self.params_value

    def contract(self, reference, model):
        return {InputSnapshot: self.snapshot, Calibration: calibration(),
                ConsentSnapshot: self.consents, WorkingImage: self.image}[model]

    @contextmanager
    def input(self, reference, *, suffix):
        yield SimpleNamespace(path=self.path)

    def stage_contract(self, value, role):
        self.staged.append((role, value))
        return ref(40 + len(self.staged))


def context():
    return SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cuda'}), check_active=lambda: None)


def test_describe_stages_faces_before_descriptor_shard(tmp_path):
    path = tmp_path / 'pixels.npy'
    np.save(path, np.full((32, 32, 3), .5, np.float32))
    artifacts = FakeArtifacts(path)
    result = execute(context(), artifacts, FakePool(), 'cuda')
    assert [role for role, _ in artifacts.staged] == ['faces', 'document']
    assert result.outputs == [ref(42)]
    assert artifacts.staged[0][1].faces == []
    frame = artifacts.staged[1][1].frames[0]
    assert frame.faces == ref(41)
    assert frame.scene_embedding[0] == 1
    assert frame.person_fraction == 0
    schemas = json.loads((Path(__file__).resolve().parents[2] /
                          'docs/implementation-v2/contracts/schemas.json').read_text())
    for role, document in artifacts.staged:
        name = 'DescriptorShard' if role == 'document' else 'FaceObservations'
        Draft202012Validator({'$defs': schemas['$defs'],
                              '$ref': f'#/$defs/{name}'},
                             format_checker=FormatChecker()).validate(document.model_dump(mode='json'))


def test_confirmed_faces_require_identity_model(tmp_path):
    artifacts = FakeArtifacts(tmp_path / 'unused.npy', confirmed=True)
    with pytest.raises(OperationFailure, match='IDENTITY_MODEL_UNAVAILABLE'):
        execute(context(), artifacts, FakePool(), 'cuda')
    assert not artifacts.staged


def test_confirmed_face_uses_qualified_identity_embedding(tmp_path, monkeypatch):
    path = tmp_path / 'pixels.npy'
    np.save(path, np.full((32, 32, 3), .5, np.float32))
    artifacts = FakeArtifacts(path, confirmed=True)
    monkeypatch.setattr('photo_worker.operations.session.describe.align_identity_face',
                        lambda rgb, face: np.full((112, 112, 3), .5, np.float32))
    execute(context(), artifacts, FakePool(faces=True, identity=True), 'cuda')
    face = artifacts.staged[0][1].faces[0]
    assert face.person_id == UUID(int=11)
    assert len(face.embedding) == 512 and face.embedding[0] == 1
    assert sum(abs(value) for value in face.embedding[1:]) == 0


def test_confirmed_face_rejects_revoked_or_mismatched_consent(tmp_path):
    artifacts = FakeArtifacts(tmp_path / 'unused.npy', confirmed=True)
    artifacts.consents.grants[0].epoch = '2'
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(context(), artifacts, FakePool(faces=True, identity=True), 'cuda')
    assert not artifacts.staged


def test_detected_face_has_stable_observation_and_landmarks(tmp_path):
    path = tmp_path / 'pixels.npy'
    np.save(path, np.full((32, 32, 3), .5, np.float32))
    artifacts = FakeArtifacts(path)
    execute(context(), artifacts, FakePool(faces=True), 'cuda')
    face = artifacts.staged[0][1].faces[0]
    assert face.face_id.version == 5
    assert face.observation_id.version == 5
    assert len(face.landmarks) == 68
    assert 0 < face.quality <= 1
    assert face.embedding is None and face.person_id is None

@pytest.mark.parametrize('weights', [[0]*6, [.1]*6, [.5]*6])
def test_calibration_rejects_non_normalized_loss_weights(weights):
    value=calibration().model_dump(mode='json')
    value['loss_weights']=weights
    with pytest.raises(ValueError,match='loss_weights must sum to one'):
        Calibration.model_validate(value)


def test_describe_cancelled_before_inputs_stages_nothing(tmp_path):
    artifacts = FakeArtifacts(tmp_path / 'unused.npy')
    ctx = context()
    def cancelled():
        raise RuntimeError('cancelled')
    ctx.check_active = cancelled
    with pytest.raises(RuntimeError, match='cancelled'):
        execute(ctx, artifacts, FakePool(), 'cuda')
    assert artifacts.staged == []


def test_describe_cancelled_after_faces_does_not_publish_descriptor(tmp_path):
    path = tmp_path / 'pixels.npy'
    np.save(path, np.full((32, 32, 3), .5, np.float32))
    artifacts = FakeArtifacts(path)
    ctx = context()
    def active():
        if artifacts.staged:
            raise RuntimeError('cancelled')
    ctx.check_active = active
    with pytest.raises(RuntimeError, match='cancelled'):
        execute(ctx, artifacts, FakePool(), 'cuda')
    assert [role for role, _ in artifacts.staged] == ['faces']
