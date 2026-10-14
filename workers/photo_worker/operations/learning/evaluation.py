"""Deterministic V23 holdout aggregation over measured render evidence.

The caller must supply one case measured from the pinned renderer, ICC
conversion and identity verifier for each frozen holdout example. This module
never substitutes synthetic image metrics or an unqualified face verdict.
"""
from __future__ import annotations

from contextlib import ExitStack
from dataclasses import dataclass
from math import ceil, isfinite
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Callable, ContextManager, Literal
from uuid import UUID

import numpy as np
from skimage.color import deltaE_ciede2000

from photo_worker.algorithms.color import rgb_to_lab_d50
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.session.contracts import (
    Calibration, FaceObservations, VersionBinding,
)

from photo_worker.runtime.scratch import ScratchSpace

from .contracts import (CandidateProfile, Evaluation, EvaluationCase,
                        LearningEvaluateParams, LearningPolicy, TrainingExample, TrainingManifest)
from .freeze import _session_order
from .model import FittedDeltaModel, predict_delta


@dataclass(frozen=True)
class RenderedHoldout:
    """Pixel output and detector evidence from one pinned renderer invocation.

    The provider owns rendering and qualified detection. This type prevents
    an evaluator from treating an absent or failed detector as a safe frame.
    """
    event_id: UUID
    binding: VersionBinding
    renderer_version: str
    icc_lock_sha: str
    model_lock_sha: str
    baseline_rgb: np.ndarray
    candidate_rgb: np.ndarray
    approved_rgb: np.ndarray
    alpha: np.ndarray
    protected: np.ndarray
    detector_qualified: bool
    detector_succeeded: bool
    detected_faces: int
    verifier_qualified: bool
    identity_status: Literal['pass', 'fail', 'not_measurable', 'no_person']


def _qualified_identity(render: RenderedHoldout, source_faces: int) -> str:
    if (type(source_faces) is not int or source_faces < 0 or source_faces > 100 or
            type(render.detected_faces) is not int or render.detected_faces < 0 or
            render.detected_faces > 100 or
            type(render.detector_qualified) is not bool or
            type(render.detector_succeeded) is not bool or
            type(render.verifier_qualified) is not bool or
            render.identity_status not in ('pass', 'fail', 'not_measurable',
                                           'no_person')):
        raise ValueError('INVALID_IDENTITY_EVIDENCE')
    if render.identity_status == 'fail':
        return 'fail'
    if not render.detector_qualified or not render.detector_succeeded:
        return 'not_measurable'
    if (source_faces == 0) != (render.detected_faces == 0):
        if render.identity_status != 'not_measurable':
            raise ValueError('INVALID_IDENTITY_EVIDENCE')
        return 'not_measurable'
    if render.detected_faces == 0:
        if render.identity_status not in ('no_person', 'not_measurable'):
            raise ValueError('INVALID_IDENTITY_EVIDENCE')
        return render.identity_status
    if render.identity_status == 'no_person':
        raise ValueError('INVALID_IDENTITY_EVIDENCE')
    if not render.verifier_qualified:
        return 'not_measurable'
    return render.identity_status


def evaluate_holdout(params: LearningEvaluateParams,
                     manifest: TrainingManifest, baseline: CandidateProfile,
                     candidate: CandidateProfile, calibration: Calibration,
                     render: Callable[[TrainingExample, CandidateProfile,
                                       CandidateProfile], ContextManager[RenderedHoldout]],
                     source_observations: Callable[[TrainingExample], FaceObservations],
                     scratch: Path, *,
                     check_active: Callable[[], None] = lambda: None,
                     scratch_space: ScratchSpace | None = None) -> Evaluation:
    """Measure each frozen holdout event through an external qualified renderer.

    Rendering is deliberately an injected provider until the task adapter can
    invoke the complete pinned V2 image pipeline. No synthetic metric fallback
    is accepted here.
    """
    check_active()
    holdout = _validated_holdout(params, manifest, baseline, candidate,
                                 calibration)
    measured = []
    for item in holdout:
        check_active()
        with render(item, baseline, candidate) as evidence:
            check_active()
            if (not isinstance(evidence, RenderedHoldout) or
                    evidence.event_id != item.event_id or
                    evidence.binding != item.render_case.binding or
                    evidence.renderer_version != params.renderer_version or
                    evidence.icc_lock_sha != params.icc_lock_sha or
                    evidence.model_lock_sha != params.model_lock_sha):
                raise ValueError('INVALID_EVALUATION_RENDER_BINDING')
            frozen_faces = source_observations(item)
            check_active()
            if (not isinstance(frozen_faces, FaceObservations) or
                    frozen_faces.binding != item.render_case.binding):
                raise ValueError('INVALID_EVALUATION_FACE_BINDING')
            identity = _qualified_identity(evidence, len(frozen_faces.faces))
            measured.append(measure_render_case(
                item.event_id, evidence.baseline_rgb, evidence.candidate_rgb,
                evidence.approved_rgb, evidence.alpha, evidence.protected,
                identity, scratch, check_active=check_active,
                scratch_space=scratch_space))
    check_active()
    return aggregate_evaluation(params, manifest, baseline, candidate,
                                calibration, measured)


def _measure_render_case(event_id: UUID, baseline_rgb: np.ndarray,
                        candidate_rgb: np.ndarray, approved_rgb: np.ndarray,
                        alpha: np.ndarray, protected: np.ndarray,
                        identity_status: Literal['pass', 'fail',
                                                 'not_measurable', 'no_person'],
                        scratch: Path, tile: int = 256, *,
                        check_active: Callable[[], None] = lambda: None,
                        error_paths: tuple[Path, Path] | None = None) -> EvaluationCase:
    """Measure exact same valid pixels with disk-backed nearest-rank p95.

    RGB arrays are renderer outputs in the locked working color space. The
    caller verifies the ICC lock and obtains identity_status from the qualified
    face verifier. No image subsampling or averaged per-tile percentiles occur.
    """
    check_active()
    shape = baseline_rgb.shape
    if (event_id.int == 0 or len(shape) != 3 or shape[2] != 3 or
            shape[0] < 1 or shape[1] < 1 or shape[0] * shape[1] > 200_000_000 or
            candidate_rgb.shape != shape or approved_rgb.shape != shape or
            alpha.shape != shape[:2] or protected.shape != shape[:2] or
            tile not in (128, 256, 512, 1024)):
        raise ValueError('INVALID_EVALUATION_RENDER')
    scratch.mkdir(parents=True, exist_ok=True)
    max_pixels = shape[0] * shape[1]
    baseline_path, candidate_path = error_paths or (
        scratch / 'baseline_delta_e.f64', scratch / 'candidate_delta_e.f64')
    # Close mappings even when an exception traceback retains local arrays.
    with ExitStack() as buffers:
        baseline_error = np.memmap(baseline_path, mode='w+',
                                   dtype=np.float64, shape=(max_pixels,))
        buffers.callback(baseline_error._mmap.close)
        candidate_error = np.memmap(candidate_path, mode='w+',
                                    dtype=np.float64, shape=(max_pixels,))
        buffers.callback(candidate_error._mmap.close)
        count = baseline_clipped = candidate_clipped = 0
        protected_max = 0.0
        for y in range(0, shape[0], tile):
            for x in range(0, shape[1], tile):
                check_active()
                sl = np.s_[y:y + tile, x:x + tile]
                b = np.asarray(baseline_rgb[sl], dtype=np.float64)
                c = np.asarray(candidate_rgb[sl], dtype=np.float64)
                a = np.asarray(approved_rgb[sl], dtype=np.float64)
                validity = np.asarray(alpha[sl])
                guard = np.asarray(protected[sl])
                if (not all(np.isfinite(v).all() for v in (b, c, a, validity, guard))
                        or np.any(validity < 0) or np.any(validity > 1)
                        or np.any(guard < 0) or np.any(guard > 1)):
                    raise ValueError('INVALID_EVALUATION_RENDER')
                covered = guard > 0
                if np.any(covered):
                    protected_max = max(protected_max,
                                        float(np.max(np.abs(c[covered] - b[covered]))))
                valid = validity > 0
                number = int(np.count_nonzero(valid))
                if number == 0:
                    continue
                lab_approved = rgb_to_lab_d50(a[valid])
                baseline_error[count:count + number] = deltaE_ciede2000(
                    rgb_to_lab_d50(b[valid]), lab_approved)
                candidate_error[count:count + number] = deltaE_ciede2000(
                    rgb_to_lab_d50(c[valid]), lab_approved)
                baseline_clipped += int(np.count_nonzero(np.any(
                    (b[valid] < 0) | (b[valid] >= 1), axis=1)))
                candidate_clipped += int(np.count_nonzero(np.any(
                    (c[valid] < 0) | (c[valid] >= 1), axis=1)))
                count += number
        if count == 0:
            raise ValueError('EVALUATION_NOT_MEASURABLE')
        check_active()
        rank = ceil(.95 * count) - 1
        baseline_error[:count].partition(rank)
        check_active()
        candidate_error[:count].partition(rank)
        check_active()
        color_delta = float(candidate_error[rank] - baseline_error[rank])
        clipping_delta = (candidate_clipped - baseline_clipped) / count
        if not isfinite(color_delta):
            raise ValueError('EVALUATION_NOT_MEASURABLE')
        return EvaluationCase(event_id=event_id, color_delta=color_delta,
                              clipping_delta=clipping_delta,
                              identity_status=identity_status,
                              protected_max_error=protected_max)


def measure_render_case(event_id: UUID, baseline_rgb: np.ndarray,
                        candidate_rgb: np.ndarray, approved_rgb: np.ndarray,
                        alpha: np.ndarray, protected: np.ndarray,
                        identity_status: Literal['pass', 'fail',
                                                 'not_measurable', 'no_person'],
                        scratch: Path, tile: int = 256, *,
                        check_active: Callable[[], None] = lambda: None,
                        scratch_space: ScratchSpace | None = None) -> EvaluationCase:
    check_active()
    if scratch_space is not None:
        shape = baseline_rgb.shape
        if (len(shape) != 3 or shape[2] != 3 or shape[0] < 1 or shape[1] < 1 or
                shape[0] * shape[1] > 200_000_000):
            raise ValueError('INVALID_EVALUATION_RENDER')
        with scratch_space.reserve(shape[0] * shape[1] * 16) as reservation:
            paths = (reservation.path('baseline-delta-e', '.bin'),
                     reservation.path('candidate-delta-e', '.bin'))
            result = _measure_render_case(
                event_id, baseline_rgb, candidate_rgb, approved_rgb, alpha,
                protected, identity_status, scratch_space.directory, tile,
                check_active=check_active, error_paths=paths)
            check_active()
            reservation.validate()
            return result
    scratch.mkdir(parents=True, exist_ok=True)
    with TemporaryDirectory(prefix='learning-evaluate-', dir=scratch) as directory:
        return _measure_render_case(event_id, baseline_rgb, candidate_rgb,
                                    approved_rgb, alpha, protected,
                                    identity_status, Path(directory), tile,
                                    check_active=check_active)


def _model(profile: CandidateProfile) -> FittedDeltaModel:
    return FittedDeltaModel(tuple(profile.means), tuple(profile.scales),
                            tuple(tuple(row) for row in profile.coefficients),
                            tuple(profile.intercept), profile.ridge_lambda)


def _loss(prediction: tuple[float, ...], target: tuple[float, ...],
          calibration: Calibration) -> float:
    result = sum(weight * abs(actual - desired) / scale
                 for weight, actual, desired, scale in zip(
                     calibration.loss_weights, prediction, target,
                     calibration.normalization_scales, strict=True))
    if not isfinite(result) or result < 0:
        raise ValueError('INVALID_HOLDOUT_LOSS')
    return result


def _validated_holdout(params: LearningEvaluateParams,
                       manifest: TrainingManifest, baseline: CandidateProfile,
                       candidate: CandidateProfile,
                       calibration: Calibration) -> list[TrainingExample]:
    manifest.identity_scope()
    expected_policy = LearningPolicy(schema_version=1, mode=manifest.mode,
        min_examples=40, min_sessions=6, holdout_fraction=.2, seed=2026,
        calibration=manifest.calibration)
    if canonical_hash(expected_policy) != manifest.policy_sha:
        raise ValueError('INVALID_TRAINING_MANIFEST')
    if (params.manifest.sha256 == '0' * 64 or
            params.candidate.sha256 == '0' * 64 or
            params.baseline != manifest.baseline or
            params.calibration != manifest.calibration or
            params.model_lock_sha != manifest.model_lock_sha or
            params.icc_lock_sha != manifest.icc_lock_sha or
            params.renderer_version != manifest.renderer_version or
            candidate.profile_id != manifest.profile_id or
            baseline.profile_id != manifest.profile_id or
            candidate.training_sha != params.manifest.sha256 or
            not calibration.qualified or
            calibration.model_lock_sha != manifest.model_lock_sha or
            abs(sum(calibration.loss_weights) - 1.0) > 1e-9):
        raise ValueError('INVALID_EVALUATION_INPUT')
    sessions = sorted({item.session_id for item in manifest.examples},
                      key=_session_order)
    if len(sessions) < 6:
        raise ValueError('INSUFFICIENT_DATA')
    if ((manifest.mode == 'person') != (manifest.person_id is not None) or
            len({item.event_id for item in manifest.examples}) != len(manifest.examples) or
            len({item.render_case.source_lineage_sha for item in manifest.examples})
            != len(manifest.examples) or
            any(item.event_id.int == 0 or item.session_id.int == 0 or
                item.render_case.binding.source != item.render_case.working_image or
                item.render_case.approval_id.int == 0 or
                (manifest.mode == 'person' and
                 item.subject_group_id != manifest.person_id)
                for item in manifest.examples)):
        raise ValueError('INVALID_TRAINING_MANIFEST')
    expected_holdout = set(sessions[:max(2, ceil(.2 * len(sessions)))])
    holdout = [item for item in manifest.examples
               if item.session_id in expected_holdout]
    if (len(holdout) < 10 or len(manifest.examples) - len(holdout) < 20 or
            len(sessions) - len(expected_holdout) < 4 or
            any((item.split == 'holdout') !=
                (item.session_id in expected_holdout)
                for item in manifest.examples)):
        raise ValueError('INVALID_HOLDOUT_SPLIT')
    return holdout


def aggregate_evaluation(params: LearningEvaluateParams,
                         manifest: TrainingManifest, baseline: CandidateProfile,
                         candidate: CandidateProfile, calibration: Calibration,
                         measured: list[EvaluationCase]) -> Evaluation:
    """Accept only exact holdout evidence; compute immutable aggregate gates."""
    holdout = _validated_holdout(params, manifest, baseline, candidate,
                                 calibration)
    if len(measured) != len(holdout):
        raise ValueError('INVALID_HOLDOUT_SPLIT')
    measured_by_event: dict[UUID, EvaluationCase] = {}
    for case in measured:
        if case.event_id in measured_by_event or case.event_id.int == 0:
            raise ValueError('DUPLICATE_EVALUATION_CASE')
        measured_by_event[case.event_id] = case
    if set(measured_by_event) != {item.event_id for item in holdout}:
        raise ValueError('MISSING_EVALUATION_CASE')
    ordered = [measured_by_event[item.event_id] for item in holdout]
    baseline_model, candidate_model = _model(baseline), _model(candidate)
    baseline_loss = candidate_loss = 0.0
    for item in holdout:
        features = tuple(item.features)
        target = item.target
        desired = (target.ev, target.log_r, target.log_g, target.log_b,
                   target.saturation_delta, target.retouch_delta)
        baseline_loss += _loss(predict_delta(baseline_model, features),
                               desired, calibration)
        candidate_loss += _loss(predict_delta(candidate_model, features),
                                desired, calibration)
    baseline_loss /= len(holdout)
    candidate_loss /= len(holdout)
    color = max(case.color_delta for case in ordered)
    clipping = max(case.clipping_delta for case in ordered)
    safety_regression = any(case.identity_status == 'fail' or
                            case.protected_max_error > 0 for case in ordered)
    if safety_regression:
        safety_status = 'fail'
    elif any(case.identity_status == 'not_measurable' for case in ordered):
        safety_status = 'not_measurable'
    else:
        safety_status = 'pass'
    reasons = []
    if baseline_loss <= 1e-12:
        reasons.append('NO_IMPROVEMENT_POSSIBLE')
    elif candidate_loss > .9 * baseline_loss:
        reasons.append('LOSS_REGRESSION')
    if color > 0:
        reasons.append('COLOR_REGRESSION')
    if clipping > 0:
        reasons.append('CLIPPING_REGRESSION')
    if safety_status == 'fail':
        reasons.append('SAFETY_REGRESSION')
    elif safety_status == 'not_measurable':
        reasons.append('SAFETY_NOT_MEASURABLE')
    holdout_sessions = len({item.session_id for item in holdout})
    metric_eligible = baseline_loss > 1e-12 and candidate_loss <= .9 * baseline_loss \
        and color <= 0 and clipping <= 0 and holdout_sessions >= 2
    return Evaluation(schema_version=1, candidate_sha=params.candidate.sha256,
                      manifest_sha=params.manifest.sha256,
                      holdout_sessions=holdout_sessions,
                      baseline_loss=baseline_loss,
                      candidate_loss=candidate_loss,
                      color_p95_delta=color, clipping_delta_fraction=clipping,
                      safety_regression=safety_regression,
                      eligible=metric_eligible and safety_status == 'pass',
                      reasons=reasons, metric_eligible=metric_eligible,
                      safety_status=safety_status, cases=ordered)
