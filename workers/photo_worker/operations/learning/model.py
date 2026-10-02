"""Deterministic scene12_v1 features and leakage-free session ridge fitting.

The caller freezes eligible correction events and the train/holdout split.
Only train examples enter this module; holdout examples are reserved for the
separate immutable evaluation operation.
"""
from __future__ import annotations

from dataclasses import dataclass
from math import isfinite, log
from uuid import UUID

import numpy as np


FEATURE_COUNT = 12
TARGET_COUNT = 6
RIDGE_LAMBDAS = (0.01, 0.1, 1.0, 10.0)
FEATURE_EPS = 1e-6
TARGET_LIMITS = np.asarray((1.0, 0.223144, 0.223144, 0.223144,
                            0.15, 0.15), dtype=np.float64)


@dataclass(frozen=True)
class TrainExample:
    event_id: UUID
    session_id: UUID
    features: tuple[float, ...]
    target: tuple[float, ...]


@dataclass(frozen=True)
class FittedDeltaModel:
    means: tuple[float, ...]
    scales: tuple[float, ...]
    coefficients: tuple[tuple[float, ...], ...]
    intercept: tuple[float, ...]
    ridge_lambda: float


def scene12_features(luma_quantiles: tuple[float, ...],
                     neutral_rgb: tuple[float, float, float] | None,
                     clipping_fraction: float, sharpness: float,
                     person_fraction: float) -> tuple[float, ...]:
    """Map one frozen pre-edit descriptor into the normative feature order."""
    if (len(luma_quantiles) != 5 or
            any(not isfinite(value) or not 0 <= value <= 1
                for value in luma_quantiles) or
            any(not isfinite(value) or not 0 <= value <= 1
                for value in (clipping_fraction, person_fraction)) or
            not isfinite(sharpness) or not 0 <= sharpness <= 1e6):
        raise ValueError('INVALID_SCENE_DESCRIPTOR')
    if neutral_rgb is None:
        neutral = (0.0, 0.0, 0.0)
        present = 0.0
    else:
        if (len(neutral_rgb) != 3 or any(not isfinite(value) or not 0 <= value <= 1
                                         for value in neutral_rgb)):
            raise ValueError('INVALID_SCENE_DESCRIPTOR')
        neutral = tuple(log(max(value, FEATURE_EPS)) for value in neutral_rgb)
        present = 1.0
    return (*luma_quantiles, *neutral, present, clipping_fraction,
            sharpness / (1.0 + sharpness), person_fraction)


def _validated_training(examples: list[TrainExample], weights: tuple[float, ...],
                        loss_scales: tuple[float, ...]) -> tuple[np.ndarray, np.ndarray,
                                                                 list[UUID], np.ndarray,
                                                                 np.ndarray]:
    sessions = {example.session_id for example in examples}
    if len(examples) < 20 or len(sessions) < 4 or len(examples) > 5000:
        raise ValueError('INSUFFICIENT_DATA')
    if (len(weights) != TARGET_COUNT or len(loss_scales) != TARGET_COUNT or
            any(not isfinite(value) or value < 0 for value in weights) or
            abs(sum(weights) - 1.0) > 1e-9 or
            any(not isfinite(value) or value <= 0 for value in loss_scales)):
        raise ValueError('INVALID_LOSS_CALIBRATION')
    ids = [example.event_id for example in examples]
    if (len(set(ids)) != len(ids) or any(item.int == 0 for item in ids) or
            any(item.int == 0 for item in sessions)):
        raise ValueError('INVALID_TRAINING_EXAMPLE')
    x = np.asarray([example.features for example in examples], dtype=np.float64)
    y = np.asarray([example.target for example in examples], dtype=np.float64)
    if (x.shape != (len(examples), FEATURE_COUNT) or
            y.shape != (len(examples), TARGET_COUNT) or
            not np.isfinite(x).all() or not np.isfinite(y).all() or
            np.any(np.abs(x) > 100) or
            np.any(np.abs(y) > TARGET_LIMITS)):
        raise ValueError('INVALID_TRAINING_EXAMPLE')
    return x, y, [example.session_id for example in examples], \
        np.asarray(weights, dtype=np.float64), np.asarray(loss_scales, dtype=np.float64)


def _fit(x: np.ndarray, y: np.ndarray, ridge_lambda: float) -> tuple[np.ndarray,
                                                                     np.ndarray,
                                                                     np.ndarray,
                                                                     np.ndarray]:
    means = np.mean(x, axis=0, dtype=np.float64)
    scales = np.std(x, axis=0, dtype=np.float64)
    scales[scales < 1e-8] = 1.0
    standardized = (x - means) / scales
    x_mean = np.mean(standardized, axis=0)
    y_mean = np.mean(y, axis=0)
    xc = standardized - x_mean
    yc = y - y_mean
    coefficients = np.linalg.solve(
        xc.T @ xc + ridge_lambda * np.eye(FEATURE_COUNT, dtype=np.float64),
        xc.T @ yc,
    )
    intercept = y_mean - x_mean @ coefficients
    return means, scales, coefficients, intercept


def fit_delta_model(examples: list[TrainExample],
                    weights: tuple[float, ...],
                    loss_scales: tuple[float, ...]) -> FittedDeltaModel:
    """Choose lambda by leave-one-train-session-out CV, then fit all train rows.

    Every held-out CV session is transformed using statistics from its own
    fold's training sessions. Equal losses within 1e-12 favor larger lambda.
    """
    x, y, session_ids, w, scales = _validated_training(examples, weights, loss_scales)
    sessions = sorted(set(session_ids), key=str)
    scores = []
    for ridge_lambda in RIDGE_LAMBDAS:
        total_loss = 0.0
        total_rows = 0
        for session in sessions:
            valid = np.asarray([item == session for item in session_ids], dtype=bool)
            train = ~valid
            if not np.any(valid) or not np.any(train):
                raise ValueError('INSUFFICIENT_DATA')
            means, feature_scales, coefficients, intercept = _fit(
                x[train], y[train], ridge_lambda)
            prediction = np.clip(
                ((x[valid] - means) / feature_scales) @ coefficients + intercept,
                -TARGET_LIMITS, TARGET_LIMITS)
            loss = np.sum(np.abs(prediction - y[valid]) / scales * w, axis=1)
            total_loss += float(np.sum(loss, dtype=np.float64))
            total_rows += int(np.sum(valid))
        scores.append(total_loss / total_rows)
    best = min(scores)
    selected = max(ridge_lambda for ridge_lambda, score in zip(RIDGE_LAMBDAS,
                                                               scores, strict=True)
                   if score <= best + 1e-12)
    means, feature_scales, coefficients, intercept = _fit(x, y, selected)
    if (not np.isfinite(means).all() or not np.isfinite(feature_scales).all() or
            not np.isfinite(coefficients).all() or not np.isfinite(intercept).all() or
            np.any(np.abs(means) > 100) or np.any(feature_scales > 1e8) or
            np.any(feature_scales < 1e-8) or np.any(np.abs(coefficients) > 1e6) or
            np.any(np.abs(intercept) > 1e6)):
        raise ValueError('MODEL_OUT_OF_BOUNDS')
    return FittedDeltaModel(
        tuple(float(value) for value in means),
        tuple(float(value) for value in feature_scales),
        tuple(tuple(float(value) for value in row) for row in coefficients),
        tuple(float(value) for value in intercept), selected,
    )


def predict_delta(model: FittedDeltaModel,
                  features: tuple[float, ...]) -> tuple[float, ...]:
    """Resolve one pinned CandidateProfile and clamp to CorrectionVector bounds."""
    x = np.asarray(features, dtype=np.float64)
    means = np.asarray(model.means, dtype=np.float64)
    scales = np.asarray(model.scales, dtype=np.float64)
    coefficients = np.asarray(model.coefficients, dtype=np.float64)
    intercept = np.asarray(model.intercept, dtype=np.float64)
    if (x.shape != (FEATURE_COUNT,) or means.shape != (FEATURE_COUNT,) or
            scales.shape != (FEATURE_COUNT,) or
            coefficients.shape != (FEATURE_COUNT, TARGET_COUNT) or
            intercept.shape != (TARGET_COUNT,) or
            not all(np.isfinite(value).all() for value in
                    (x, means, scales, coefficients, intercept)) or
            np.any(scales < 1e-8) or np.any(scales > 1e8) or
            np.any(np.abs(x) > 100) or np.any(np.abs(means) > 100)):
        raise ValueError('INVALID_CANDIDATE_PROFILE')
    predicted = ((x - means) / scales) @ coefficients + intercept
    if not np.isfinite(predicted).all():
        raise ValueError('INVALID_CANDIDATE_PROFILE')
    bounded = np.clip(predicted, -TARGET_LIMITS, TARGET_LIMITS)
    return tuple(float(value) for value in bounded)
