from types import SimpleNamespace
from contextlib import contextmanager
from uuid import uuid4

import pytest
import numpy as np

from photo_worker.algorithms.color import identity_lut
from photo_worker.algorithms.statistics import coverage_counts_bounded
from photo_worker.contracts.style import (Adaptation, Defects, LUT, ModelReference,
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


def test_triplet_rechecks_reviewed_repair_protection_before_render(monkeypatch):
    from photo_worker.operations.learning.workspace import render_holdout_triplet
    base = profile().model_copy(update={'defects': Defects(
        enabled=True, algorithm='defectrepair_telea_v1', policy='review',
        strength=.5, diameter_face_fraction=[.002, .012],
        preserve_permanent_marks=True)})
    source = np.zeros((2, 2, 3), dtype=np.float32)
    protected = np.zeros((2, 2), dtype=np.float32)
    protected[0, 0] = 1
    repaired = source.copy()
    repaired[0, 0, 0] = np.float32(.1)
    workspace = SimpleNamespace(
        source=SimpleNamespace(rgb=source, alpha=None, protected=protected,
                               effective_mask=None, region_masks={}),
        baseline=np.empty_like(source), candidate=np.empty_like(source),
        approved=np.empty_like(source), alpha=np.empty((2, 2), np.float32))
    monkeypatch.setattr(
        'photo_worker.operations.learning.color_render.render_materialized_style',
        lambda *args, **kwargs: pytest.fail('render began with changed protected pixels'))
    with pytest.raises(ValueError, match='HOLDOUT_DEFECT_PROTECTION_CHANGED'):
        render_holdout_triplet(workspace, FrozenHoldoutStyles(base, base, base),
            person_reliable=False, reviewed_source_rgb=repaired)


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



def test_pinned_holdout_renders_beauty_for_all_three_frozen_styles(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Beauty
    from photo_worker.operations.learning import beauty_render
    provider, ctx, artifacts, item, calibration, scratch = _setup(tmp_path, monkeypatch)
    base = profile().model_copy(deep=True)
    base.strength = .5
    base.beauty = Beauty(enabled=True, algorithm='beautyfilter_v1', smoothing=.6,
                         tone_evenness=.2, texture_keep=1,
                         skin_mask='face68_conservative_v1', protect_features=True)
    calls = []
    production = beauty_render.prepare_beauty
    @contextmanager
    def stage(rgb, image, faces, params, scratch_space, **kwargs):
        calls.append((params.model_copy(deep=True), kwargs['permanent'].copy()))
        with production(rgb, image, faces, params, scratch_space, **kwargs) as result:
            yield result
    monkeypatch.setattr(beauty_render, 'prepare_beauty', stage)
    try:
        with provider.render(ctx, artifacts, item,
                             FrozenHoldoutStyles(base, base, base), calibration) as evidence:
            assert evidence.identity_status == 'no_person'
            np.testing.assert_array_equal(evidence.baseline_rgb, evidence.candidate_rgb)
            np.testing.assert_array_equal(evidence.candidate_rgb, evidence.approved_rgb)
        assert len(calls) == 3
        for params, protected in calls:
            assert params.smoothing == pytest.approx(.3)
            assert params.tone_evenness == pytest.approx(.1)
            assert params.texture_keep == 1
            np.testing.assert_array_equal(protected, np.zeros(artifacts.rgb.shape[:2]))
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()


def test_holdout_beauty_rejects_skipped_face_instead_of_measured_success(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Beauty
    from photo_worker.operations.learning import beauty_render
    from photo_worker.operations.learning.beauty_render import render_holdout_beauty
    from test_v23_learning_source import _fixture
    artifacts, case = _fixture(tmp_path)
    style = profile().model_copy(deep=True)
    style.beauty = Beauty(enabled=True, algorithm='beautyfilter_v1', smoothing=.6,
                         tone_evenness=0, texture_keep=1,
                         skin_mask='face68_conservative_v1', protect_features=True)
    observations = FaceObservations(schema_version=1, binding=case.binding, faces=[])
    released = []
    @contextmanager
    def stage(*args, **kwargs):
        try:
            yield SimpleNamespace(faces=[SimpleNamespace(applied=False, reason='low_confidence')])
        finally:
            released.append(True)
    monkeypatch.setattr(beauty_render, 'prepare_beauty', stage)
    source = SimpleNamespace(image=artifacts.image, protected=np.zeros(artifacts.rgb.shape[:2], np.float32))
    with pytest.raises(ValueError, match='HOLDOUT_BEAUTY_NOT_MEASURABLE'):
        render_holdout_beauty(SimpleNamespace(scratch=None), source,
                              artifacts.rgb.copy(), style, observations)
    assert released == [True]



def test_holdout_beauty_matches_production_face_composition(tmp_path):
    from photo_worker.algorithms.effective import effective_beauty_params
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.operations.learning.beauty_render import render_holdout_beauty
    from photo_worker.operations.portrait.prepare import prepare_beauty
    from test_beauty_prepare import inputs
    from test_beauty import params
    rgb, image, faces, _ = inputs()
    observations = FaceObservations(schema_version=1, binding=dict(
        asset_version_id=uuid4(), source=image.pixels, canvas_hash=canonical_hash(image.canvas)),
        faces=[FaceObservation(observation_id=uuid4(), face_id=entry.face.object_id,
            box=FaceRect(x0=.1,y0=.1,x1=.9,y1=.9),
            landmarks=[FacePoint(x=point.x,y=point.y) for point in entry.face.points],
            embedding=None, quality=1, person_id=None) for entry in faces])
    style=profile().model_copy(deep=True);style.beauty=params();style.strength=.5
    protected=np.zeros(rgb.shape[:2],np.float32);protected[:,100:110]=1
    target=rgb.copy()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,40*1024**2,min_free_bytes=0) as scratch:
        render_holdout_beauty(SimpleNamespace(scratch=scratch),
            SimpleNamespace(image=image,protected=protected),target,style,observations,
            skin_probability=faces[0].skin_probability,
            landmarks=tuple(entry.face for entry in faces))
        with prepare_beauty(rgb,image,faces,effective_beauty_params(style.beauty,.5),
                            scratch,permanent=protected) as result:
            expected=np.load(result.image,allow_pickle=False)
            np.testing.assert_array_equal(target,expected)
            assert all(face.applied for face in result.faces)
        np.testing.assert_array_equal(target[protected==1],rgb[protected==1])
        assert list(scratch.directory.iterdir())==[]


def test_holdout_triplet_owns_all_styles_before_first_callback(tmp_path, monkeypatch):
    from photo_worker.operations.learning.workspace import render_holdout_triplet

    base = profile()
    candidate = base.model_copy(deep=True)
    approved = base.model_copy(deep=True)
    styles = FrozenHoldoutStyles(base, candidate, approved)
    expected = [style.model_dump(mode='json', by_alias=True)
                for style in (base, candidate, approved)]
    calls = []
    buffers = []
    for name in ('baseline', 'candidate', 'approved'):
        buffers.append(np.lib.format.open_memmap(tmp_path / (name + '.npy'),
            mode='w+', dtype='<f4', shape=(2, 2, 3)))
    workspace = SimpleNamespace(source=SimpleNamespace(rgb=np.zeros((2, 2, 3), np.float32),
        alpha=None, protected=np.zeros((2, 2), np.float32), effective_mask=None,
        region_masks={}), baseline=buffers[0], candidate=buffers[1],
        approved=buffers[2], alpha=np.ones((2, 2), np.float32))
    mutated = False

    def active():
        nonlocal mutated
        if not mutated:
            mutated = True
            base.strength = .11
            candidate.strength = .22
            approved.strength = .33

    def render(source, target, style, **kwargs):
        calls.append(style.model_dump(mode='json', by_alias=True))
        target[:] = source

    monkeypatch.setattr('photo_worker.operations.learning.color_render.render_materialized_style', render)
    try:
        render_holdout_triplet(workspace, styles, person_reliable=False, check_active=active)
        assert calls == expected
        assert mutated
    finally:
        for buffer in buffers:
            buffer._mmap.close()


def test_holdout_denoise_source_is_scoped_for_each_comparison_arm(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Restoration
    from photo_worker.operations.learning.workspace import render_holdout_triplet

    styles = []
    for strength in (.2, .5, .8):
        style = profile().model_copy(deep=True)
        style.strength = strength
        style.extensions.restoration = Restoration(denoise='scunet_color_real_psnr',
            denoise_strength=.6, upscale='off', sensor_dust='off')
        styles.append(style)
    events = []
    buffers = [np.lib.format.open_memmap(tmp_path / (str(i) + '.npy'), mode='w+',
               dtype='<f4', shape=(2, 2, 3)) for i in range(3)]
    workspace = SimpleNamespace(source=SimpleNamespace(rgb=np.zeros((2, 2, 3), np.float32),
        alpha=None, protected=np.zeros((2, 2), np.float32), effective_mask=None,
        region_masks={}), baseline=buffers[0], candidate=buffers[1],
        approved=buffers[2], alpha=np.ones((2, 2), np.float32))

    @contextmanager
    def prepare(style):
        events.append(('denoise-open', style.strength))
        try:
            yield np.full((2, 2, 3), style.strength, np.float32)
        finally:
            events.append(('denoise-close', style.strength))

    def color(source, target, style, **kwargs):
        assert style.extensions.restoration is None
        assert np.all(source == np.float32(style.strength))
        events.append(('color', style.strength))
        target[:] = source

    monkeypatch.setattr('photo_worker.operations.learning.color_render.render_materialized_style', color)
    try:
        render_holdout_triplet(workspace, FrozenHoldoutStyles(*styles),
                              person_reliable=False, prepare_source=prepare)
        assert events == [(kind, strength) for strength in (.2, .5, .8)
                          for kind in ('denoise-open', 'color', 'denoise-close')]
        for buffer, strength in zip(buffers, (.2, .5, .8), strict=True):
            assert np.all(buffer == np.float32(strength))
    finally:
        for buffer in buffers:
            buffer._mmap.close()


def test_reviewed_dust_renders_each_arm_without_denoise_installation(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Restoration
    from photo_worker.operations.learning.workspace import render_holdout_triplet

    styles = []
    for strength in (.2, .5, .8):
        style = profile().model_copy(deep=True)
        style.strength = strength
        style.extensions.restoration = Restoration(
            denoise='off', denoise_strength=0, upscale='off',
            sensor_dust='review')
        styles.append(style)
    buffers = [np.lib.format.open_memmap(tmp_path / (str(i) + '.npy'),
               mode='w+', dtype='<f4', shape=(2, 2, 3)) for i in range(3)]
    workspace = SimpleNamespace(
        source=SimpleNamespace(rgb=np.zeros((2, 2, 3), np.float32), alpha=None,
            protected=np.zeros((2, 2), np.float32), effective_mask=None,
            region_masks={}), baseline=buffers[0], candidate=buffers[1],
        approved=buffers[2], alpha=np.ones((2, 2), np.float32))
    events = []

    def render(source, target, style, **kwargs):
        assert style.extensions.restoration is None
        assert source is workspace.source.rgb
        events.append(('color', id(target)))
        target[:] = np.float32(style.strength)

    def repair(target):
        events.append(('dust', id(target)))
        target[:] += np.float32(.1)

    monkeypatch.setattr(
        'photo_worker.operations.learning.color_render.render_materialized_style',
        render)
    try:
        render_holdout_triplet(workspace, FrozenHoldoutStyles(*styles),
                              person_reliable=False, reviewed_dust=repair)
        assert events == [event for buffer in buffers for event in
                          (('color', id(buffer)), ('dust', id(buffer)))]
        for buffer, strength in zip(buffers, (.2, .5, .8), strict=True):
            assert np.all(buffer == np.float32(strength) + np.float32(.1))
    finally:
        for buffer in buffers:
            buffer._mmap.close()


def test_solid_candidate_detection_uses_output_opacity(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Background
    provider, ctx, artifacts, item, calibration, scratch = _setup(tmp_path, monkeypatch)
    artifacts.image.alpha = artifacts.array(tmp_path, 'source-invisible-alpha',
                                           np.zeros(artifacts.rgb.shape[:2], np.uint16))
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode='solid', asset_id=None,
        solid_rgb=[.3, .4, .5], blur_strength=0, protect_subject=True)
    detected = []

    @contextmanager
    def person(*args):
        yield SimpleNamespace(mask=np.ones(artifacts.rgb.shape[:2], np.float32),
                              confidence=.9, reliable=False, features=None, coverage_counts=None)

    def detect(rgb, binding, pool, backend, edge, check):
        detected.append(rgb.copy())
        return FaceObservations(schema_version=1, binding=binding, faces=[])

    monkeypatch.setattr('photo_worker.operations.learning.pinned_provider.open_holdout_person_stats', person)
    monkeypatch.setattr('photo_worker.operations.learning.pinned_provider._face_observations', detect)
    try:
        with provider.render(ctx, artifacts, item, FrozenHoldoutStyles(style, style, style),
                             calibration) as evidence:
            assert np.all(evidence.alpha == 1)
            assert len(detected) == 2
            assert not np.any(detected[0])
            assert np.any(detected[1] > 0)
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()


@pytest.mark.parametrize('mode', ['valid', 'missing_verifier', 'changed_verifier', 'unqualified_verifier', 'decoder_failure'])
def test_asset_provider_keeps_one_decoder_scope_for_triplet(tmp_path, monkeypatch, mode):
    from photo_worker.contracts.style import Background
    from photo_worker.operations.learning.contracts import FrozenRecipe
    from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
    from photo_worker.v2.render_background_decode import NormalizedBackground
    provider, ctx, artifacts, item, calibration, scratch = _setup(tmp_path, monkeypatch)
    ctx.lease = SimpleNamespace(execution_profile={})
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode='asset', asset_id='installed',
        blur_strength=0, protect_subject=True)
    style_ref = artifacts.image_ref
    pin = FrozenBackgroundAsset(schema_version=1, portable_asset_id='installed',
        style_version_id=uuid4(), style=style_ref, source=style_ref,
        expected_bytes=10, media_type='image/png', suffix='.png',
        assume_profile=None, work_icc_sha=provider.icc_lock_sha)
    item.render_case.background_asset = pin
    contract = artifacts.contract
    artifacts.contract = lambda ref, kind: (SimpleNamespace(base_style=style_ref)
        if kind is FrozenRecipe else contract(ref, kind))
    events = []

    @contextmanager
    def person(*args):
        events.append('person_open')
        try:
            yield SimpleNamespace(mask=np.zeros(artifacts.rgb.shape[:2], np.float32),
                confidence=.9, reliable=False, features=None, coverage_counts=None)
        finally:
            events.append('person_close')

    def verify():
        events.append('verify')
        if mode == 'unqualified_verifier':
            return False
        if mode == 'changed_verifier':
            provider.background_verify_runtime = lambda: None

    if mode != 'missing_verifier':
        provider.background_verify_runtime = verify

    @contextmanager
    def normalize(artifacts_arg, owned, registry, **kwargs):
        kwargs['check_active']()
        events.append('decoder_open')
        try:
            if mode == 'decoder_failure':
                raise ValueError('DECODER_FAILURE')
            yield NormalizedBackground(SimpleNamespace(profile_sha256=provider.icc_lock_sha,
                height=7, width=11), np.full((7, 11, 3), .25, np.float32))
        finally:
            events.append('decoder_close')

    from photo_worker.operations.learning import background_asset
    compose = background_asset.render_holdout_asset_background
    def render_arm(*args, **kwargs):
        assert 'decoder_open' in events and 'decoder_close' not in events
        events.append('arm')
        return compose(*args, **kwargs)

    monkeypatch.setattr('photo_worker.operations.learning.pinned_provider.open_holdout_person_stats', person)
    monkeypatch.setattr(background_asset, 'normalize_frozen_background', normalize)
    monkeypatch.setattr(background_asset, 'render_holdout_asset_background', render_arm)
    try:
        if mode == 'valid':
            with provider.render(ctx, artifacts, item, FrozenHoldoutStyles(style, style, style),
                                 calibration) as evidence:
                assert events.count('arm') == 3
                assert events.count('decoder_open') == 1
                assert events.count('decoder_close') == 1
                np.testing.assert_array_equal(evidence.baseline_rgb, evidence.candidate_rgb)
                np.testing.assert_array_equal(evidence.approved_rgb, evidence.candidate_rgb)
        else:
            error = {'missing_verifier': 'HOLDOUT_BACKGROUND_DECODER_INSTALLATION_REQUIRED',
                'changed_verifier': 'HOLDOUT_BACKGROUND_DECODER_INSTALLATION_CHANGED',
                'unqualified_verifier': 'HOLDOUT_BACKGROUND_DECODER_UNQUALIFIED',
                'decoder_failure': 'DECODER_FAILURE'}[mode]
            with pytest.raises(ValueError, match=error):
                with provider.render(ctx, artifacts, item, FrozenHoldoutStyles(style, style, style),
                                     calibration):
                    pytest.fail('refused decoder must not yield measured evidence')
            assert 'arm' not in events
            assert events.count('decoder_open') == (1 if mode == 'decoder_failure' else 0)
            assert events.count('decoder_close') == (1 if mode == 'decoder_failure' else 0)
        assert events.count('person_close') == 1
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()


def test_provider_owns_style_before_person_model_callback(tmp_path, monkeypatch):
    from photo_worker.contracts.style import Background
    provider, ctx, artifacts, item, calibration, scratch = _setup(tmp_path, monkeypatch)
    style = profile().model_copy(deep=True)
    style.extensions.background = Background(mode='solid', asset_id=None,
        solid_rgb=[.3, .4, .5], blur_strength=0, protect_subject=True)
    @contextmanager
    def person(*args):
        # Caller-owned style mutation after admission cannot change rendering.
        style.extensions.background.mode = 'generate'
        style.extensions.background.solid_rgb = None
        yield SimpleNamespace(mask=np.zeros(artifacts.rgb.shape[:2], np.float32),
            confidence=.9, reliable=False, features=None, coverage_counts=None)
    monkeypatch.setattr('photo_worker.operations.learning.pinned_provider.open_holdout_person_stats', person)
    try:
        with provider.render(ctx, artifacts, item, FrozenHoldoutStyles(style, style, style),
                             calibration) as evidence:
            assert style.extensions.background.mode == 'generate'
            assert np.isfinite(evidence.candidate_rgb).all()
            np.testing.assert_array_equal(evidence.baseline_rgb, evidence.candidate_rgb)
            np.testing.assert_array_equal(evidence.approved_rgb, evidence.candidate_rgb)
        assert list(scratch.directory.iterdir()) == []
    finally:
        scratch.cleanup()
