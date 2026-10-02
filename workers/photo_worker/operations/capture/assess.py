"""Deterministic pixel checks; model-backed checks require separate evidence."""

from dataclasses import dataclass
import math

import cv2
import numpy as np

from photo_worker.operations.session.describe_features import _resize
from photo_worker.operations.session.contracts import Calibration, FaceObservations, VersionBinding
from .contracts import CaptureContext, CapturePolicy, QACheck, QAReport, ShotList


@dataclass(frozen=True)
class PixelCheck:
    code: str
    status: str
    score: float
    threshold: float


def _rgb(source: np.ndarray) -> np.ndarray:
    rgb = np.asarray(source)
    if (rgb.ndim != 3 or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1
            or rgb.shape[0] * rgb.shape[1] > 16_000_000
            or not np.issubdtype(rgb.dtype, np.number) or np.iscomplexobj(rgb)
            or not np.isfinite(rgb).all()):
        raise ValueError('INVALID_CAPTURE_PROXY')
    return rgb.astype(np.float64, copy=False)


def focus_score(linear_rgb: np.ndarray) -> float:
    rgb = _rgb(linear_rgb)
    if max(rgb.shape[:2]) > 2048:
        rgb = _resize(rgb, longest=2048)
    normalized = _resize(rgb, shortest=512)
    luma = normalized @ np.array([.2126, .7152, .0722], np.float64)
    laplacian = cv2.Laplacian(luma, cv2.CV_64F, ksize=3)
    q = float(np.mean(np.square(laplacian), dtype=np.float64)) * 10000
    if not math.isfinite(q):
        raise ValueError('INVALID_CAPTURE_FOCUS')
    return q / (1 + q)


def clipping_score(linear_rgb: np.ndarray) -> float:
    rgb = _rgb(linear_rgb)
    # Exact black and white remain valid; only values outside [0,1] clip.
    return float(np.mean(np.any((rgb < 0) | (rgb > 1), axis=2)))


def assess_focus(linear_rgb: np.ndarray, critical: float, warning: float) -> PixelCheck:
    if not 0 <= critical < warning <= 1:
        raise ValueError('INVALID_FOCUS_CALIBRATION')
    score = focus_score(linear_rgb)
    status = 'critical' if score < critical else 'warning' if score < warning else 'pass'
    return PixelCheck('focus', status, score, critical if status == 'critical' else warning)


def assess_clipping(linear_rgb: np.ndarray, warning: float, critical: float) -> PixelCheck:
    if not 0 <= warning < critical <= 1:
        raise ValueError('INVALID_CLIPPING_CALIBRATION')
    score = clipping_score(linear_rgb)
    status = 'critical' if score > critical else 'warning' if score > warning else 'pass'
    return PixelCheck('clipping', status, score, critical if status == 'critical' else warning)


@dataclass(frozen=True)
class FaceModelEvidence:
    qualified: bool
    eye_probabilities: tuple[float, float] | None = None
    glare_fraction: float | None = None


def assess_capture(rgb: np.ndarray, binding: VersionBinding, faces: FaceObservations,
                   policy: CapturePolicy, calibration: Calibration, context: CaptureContext,
                   shots: ShotList, model_lock_sha: str,
                   original_content_sha: str,
                   evidence: dict | None = None) -> QAReport:
    rgb = _rgb(rgb)
    if len(original_content_sha) != 64 or any(c not in '0123456789abcdef' for c in original_content_sha):
        raise ValueError('INVALID_CAPTURE_ORIGINAL_SHA')
    if faces.binding != binding:
        raise ValueError('CAPTURE_FACE_BINDING_MISMATCH')
    if not calibration.qualified or calibration.model_lock_sha != model_lock_sha:
        raise ValueError('CAPTURE_CALIBRATION_UNQUALIFIED')
    if not calibration.focus_critical < calibration.focus_warning or not calibration.clipping_warning_fraction < calibration.clipping_critical_fraction:
        raise ValueError('INVALID_CAPTURE_CALIBRATION')
    if len({face.face_id for face in faces.faces}) != len(faces.faces):
        raise ValueError('DUPLICATE_CAPTURE_FACE')
    coverage = {item.requirement_id: item.confirmed_frames for item in context.coverage}
    if set(coverage) != {item.id for item in shots.requirements}:
        raise ValueError('CAPTURE_COVERAGE_MISMATCH')
    checks = []
    evidence = evidence or {}
    def unavailable(code, face=None, reason='MODEL_UNAVAILABLE'):
        checks.append(QACheck(code=code, status='unavailable', face_id=None if face is None else face.face_id,
                              score=None, threshold=None, region=None if face is None else face.box, reason=reason))
    def pixel(result, face=None):
        checks.append(QACheck(code=result.code, status=result.status,
            face_id=None if face is None else face.face_id, score=result.score, threshold=result.threshold,
            region=None if face is None else face.box, reason=None))
    if 'focus' in policy.checks:
        pixel(assess_focus(rgb, calibration.focus_critical, calibration.focus_warning))
    if 'clipping' in policy.checks:
        pixel(assess_clipping(rgb, calibration.clipping_warning_fraction, calibration.clipping_critical_fraction))
    height, width = rgb.shape[:2]
    for face in faces.faces:
        box = face.box
        x0, y0 = int(box.x0 * width), int(box.y0 * height)
        x1, y1 = int(box.x1 * width), int(box.y1 * height)
        usable = min(x1 - x0, y1 - y0) >= calibration.min_face_edge_px and face.quality > 0
        if 'focus' in policy.checks:
            if usable:
                pixel(assess_focus(rgb[y0:y1, x0:x1], calibration.focus_critical, calibration.focus_warning), face)
            else:
                unavailable('focus', face, 'FACE_ROI_UNAVAILABLE')
        model = evidence.get(face.face_id)
        for code in ('eyes', 'glare'):
            if code not in policy.checks:
                continue
            if not usable:
                unavailable(code, face, 'FACE_ROI_UNAVAILABLE')
                continue
            if model is None or not model.qualified:
                unavailable(code, face)
                continue
            values = model.eye_probabilities if code == 'eyes' else None if model.glare_fraction is None else (model.glare_fraction,)
            if values is None:
                unavailable(code, face)
                continue
            if len(values) != (2 if code == 'eyes' else 1) or any(not math.isfinite(value) or not 0 <= value <= 1 for value in values):
                raise ValueError('CAPTURE_MODEL_OUTPUT_INVALID')
            score = min(values) if code == 'eyes' else values[0]
            threshold = calibration.eye_open_min if code == 'eyes' else calibration.glare_warning_fraction
            warning = score < threshold if code == 'eyes' else score > threshold
            checks.append(QACheck(code=code, status='warning' if warning else 'pass', face_id=face.face_id,
                                 score=score, threshold=threshold, region=face.box, reason=None))
    if not faces.faces:
        for code in ('eyes', 'glare'):
            if code in policy.checks:
                unavailable(code, reason='FACE_ROI_UNAVAILABLE')
    if 'duplicate' in policy.checks:
        duplicate = original_content_sha in context.recent_content_shas
        checks.append(QACheck(code='duplicate', status='warning' if duplicate else 'pass', face_id=None,
                             score=float(duplicate), threshold=0., region=None, reason=None))
    if 'shot_list' in policy.checks:
        if not shots.requirements:
            unavailable('shot_list', reason='NO_SHOT_REQUIREMENTS')
        for requirement in shots.requirements:
            count = coverage[requirement.id]
            checks.append(QACheck(code='shot_list', status='pass' if count >= requirement.min_confirmed_frames else 'warning',
                face_id=None, score=min(1., count / requirement.min_confirmed_frames),
                threshold=float(requirement.min_confirmed_frames), region=None,
                reason='SHOT_REQUIREMENT/' + str(requirement.id)))
    return QAReport(schema_version=1, binding=binding, checks=checks)
