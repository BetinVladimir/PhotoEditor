"""Model evidence comes only from approved, backend-qualified pinned entries."""

import numpy as np

from .prepare import capture_model_preview
from .assess import FaceModelEvidence


def infer_face_evidence(ctx, work, faces, calibration, pool, backend, policy, alpha=None):
    entries = {entry.id: entry for entry in pool.lock.models}
    def available(model_id):
        entry = entries.get(model_id)
        return (entry is not None and entry.approved_for_install and backend in entry.backends
                and entry.backends[backend].status == 'passed')
    eye_ready = 'eyes' in policy.checks and available('eye_open')
    glare_ready = 'glare' in policy.checks and available('glare_mask')
    if not eye_ready and not glare_ready:
        return {}
    srgb = capture_model_preview(work, alpha)
    height, width = srgb.shape[:2]
    evidence = {}
    for face in faces.faces:
        ctx.check_active()
        box = face.box
        x0, y0, x1, y1 = int(box.x0*width), int(box.y0*height), int(box.x1*width), int(box.y1*height)
        if min(x1-x0, y1-y0) < calibration.min_face_edge_px or face.quality <= 0:
            continue
        crop = np.ascontiguousarray(srgb[y0:y1, x0:x1], dtype=np.float32)
        eyes, glare = None, None
        eye_spans = [(max(point.x for point in face.landmarks[start:start+6])
                     - min(point.x for point in face.landmarks[start:start+6])) * width
                     for start in (36, 42)]
        visible = all(box.x0 <= point.x <= box.x1 and box.y0 <= point.y <= box.y1
                      for point in face.landmarks[36:48])
        if eye_ready and visible and min(eye_spans) >= 8:
            with pool.acquire('eye_open', backend) as engine:
                values = engine.infer(crop)
            eyes = tuple(float(value) for value in values)
        if glare_ready:
            ctx.check_active()
            with pool.acquire('glare_mask', backend) as engine:
                mask = engine.infer(crop)
            if mask.shape != crop.shape[:2]:
                raise ValueError('CAPTURE_GLARE_SHAPE_INVALID')
            glare = float(np.mean(mask > .5))
        evidence[face.face_id] = FaceModelEvidence(True, eyes, glare)
    return evidence
