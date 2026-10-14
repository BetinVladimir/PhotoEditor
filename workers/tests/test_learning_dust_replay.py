from types import SimpleNamespace
from contextlib import contextmanager
from uuid import uuid4

import pytest
import numpy as np

from photo_worker.operations.learning.dust_replay import (
    _admit_holdout_dust, _copy_measurable_dust, _reviewed_dust_styles,
    open_holdout_reviewed_dust)
from photo_worker.operations.learning.pipeline_admission import validate_holdout_pipeline
from photo_worker.runtime.executor import OperationFailure


def _styles(mode='review', denoise='off'):
    def arm():
        restoration = SimpleNamespace(sensor_dust=mode, denoise=denoise,
                                      upscale='off')
        extensions = SimpleNamespace(restoration=restoration, background=None,
            culling=None, series=None, composition=None, portrait_detail=None,
            inpainting=None, generation=None)
        return SimpleNamespace(extensions=extensions,
            defects=SimpleNamespace(enabled=False),
            geometry=SimpleNamespace(enabled=False))
    return SimpleNamespace(baseline=arm(), candidate=arm(), approved=arm())


def test_reviewed_dust_requires_all_three_review_arms():
    styles = _styles()
    _reviewed_dust_styles(styles)
    styles.candidate.extensions.restoration.sensor_dust = 'off'
    with pytest.raises(ValueError, match='HOLDOUT_DUST_STYLE_MISMATCH'):
        _reviewed_dust_styles(styles)


def test_reviewed_dust_rejects_unmodeled_denoise():
    with pytest.raises(ValueError, match='HOLDOUT_DUST_STYLE_MISMATCH'):
        _reviewed_dust_styles(_styles(denoise='model'))


def test_dust_pipeline_requires_evidence_and_refuses_combined_defect():
    styles = _styles()
    validate_holdout_pipeline(styles, reviewed_dust=True)
    with pytest.raises(ValueError, match='HOLDOUT_COMPLETE_RESTORATION_REQUIRED'):
        validate_holdout_pipeline(styles)
    with pytest.raises(ValueError, match='HOLDOUT_EFFECT_EVIDENCE_CONFLICT'):
        validate_holdout_pipeline(styles, reviewed_dust=True, reviewed_defect=True)


def test_dust_pipeline_rejects_different_arms_before_replay():
    styles = _styles()
    styles.candidate.extensions.restoration.extra_setting = 'different'
    with pytest.raises(ValueError, match='HOLDOUT_DUST_STYLE_MISMATCH'):
        validate_holdout_pipeline(styles, reviewed_dust=True)


def test_dust_resource_admission_uses_sequential_case_peak():
    pixels = 64 * 64
    profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
        'resource_estimate': {'pixel_count': pixels * 10,
            'ram_bytes': 128*pixels + 512*1_048_576,
            'scratch_bytes': 52*pixels + 32768,
            'vram_bytes': 10_000_000_000, 'output_bytes': 8_388_608}}
    _admit_holdout_dust(profile, pixels)
    profile['resource_estimate']['scratch_bytes'] -= 1
    with pytest.raises(OperationFailure):
        _admit_holdout_dust(profile, pixels)
    profile['resource_estimate']['scratch_bytes'] += 1
    profile['resource_estimate']['pixel_count'] = pixels - 1
    with pytest.raises(OperationFailure):
        _admit_holdout_dust(profile, pixels)


def test_dust_replay_restores_protection_and_requires_observable_edit():
    source = np.zeros((4, 4, 3), dtype=np.float32)
    source[0, 0] = np.array([.1, .2, .3], dtype=np.float32)
    target = source.copy()
    repaired = target.copy()
    repaired[0, 0] = 1
    repaired[1, 1] = .5
    protected = np.zeros((4, 4), dtype=np.float32)
    protected[0, 0] = 1
    _copy_measurable_dust(target, repaired, source, protected)
    assert np.array_equal(target[0, 0].view(np.uint32),
                          source[0, 0].view(np.uint32))
    assert np.all(target[1, 1] == .5)
    unchanged = source.copy()
    with pytest.raises(ValueError, match='HOLDOUT_DUST_REPAIR_NOT_MEASURABLE'):
        _copy_measurable_dust(unchanged, source.copy(), source, protected)


def test_reviewed_dust_replay_owns_exactly_three_distinct_arms(monkeypatch):
    import photo_worker.operations.learning.dust_replay as replay

    class Snapshot(SimpleNamespace):
        def model_copy(self, **kwargs):
            return self

    image = SimpleNamespace(canvas='canvas', transform_chain='chain',
                            color='color')
    source = SimpleNamespace(rgb=np.zeros((4, 4, 3), np.float32),
        protected=np.zeros((4, 4), np.float32), image=image)
    stage = Snapshot(canvas='canvas', transform_chain='chain',
                     color='color', dtype='float32')
    evidence = Snapshot(params='params', proposal='proposal',
                        stage_image='stage', selection='selection')
    case = SimpleNamespace(dust_evidence='evidence')
    item = SimpleNamespace(render_case=case, event_id=uuid4())
    documents = {'evidence': evidence, 'params': Snapshot(),
                 'proposal': Snapshot(raw_crop='crop'), 'stage': stage}
    artifacts = SimpleNamespace(contract=lambda ref, contract: documents[ref],
                                scratch=object())
    arms = [np.full((4, 4, 3), value, np.float32)
            for value in (.2, .4, .6)]
    workspace = SimpleNamespace(source=source, baseline=arms[0],
                                candidate=arms[1], approved=arms[2])
    profile = {'backend': 'cuda', 'renderer_version': 'v2.2',
        'resource_estimate': {'pixel_count': 160,
            'ram_bytes': 128*16 + 512*1_048_576,
            'scratch_bytes': 52*16 + 32768,
            'vram_bytes': 10_000_000_000, 'output_bytes': 8_388_608}}
    monkeypatch.setattr(replay, 'validate_holdout_dust_binding',
                        lambda *args: None)
    monkeypatch.setattr(replay, 'validate_frozen_dust_material',
                        lambda *args: ('selected',))
    monkeypatch.setattr(replay, 'accepted_dust_candidates',
                        lambda *args: [SimpleNamespace(id='selected')])
    prepared = None

    @contextmanager
    def prepare(rgb, crop, selected, scratch, **kwargs):
        nonlocal prepared
        assert id(rgb) in {id(arm) for arm in arms}
        prepared = rgb.copy() + np.float32(.1)
        yield SimpleNamespace(path='prepared', candidates=[
            SimpleNamespace(candidate_id='selected', applied_pixels=1)])

    @contextmanager
    def read(path, shape, dtype, check_active, **kwargs):
        assert path == 'prepared' and shape == (4, 4, 3)
        yield prepared

    monkeypatch.setattr(replay, 'prepare_dust_repair', prepare)
    monkeypatch.setattr(replay, 'read_array', read)
    with open_holdout_reviewed_dust(artifacts, item, workspace, _styles(),
                                    execution_profile=profile) as repair:
        for arm in arms:
            repair(arm)
        with pytest.raises(ValueError, match='HOLDOUT_DUST_ARM_INVALID'):
            repair(arms[0])
    for arm, value in zip(arms, (.2, .4, .6), strict=True):
        assert np.all(arm == np.float32(value) + np.float32(.1))
    with pytest.raises(ValueError, match='HOLDOUT_DUST_ARMS_INCOMPLETE'):
        with open_holdout_reviewed_dust(
                artifacts, item, workspace, _styles(),
                execution_profile=profile) as repair:
            repair(arms[0])
