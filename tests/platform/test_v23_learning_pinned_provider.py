from types import SimpleNamespace
from contextlib import contextmanager
from uuid import uuid4

import pytest
import numpy as np

from photo_worker.algorithms.color import identity_lut
from photo_worker.algorithms.statistics import coverage_counts_bounded
from photo_worker.contracts.style import (Adaptation, LUT, ModelReference,
                                          Prototype, RegionAdjustment)
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.learning.pinned_provider import (
    PinnedColorHoldoutProvider,
)
from photo_worker.operations.learning.contracts import RenderCase
from photo_worker.operations.learning.recipe import FrozenHoldoutStyles
from photo_worker.operations.session.contracts import (
    ConsentSnapshot, FaceObservation, FaceObservations, FacePoint, FaceRect,
)
from photo_worker.runtime.scratch import ScratchSpace

from test_style_resolve import profile
from test_v23_learning_source import _fixture


def _setup(tmp_path, monkeypatch):
    artifacts, case = _fixture(tmp_path)
    before = FaceObservations(schema_version=1, binding=case.binding, faces=[])
    case.faces = artifacts.add(before)
    lock = SimpleNamespace(lock_id='c' * 64,
                           models=[SimpleNamespace(id='sfd'),
                                   SimpleNamespace(id='fan2d')])
    pool = SimpleNamespace(lock=lock)
    registry = SimpleNamespace(resolve=lambda name: SimpleNamespace(
        sha256=artifacts.image.color.icc_sha256))
    provider = PinnedColorHoldoutProvider(pool, registry)
    monkeypatch.setattr(
        'photo_worker.operations.learning.pinned_provider._face_observations',
        lambda rgb, binding, pool, backend, edge, check:
        FaceObservations(schema_version=1, binding=binding, faces=[]))
    ctx = SimpleNamespace(check_active=lambda: None)
    calibration = SimpleNamespace(qualified=True, model_lock_sha='c' * 64,
                                  min_face_edge_px=64)
    item = SimpleNamespace(render_case=case, event_id=uuid4())
    root = tmp_path / 'scratch'
    root.mkdir()
    scratch = ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000,
                           min_free_bytes=0)
    artifacts.scratch = scratch
    return provider, ctx, artifacts, item, calibration, scratch


def test_pinned_color_provider_returns_measured_no_person_case(tmp_path,
                                                                monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    base = profile()
    try:
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(base, base, base),
                             calibration) as evidence:
            assert evidence.identity_status == 'no_person'
            assert evidence.detector_succeeded
            assert evidence.detected_faces == 0
            assert evidence.baseline_rgb.shape == artifacts.rgb.shape
            assert evidence.protected.shape == artifacts.rgb.shape[:2]
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()


def test_pinned_color_provider_uses_frozen_region_mask(tmp_path, monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    pixels = np.full(artifacts.rgb.shape[:2], .4, np.float32)
    mask = MaskRef(artifact=artifacts.array(tmp_path, 'region-sky', pixels),
                   canvas=artifacts.image.canvas,
                   transform_chain=artifacts.image.transform_chain,
                   selector='sky', confidence=.9, model_lock_id=None,
                   protected=False)
    item.render_case = RenderCase.model_validate({
        **item.render_case.model_dump(mode='python'),
        'region_masks': [{'region_id': 'sky', 'mask': artifacts.add(mask)}]})
    style = profile().model_copy(deep=True)
    style.extensions.region_adjustments = [RegionAdjustment(
        region_id='sky', selector='sky', ev=.1, saturation=1.05,
        hue_deg=2, strength=.8, priority=1)]
    try:
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(style, style, style),
                             calibration) as evidence:
            assert evidence.identity_status == 'no_person'
            assert not np.array_equal(evidence.candidate_rgb, artifacts.rgb)
    finally:
        scratch.cleanup()


def test_pinned_color_provider_fails_on_change_outside_effective_mask(
        tmp_path, monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    mask = MaskRef(artifact=artifacts.array(
        tmp_path, 'effective', np.zeros(artifacts.rgb.shape[:2], np.float32)),
        canvas=artifacts.image.canvas,
        transform_chain=artifacts.image.transform_chain,
        selector='person', confidence=1, model_lock_id=None,
        protected=False)
    item.render_case = RenderCase.model_validate({
        **item.render_case.model_dump(mode='python'),
        'effective_mask': artifacts.add(mask),
        'consent_snapshot': artifacts.image_ref})

    def render(workspace, styles, **kwargs):
        workspace.baseline[:] = workspace.source.rgb
        workspace.candidate[:] = workspace.source.rgb
        workspace.candidate[0, 0, 0] += np.float32(.01)
        workspace.approved[:] = workspace.source.rgb

    monkeypatch.setattr(
        'photo_worker.operations.learning.pinned_provider.render_holdout_triplet',
        render)
    try:
        base = profile()
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(base, base, base),
                             calibration) as evidence:
            assert evidence.identity_status == 'fail'
    finally:
        scratch.cleanup()


def test_pinned_color_provider_measures_confirmed_identity(tmp_path,
                                                           monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    provider.pool.lock.models.append(SimpleNamespace(id='identity512'))
    calibration.min_face_edge_px = 1
    calibration.identity_similarity_min = .8
    calibration.landmark_drift_max = .1
    point = FacePoint(x=.3, y=.3)
    person, consent_id, face_id = uuid4(), uuid4(), uuid4()
    face = FaceObservation(observation_id=uuid4(), face_id=face_id,
        box=FaceRect(x0=.2, y0=.2, x1=.4, y1=.4),
        landmarks=[point] * 68, embedding=[1.] + [0.] * 511,
        quality=1, person_id=person)
    before = FaceObservations(schema_version=1,
                              binding=item.render_case.binding, faces=[face])
    item.render_case.faces = artifacts.add(before)
    mask = MaskRef(artifact=artifacts.array(
        tmp_path, 'effective', np.ones(artifacts.rgb.shape[:2], np.float32)),
        canvas=artifacts.image.canvas,
        transform_chain=artifacts.image.transform_chain,
        selector='person', confidence=1, model_lock_id=None,
        protected=False)
    snapshot = ConsentSnapshot(schema_version=1, grants=[dict(
        consent_id=consent_id, epoch='3', person_id=person,
        purpose='identity_matching')], confirmed_faces=[dict(
        asset_version_id=item.render_case.binding.asset_version_id,
        face_id=face_id, person_id=person, consent_id=consent_id,
        epoch='3')])
    item.render_case = RenderCase.model_validate({
        **item.render_case.model_dump(mode='python'),
        'effective_mask': artifacts.add(mask),
        'consent_snapshot': artifacts.add(snapshot)})
    plain = face.model_copy(update={'embedding': None, 'person_id': None})

    def detect(rgb, binding, pool, backend, edge, check, confirmed=None):
        return FaceObservations(schema_version=1, binding=binding,
            faces=[face if confirmed else plain])

    monkeypatch.setattr(
        'photo_worker.operations.learning.pinned_provider._face_observations',
        detect)
    try:
        base = profile()
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(base, base, base),
                             calibration) as evidence:
            assert evidence.identity_status == 'pass'
            assert evidence.verifier_qualified
    finally:
        scratch.cleanup()


def test_pinned_color_provider_rejects_unqualified_lock(tmp_path,
                                                         monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    try:
        calibration.model_lock_sha = '0' * 64
        with pytest.raises(ValueError, match='HOLDOUT_DETECTOR_UNQUALIFIED'):
            with provider.render(ctx, artifacts, item,
                                 FrozenHoldoutStyles(profile(), profile(),
                                                     profile()), calibration):
                pass
    finally:
        scratch.cleanup()


def test_pinned_color_provider_requires_fresh_empty_source_detection(
        tmp_path, monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    point = FacePoint(x=.3, y=.3)
    face = FaceObservation(observation_id=uuid4(), face_id=uuid4(),
        box=FaceRect(x0=.2, y0=.2, x1=.4, y1=.4),
        landmarks=[point] * 68, embedding=None, quality=1, person_id=None)
    calls = []

    def detect(rgb, binding, pool, backend, edge, check):
        calls.append(rgb.shape)
        return FaceObservations(schema_version=1, binding=binding,
                                faces=[face] if len(calls) == 1 else [])

    monkeypatch.setattr(
        'photo_worker.operations.learning.pinned_provider._face_observations',
        detect)
    try:
        base = profile()
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(base, base, base),
                             calibration) as evidence:
            assert evidence.identity_status == 'not_measurable'
        assert len(calls) == 2
    finally:
        scratch.cleanup()


def test_pinned_color_provider_resolves_adaptive_style_with_person_stats(
        tmp_path, monkeypatch):
    provider, ctx, artifacts, item, calibration, scratch = _setup(
        tmp_path, monkeypatch)
    base = profile()
    lut = LUT(kind='absolute', size=33, encoding='dense',
              values=identity_lut(33).reshape(-1).tolist())
    adaptation = Adaptation(feature_version='stats20_v1', mean=[0.] * 20,
        scale=[1.] * 20, k=1, temperature=1, ood_threshold=1,
        prototypes=[Prototype(id='one', features=[0.] * 20,
                              absolute_lut=lut, local=base.local)],
        coverage_bins=[True] * 512)
    adaptive = base.model_copy(update={
        'mode': 'adaptive', 'adaptation': adaptation,
        'models': [ModelReference(id='lraspp_mobilenet_v3_large',
                                  weight_id='test')]})
    calls = []

    @contextmanager
    def person_stats(source, alpha, pool, backend, lock, scratch, check):
        calls.append(source.shape)
        yield SimpleNamespace(mask=np.ones(source.shape[:2], np.float32),
            features=np.zeros(20),
            coverage_counts=coverage_counts_bounded(
                source, alpha=alpha, encoding='gamma24'),
            reliable=True)

    monkeypatch.setattr(
        'photo_worker.operations.learning.pinned_provider.open_holdout_person_stats',
        person_stats)
    try:
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(adaptive, adaptive, adaptive),
                             calibration) as evidence:
            assert evidence.identity_status == 'no_person'
            assert evidence.baseline_rgb.shape == artifacts.rgb.shape
        assert len(calls) == 1
    finally:
        scratch.cleanup()
