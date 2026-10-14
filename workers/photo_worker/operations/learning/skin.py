"""Pinned Face19 parsing for all three V23 holdout beauty renders."""
from contextlib import contextmanager

import cv2
import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.operations.face.parse import MEAN, STD, crop_geometry, decode_logits


@contextmanager
def open_holdout_skin(rgb, observations, pool, backend, model_lock_sha, scratch,
                      check_active=lambda: None):
    check_active()
    if pool.lock.lock_id != model_lock_sha:
        raise ValueError('HOLDOUT_SKIN_MODEL_LOCK')
    if not observations.faces:
        yield None
        check_active()
        return
    if ('bisenet_face19' not in {entry.id for entry in pool.lock.models}
            or len(observations.faces) > 64):
        raise ValueError('HOLDOUT_SKIN_MODEL_REQUIRED')
    if (not isinstance(rgb, np.ndarray) or rgb.ndim != 3 or rgb.shape[-1] != 3
            or rgb.dtype != np.float32 or min(rgb.shape[:2]) < 1
            or rgb.shape[0] * rgb.shape[1] > 200_000_000
            or any(face.box.x1 <= face.box.x0 or face.box.y1 <= face.box.y0
                   for face in observations.faces)):
        raise ValueError('HOLDOUT_SKIN_INPUT_INVALID')
    height, width = rgb.shape[:2]
    # Match face.parse: color conversion precedes the affine crop; labels from
    # overlapping faces are selected by greatest class confidence, not union.
    srgb, _ = work_to_model(rgb)
    check_active()
    labels = np.zeros((height, width), np.uint16)
    confidence = np.zeros((height, width), np.float32)
    face_scores = []
    with pool.acquire('bisenet_face19', backend) as engine:
        check_active()
        for face in sorted(observations.faces, key=lambda face: face.face_id.hex):
            check_active()
            matrix, geometry = crop_geometry(face.box, width, height)
            crop = cv2.warpAffine(srgb, matrix, (512, 512),
                flags=cv2.INTER_LINEAR, borderMode=cv2.BORDER_REFLECT_101)
            raw = engine.infer((crop - MEAN) / STD)
            check_active()
            local, probability = decode_logits(raw)
            face_scores.append(float(np.mean(probability)))
            left, top, side = geometry
            inverse = np.array([[side / 512, 0, left],
                                [0, side / 512, top]], np.float32)
            restored = cv2.warpAffine(local, inverse, (width, height),
                flags=cv2.INTER_NEAREST, borderMode=cv2.BORDER_CONSTANT, borderValue=0)
            scores = cv2.warpAffine(probability, inverse, (width, height),
                flags=cv2.INTER_LINEAR, borderMode=cv2.BORDER_CONSTANT, borderValue=0)
            replace = (restored > 0) & (scores > confidence)
            labels[replace] = restored[replace]
            confidence[replace] = scores[replace]
            check_active()
            del raw, crop, local, probability, restored, scores, replace
    # mask.face_skin preserves FaceLabels19.mean_confidence. Production beauty
    # gates automatic masks below .75; do not replace that metadata with the
    # detector's unrelated face quality and accidentally upgrade weak parsing.
    if not face_scores or float(np.mean(face_scores)) < .75:
        raise ValueError('HOLDOUT_SKIN_NOT_MEASURABLE')
    del srgb, confidence
    with scratch.reserve(height * width * 4 + 8192) as reservation:
        check_active()
        path = reservation.path('holdout-skin', '.npy')
        skin = np.lib.format.open_memmap(path, mode='w+', dtype='<f4',
                                         shape=(height, width))
        try:
            for y in range(0, height, 512):
                check_active()
                skin[y:y + 512] = (labels[y:y + 512] == 1).astype(np.float32)
            del labels
            skin.flush()
            reservation.validate_file(path)
            skin.flags.writeable = False
            check_active()
            yield skin
            check_active()
        finally:
            skin._mmap.close()
