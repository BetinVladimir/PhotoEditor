"""V23 ridge learning: exact feature order, split isolation and CV tie rules."""

from math import isclose, log
from uuid import uuid4

import pytest

from photo_worker.operations.learning.model import (
    FittedDeltaModel, TrainExample, fit_delta_model, predict_delta,
    scene12_features,
)


WEIGHTS = (0.4, 0.1, 0.1, 0.1, 0.2, 0.1)
SCALES = (1.0, 0.2, 0.2, 0.2, 0.15, 0.15)


def examples(zero=False):
    sessions = [uuid4() for _ in range(4)]
    rows = []
    for session_index, session in enumerate(sessions):
        for index in range(6):
            value = (session_index * 6 + index) / 24
            features = (value, *(0.0 for _ in range(11)))
            target = (0.0 if zero else value * 0.5,
                      0.0, 0.0, 0.0, 0.0, 0.0)
            rows.append(TrainExample(uuid4(), session, features, target))
    return rows


def test_scene12_order_and_missing_neutral_indicator():
    actual = scene12_features((0.1, 0.2, 0.3, 0.4, 0.5),
                              (0.5, 1.0, 0.0), 0.05, 3.0, 0.6)
    assert actual[:5] == (0.1, 0.2, 0.3, 0.4, 0.5)
    assert isclose(actual[5], log(0.5))
    assert actual[6] == 0
    assert isclose(actual[7], log(1e-6))
    assert actual[8:] == (1.0, 0.05, 0.75, 0.6)
    assert scene12_features(actual[:5], None, 0.05, 3.0, 0.6)[5:9] == (
        0.0, 0.0, 0.0, 0.0)
    with pytest.raises(ValueError, match='INVALID_SCENE_DESCRIPTOR'):
        scene12_features(actual[:5], None, 0.0, -1.0, 0.0)


def test_fit_recovers_train_signal_and_constant_feature_scale():
    fitted = fit_delta_model(examples(), WEIGHTS, SCALES)
    assert fitted.ridge_lambda in (0.01, 0.1, 1.0, 10.0)
    assert len(fitted.means) == len(fitted.scales) == len(fitted.coefficients) == 12
    assert all(scale == 1.0 for scale in fitted.scales[1:])
    assert all(len(row) == 6 for row in fitted.coefficients)
    assert fitted.coefficients[0][0] > 0
    value = 0.5
    prediction = (value - fitted.means[0]) / fitted.scales[0] * \
        fitted.coefficients[0][0] + fitted.intercept[0]
    assert abs(prediction - 0.25) < 0.02


def test_cv_equal_losses_choose_larger_lambda_and_reject_unsafe_targets():
    fitted = fit_delta_model(examples(zero=True), WEIGHTS, SCALES)
    assert fitted.ridge_lambda == 10.0
    assert all(value == 0 for row in fitted.coefficients for value in row)
    bad = examples()
    bad[0] = TrainExample(bad[0].event_id, bad[0].session_id,
                          bad[0].features, (0, 0, 0, 0, 0.151, 0))
    with pytest.raises(ValueError, match='INVALID_TRAINING_EXAMPLE'):
        fit_delta_model(bad, WEIGHTS, SCALES)


def test_fit_requires_four_train_sessions_and_twenty_examples():
    with pytest.raises(ValueError, match='INSUFFICIENT_DATA'):
        fit_delta_model(examples()[:18], WEIGHTS, SCALES)
    rows = examples()
    same_session = rows[12].session_id
    rows[18:] = [TrainExample(row.event_id, same_session,
                              row.features, row.target) for row in rows[18:]]
    with pytest.raises(ValueError, match='INSUFFICIENT_DATA'):
        fit_delta_model(rows, WEIGHTS, SCALES)


def test_inference_clamps_each_correction_coordinate():
    model = FittedDeltaModel((0.0,) * 12, (1.0,) * 12,
                             ((0.0,) * 6,) * 12,
                             (2.0, -1.0, 0.3, -0.3, 0.4, -0.4), 1.0)
    assert predict_delta(model, (0.0,) * 12) == (
        1.0, -0.223144, 0.223144, -0.223144, 0.15, -0.15)
    with pytest.raises(ValueError, match='INVALID_CANDIDATE_PROFILE'):
        predict_delta(model, (0.0,) * 11)


@pytest.mark.parametrize('stop_after_fit', [0, 1, 17])
def test_fit_observes_activity_before_between_and_after_folds(monkeypatch, stop_after_fit):
    from photo_worker.operations.learning import model
    rows = examples()
    # Fixture has four train sessions:16 CV solves followed by one final fit.
    fits = 0
    original = model._fit
    def fit(*args):
        nonlocal fits
        result = original(*args)
        fits += 1
        return result
    def active():
        if fits >= stop_after_fit:
            raise RuntimeError('lease no longer active')
    monkeypatch.setattr(model, '_fit', fit)
    with pytest.raises(RuntimeError, match='lease no longer active'):
        model.fit_delta_model(rows, WEIGHTS, SCALES, check_active=active)
    assert fits == stop_after_fit
