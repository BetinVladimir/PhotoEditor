"""Frozen V23 beauty uses the same multi-face stage as portrait.beauty."""
from __future__ import annotations

from photo_worker.algorithms.effective import effective_beauty_params
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import Face68
from photo_worker.operations.portrait.prepare import FaceInput, prepare_beauty
from photo_worker.operations.session.contracts import FaceObservations
from photo_worker.runtime.arrays import read_array


def render_holdout_beauty(artifacts, source, target, style, observations, *,
                          skin_probability=None, landmarks=(), check_active=lambda: None):
    """Composite retouch after aligned color; never substitute skipped faces.

    Source observations are immutable and in normalized coordinates on the
    same canvas. Protection and filter/texture behavior are shared with the
    production portrait stage. All temporary files remain attempt-scoped.
    """
    check_active()
    if not style.beauty.enabled:
        return
    if (not isinstance(observations, FaceObservations) or
            observations.binding.canvas_hash != canonical_hash(source.image.canvas)):
        raise ValueError('HOLDOUT_BEAUTY_FACE_BINDING')
    # Binding is checked against the RenderCase by the triplet caller; canvas
    # and transform are pinned by open_holdout_source before mapping pixels.
    if (not isinstance(landmarks, tuple) or len(landmarks) != len(observations.faces)
            or any(not isinstance(face, Face68) for face in landmarks)):
        raise ValueError('HOLDOUT_BEAUTY_LANDMARKS_REQUIRED')
    by_id = {face.object_id: face for face in landmarks}
    if len(by_id) != len(landmarks):
        raise ValueError('HOLDOUT_BEAUTY_FACE_BINDING')
    faces = []
    for observation in observations.faces:
        face = by_id.get(observation.face_id)
        if (face is None or face.canvas != source.image.canvas
                or face.transform_chain != source.image.transform_chain
                or [(point.x, point.y) for point in face.points] !=
                   [(point.x, point.y) for point in observation.landmarks]):
            raise ValueError('HOLDOUT_BEAUTY_FACE_BINDING')
        faces.append(FaceInput(face.model_copy(deep=True), skin_probability))
    params = effective_beauty_params(style.beauty, style.strength)
    with prepare_beauty(target, source.image, faces, params, artifacts.scratch,
                        permanent=source.protected, check_active=check_active) as prepared:
        check_active()
        if any(not face.applied and face.reason != 'zero_strength'
               for face in prepared.faces):
            raise ValueError('HOLDOUT_BEAUTY_NOT_MEASURABLE')
        shape = target.shape
        with read_array(prepared.image, shape, 'float32', check_active,
                        unit_interval=True) as pixels:
            for y in range(0, shape[0], 512):
                check_active()
                target[y:y + 512] = pixels[y:y + 512]
        check_active()


def recover_holdout_landmarks(rgb, image, observations, pool, backend,
                               model_lock_sha, check_active=lambda: None, *, alpha=None):
    """Recover confidence omitted by FaceObservations without fabricating it."""
    import numpy as np
    from photo_worker.algorithms.model_delta import work_to_model
    from photo_worker.operations.session.describe import _preview
    from photo_worker.operations.face.landmarks import _crop, _points
    check_active()
    if pool.lock.lock_id != model_lock_sha:
        raise ValueError('HOLDOUT_LANDMARK_MODEL_LOCK')
    if not observations.faces:
        return ()
    if 'fan2d' not in {entry.id for entry in pool.lock.models}:
        raise ValueError('HOLDOUT_LANDMARK_MODEL_REQUIRED')
    preview, _ = work_to_model(_preview(rgb, 2048))
    if alpha is not None:
        if (alpha.shape != rgb.shape[:2] or alpha.dtype not in
                (np.dtype('float32'), np.dtype('uint16'))):
            raise ValueError('HOLDOUT_LANDMARK_ALPHA_INVALID')
        opacity = _preview(alpha, 2048)
        preview = np.where(opacity[..., None] > 0, preview, 0).astype(np.float32)
    check_active()
    height, width = preview.shape[:2]
    result = []
    with pool.acquire('fan2d', backend) as engine:
        check_active()
        for observation in observations.faces:
            check_active()
            crop, geometry = _crop(preview, observation.box, 256, 1.25)
            raw = engine.infer(crop)
            check_active()
            points = _points(raw, width, height, geometry)
            if [(point.x, point.y) for point in points] != [
                    (point.x, point.y) for point in observation.landmarks]:
                raise ValueError('HOLDOUT_LANDMARKS_CHANGED')
            result.append(Face68(object_id=observation.face_id, points=points,
                canvas=image.canvas.model_copy(deep=True),
                transform_chain=image.transform_chain.model_copy(deep=True)))
    check_active()
    return tuple(result)
