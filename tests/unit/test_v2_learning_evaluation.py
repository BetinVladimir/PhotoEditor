"""Holdout aggregation cannot conceal one unsafe or regressed frame."""

from uuid import UUID
from contextlib import contextmanager, nullcontext
from dataclasses import replace

import pytest
import numpy as np
from photo_worker.operations.session.contracts import FaceObservations

from photo_worker.operations.learning.contracts import (
    EvaluationCase, LearningEvaluateParams,
)
from photo_worker.operations.learning.evaluation import (
    RenderedHoldout, aggregate_evaluation, evaluate_holdout,
    measure_render_case,
)
from test_v2_learning_fit_adapter import fixture, ref


def inputs():
    _, manifest, baseline, calibration = fixture()
    candidate = baseline.model_copy(deep=True)
    candidate.training_sha = f'{100:064x}'
    candidate.intercept[0] = .1
    params = LearningEvaluateParams(schema_version=1, candidate=ref(103),
        manifest=ref(100), baseline=manifest.baseline,
        calibration=manifest.calibration, model_lock_sha=manifest.model_lock_sha,
        icc_lock_sha=manifest.icc_lock_sha, renderer_version='v2.2')
    candidate.training_sha = params.manifest.sha256
    cases = [EvaluationCase(event_id=item.event_id, color_delta=-.1,
                            clipping_delta=0, identity_status='pass',
                            protected_max_error=0)
             for item in manifest.examples if item.split == 'holdout']
    return params, manifest, baseline, candidate, calibration, cases


def test_evaluation_accepts_measured_safe_improvement():
    args = inputs()
    result = aggregate_evaluation(*args)
    assert result.holdout_sessions == 2
    assert result.baseline_loss > 0
    assert result.candidate_loss < 1e-12
    assert result.metric_eligible and result.eligible
    assert result.safety_status == 'pass' and result.reasons == []


def test_one_regressed_case_or_unmeasurable_identity_blocks_activation():
    params, manifest, baseline, candidate, calibration, cases = inputs()
    cases[0].color_delta = .01
    result = aggregate_evaluation(params, manifest, baseline, candidate,
                                  calibration, cases)
    assert not result.metric_eligible and not result.eligible
    assert result.color_p95_delta == .01
    cases[0].color_delta = -.1
    cases[0].identity_status = 'not_measurable'
    result = aggregate_evaluation(params, manifest, baseline, candidate,
                                  calibration, cases)
    assert result.metric_eligible and not result.eligible
    assert result.safety_status == 'not_measurable'
    cases[1].protected_max_error = .000001
    result = aggregate_evaluation(params, manifest, baseline, candidate,
                                  calibration, cases)
    assert result.safety_status == 'fail' and result.safety_regression


def test_evaluation_requires_exact_holdout_evidence_and_training_sha():
    params, manifest, baseline, candidate, calibration, cases = inputs()
    with pytest.raises(ValueError, match='MISSING_EVALUATION_CASE'):
        changed = [*cases]
        changed[0] = changed[0].model_copy(update={'event_id': UUID(int=99999)})
        aggregate_evaluation(params, manifest, baseline, candidate,
                             calibration, changed)
    candidate.training_sha = 'f' * 64
    with pytest.raises(ValueError, match='INVALID_EVALUATION_INPUT'):
        aggregate_evaluation(params, manifest, baseline, candidate,
                             calibration, cases)
    candidate.training_sha = params.manifest.sha256
    manifest.examples[1].render_case.source_lineage_sha = (
        manifest.examples[0].render_case.source_lineage_sha)
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        aggregate_evaluation(params, manifest, baseline, candidate,
                             calibration, cases)


def test_render_metric_uses_exact_valid_pixels_and_protected_guard(tmp_path):
    baseline = np.full((1, 20, 3), .5, np.float32)
    candidate = baseline.copy()
    approved = baseline.copy()
    alpha = np.ones((1, 20), np.float32)
    protected = np.zeros((1, 20), np.float32)
    candidate[0, 0] = .8
    alpha[0, 0] = 0
    alpha[0, 19] = 0
    case = measure_render_case(UUID(int=1), baseline, candidate, approved,
                               alpha, protected, 'pass', tmp_path, tile=128)
    assert case.color_delta == 0 and case.protected_max_error == 0
    protected[0, 0] = 1
    case = measure_render_case(UUID(int=1), baseline, candidate, approved,
                               alpha, protected, 'pass', tmp_path, tile=128)
    assert case.protected_max_error > 0
    alpha[0, 0] = 1
    case = measure_render_case(UUID(int=1), baseline, candidate, approved,
                               alpha, protected, 'pass', tmp_path, tile=128)
    assert case.color_delta > 0  # nearest rank over 19 pixels includes one outlier
    alpha[:] = 0
    with pytest.raises(ValueError, match='EVALUATION_NOT_MEASURABLE'):
        measure_render_case(UUID(int=1), baseline, candidate, approved,
                            alpha, protected, 'pass', tmp_path, tile=128)
    assert list(tmp_path.iterdir()) == []


def test_render_metric_percentile_spans_tiles(tmp_path):
    baseline = np.full((1, 300, 3), .5, np.float32)
    candidate = baseline.copy()
    approved = baseline.copy()
    alpha = np.zeros((1, 300), np.float32)
    alpha[0, :18] = 1
    alpha[0, 280] = 1
    candidate[0, 280] = .8
    protected = np.zeros((1, 300), np.float32)
    case = measure_render_case(UUID(int=2), baseline, candidate, approved,
                               alpha, protected, 'pass', tmp_path, tile=128)
    assert case.color_delta > 0


def test_evaluate_holdout_requires_pinned_render_and_qualified_detector(tmp_path):
    params, manifest, baseline, candidate, calibration, _ = inputs()
    visited = []
    rgb = np.full((2, 2, 3), .5, np.float32)
    alpha = np.ones((2, 2), np.float32)
    protected = np.zeros((2, 2), np.float32)

    def render(item, base, fitted):
        assert base is baseline and fitted is candidate
        visited.append(item.event_id)
        return nullcontext(RenderedHoldout(item.event_id, item.render_case.binding,
            params.renderer_version, params.icc_lock_sha, params.model_lock_sha,
            rgb, rgb.copy(), rgb.copy(), alpha, protected,
            True, True, 0, False, 'no_person'))

    def source_observations(item):
        return FaceObservations(schema_version=1,
                                binding=item.render_case.binding, faces=[])

    result = evaluate_holdout(params, manifest, baseline, candidate,
                              calibration, render, source_observations, tmp_path)
    assert visited == [item.event_id for item in manifest.examples
                       if item.split == 'holdout']
    assert result.eligible and result.safety_status == 'pass'

    def unqualified(item, base, fitted):
        with render(item, base, fitted) as evidence:
            return nullcontext(replace(evidence, detector_qualified=False,
                                       identity_status='pass'))

    result = evaluate_holdout(params, manifest, baseline, candidate,
                              calibration, unqualified, source_observations, tmp_path)
    assert not result.eligible and result.safety_status == 'not_measurable'

    def wrong_binding(item, base, fitted):
        with render(item, base, fitted) as evidence:
            return nullcontext(replace(evidence, icc_lock_sha='0' * 64))

    with pytest.raises(ValueError, match='INVALID_EVALUATION_RENDER_BINDING'):
        evaluate_holdout(params, manifest, baseline, candidate,
                         calibration, wrong_binding, source_observations, tmp_path)

    def false_no_person(item, base, fitted):
        with render(item, base, fitted) as evidence:
            return nullcontext(replace(evidence, detected_faces=1))

    with pytest.raises(ValueError, match='INVALID_IDENTITY_EVIDENCE'):
        evaluate_holdout(params, manifest, baseline, candidate,
                         calibration, false_no_person, source_observations, tmp_path)


def test_holdout_render_mapping_stays_open_through_measurement(tmp_path, monkeypatch):
    import photo_worker.operations.learning.evaluation as evaluation
    params, manifest, baseline, candidate, calibration, _ = inputs()
    rgb = np.full((2, 2, 3), .5, np.float32)
    alpha = np.ones((2, 2), np.float32)
    protected = np.zeros((2, 2), np.float32)
    active = False
    closed = []
    original_measure = evaluation.measure_render_case

    def checked_measure(*args, **kwargs):
        assert active
        return original_measure(*args, **kwargs)

    @contextmanager
    def render(item, base, fitted):
        nonlocal active
        assert not active
        active = True
        try:
            yield RenderedHoldout(item.event_id, item.render_case.binding,
                params.renderer_version, params.icc_lock_sha,
                params.model_lock_sha, rgb, rgb, rgb, alpha, protected,
                True, True, 0, False, 'no_person')
        finally:
            active = False
            closed.append(item.event_id)

    monkeypatch.setattr(evaluation, 'measure_render_case', checked_measure)
    source = lambda item: FaceObservations(
        schema_version=1, binding=item.render_case.binding, faces=[])
    evaluate_holdout(params, manifest, baseline, candidate, calibration,
                     render, source, tmp_path)
    assert not active and len(closed) >= 10


@pytest.mark.parametrize('stop_at', [1, 4, 5, 6, 7])
def test_metric_cancellation_cleans_scratch_and_stops_tiles(tmp_path, monkeypatch, stop_at):
    from photo_worker.operations.learning import evaluation
    from photo_worker.runtime.executor import LeaseLost
    rgb = np.full((128, 256, 3), .5, np.float32)
    checks = 0
    conversions = 0
    original = evaluation.rgb_to_lab_d50

    def active():
        nonlocal checks
        checks += 1
        if checks == stop_at:
            raise LeaseLost('metric lease lost')

    def convert(*args, **kwargs):
        nonlocal conversions
        conversions += 1
        return original(*args, **kwargs)

    monkeypatch.setattr(evaluation, 'rgb_to_lab_d50', convert)
    with pytest.raises(LeaseLost, match='metric lease lost'):
        measure_render_case(UUID(int=1), rgb, rgb, rgb,
                            np.ones((128, 256), np.float32),
                            np.zeros((128, 256), np.float32), 'no_person',
                            tmp_path, tile=128, check_active=active)
    assert checks == stop_at
    # Two tiles, three conversions per tile; stop_at4 is before tile2.
    assert conversions == (0 if stop_at == 1 else 3 if stop_at == 4 else 6)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize('outcome', ['success', 'cancel', 'second_allocation'])
def test_metric_explicitly_closes_disk_mappings(tmp_path, monkeypatch, outcome):
    from photo_worker.operations.learning import evaluation
    from photo_worker.runtime.executor import LeaseLost
    original = evaluation.np.memmap
    mappings = []

    def mapped(*args, **kwargs):
        if outcome == 'second_allocation' and mappings:
            raise OSError('scratch allocation failed')
        result = original(*args, **kwargs)
        mappings.append(result)
        return result

    def active():
        if outcome == 'cancel' and len(mappings) == 2:
            raise LeaseLost('cancelled with mapped buffers')

    monkeypatch.setattr(evaluation.np, 'memmap', mapped)
    rgb = np.full((2, 2, 3), .5, np.float32)
    def measure():
        return measure_render_case(UUID(int=1), rgb, rgb, rgb,
            np.ones((2, 2), np.float32), np.zeros((2, 2), np.float32),
            'no_person', tmp_path, check_active=active)
    if outcome == 'success':
        assert measure().color_delta == 0
    else:
        expected = LeaseLost if outcome == 'cancel' else OSError
        with pytest.raises(expected):
            measure()
    assert len(mappings) == (1 if outcome == 'second_allocation' else 2)
    assert all(value._mmap.closed for value in mappings)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize('budget', [63, 64])
def test_metric_uses_attempt_disk_reservation(tmp_path, budget):
    from uuid import uuid4
    from photo_worker.runtime.scratch import ScratchSpace, DiskQuota
    rgb = np.full((2, 2, 3), .5, np.float32)
    with ScratchSpace(tmp_path.resolve(), uuid4(), uuid4(), 1, budget,
                      min_free_bytes=0) as scratch:
        def measure():
            return measure_render_case(UUID(int=1), rgb, rgb, rgb,
                np.ones((2, 2), np.float32), np.zeros((2, 2), np.float32),
                'no_person', scratch.directory, scratch_space=scratch)
        if budget < 64:
            with pytest.raises(DiskQuota):
                measure()
        else:
            assert measure().color_delta == 0
            assert measure().color_delta == 0  # Previous reservation released.
        assert list(scratch.directory.iterdir()) == []


@pytest.mark.parametrize('change', ['consent', 'duplicate', 'epoch'])
def test_evaluation_rechecks_manifest_consent_shape(change):
    params, manifest, baseline, candidate, calibration, cases = inputs()
    if change == 'consent':
        manifest.consents[0].consent_id = UUID(int=0)
    elif change == 'duplicate':
        manifest.consents.append(manifest.consents[0].model_copy(deep=True))
    else:
        manifest.consents[0].epoch = '9223372036854775808'
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        aggregate_evaluation(params, manifest, baseline, candidate, calibration, cases)


def test_evaluate_rejects_insufficient_train_partition_before_render(tmp_path):
    params, manifest, baseline, candidate, calibration, _ = inputs()
    held = [item for item in manifest.examples if item.split == 'holdout']
    train = [item for item in manifest.examples if item.split == 'train']
    # Preserve all four train sessions but retain only19 train examples.
    by_session = {}
    for item in train:
        by_session.setdefault(item.session_id, []).append(item)
    retained = [item for rows in by_session.values() for item in rows[:4]]
    retained.extend(rows[4] for rows in list(by_session.values())[:3])
    extra = held[0].model_copy(deep=True)
    extra.event_id = UUID(int=90000)
    extra.render_case.source_lineage_sha = 'f' * 64
    manifest.examples = [*held, extra, *retained]
    assert len(manifest.examples) == 40
    assert len({item.session_id for item in manifest.examples}) == 6
    with pytest.raises(ValueError, match='INVALID_HOLDOUT_SPLIT'):
        evaluate_holdout(params, manifest, baseline, candidate, calibration,
            lambda *args: pytest.fail('rendered insufficient training manifest'),
            lambda *args: pytest.fail('read insufficient training manifest faces'),
            tmp_path)


def test_evaluate_rejects_zero_subject_group():
    params, manifest, baseline, candidate, calibration, cases = inputs()
    manifest.examples[0] = manifest.examples[0].model_copy(update={'subject_group_id':UUID(int=0)})
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        aggregate_evaluation(params, manifest, baseline, candidate, calibration, cases)


def test_evaluate_rejects_manifest_policy_digest_substitution():
    params, manifest, baseline, candidate, calibration, cases = inputs()
    manifest.policy_sha = 'f' * 64
    with pytest.raises(ValueError, match='INVALID_TRAINING_MANIFEST'):
        aggregate_evaluation(params, manifest, baseline, candidate, calibration, cases)
