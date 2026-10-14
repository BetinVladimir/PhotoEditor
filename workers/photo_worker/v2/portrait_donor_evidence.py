"""V28 donor observation geometry; current DB consent/epoch checks are separate."""
from dataclasses import dataclass
import math
import numpy as np
from photo_worker.contracts.donor_confirmation import verify_open_eyes_confirmation
from photo_worker.operations.session.contracts import FaceObservations
from photo_worker.v2.observation_evidence import resolve_face_id


@dataclass(frozen=True)
class DonorFaceGeometry:
    source_landmarks: np.ndarray
    donor_landmarks: np.ndarray
    source_diagonal: float
    donor_diagonal: float


def verify_open_eyes_faces(params, confirmation_raw, source_faces_raw, donor_faces_raw,
                          source_size, donor_size, min_face_edge_px: float) -> DonorFaceGeometry:
    confirmation = verify_open_eyes_confirmation(params, confirmation_raw)
    if not math.isfinite(min_face_edge_px) or min_face_edge_px <= 0:
        raise ValueError('DONOR_CALIBRATION')
    geometries = []
    for raw, ref, binding, face_id, observation_id, size in (
        (source_faces_raw, params.source_faces, params.base, params.source_face_id,
         confirmation.source_observation_id, source_size),
        (donor_faces_raw, params.donor_faces, params.donor, params.donor_face_id,
         confirmation.donor_observation_id, donor_size),
    ):
        if (len(raw) > 1_048_576 or len(size) != 2 or
                any(type(n) is not int or n < 1 for n in size) or size[0] * size[1] > 200_000_000):
            raise ValueError('DONOR_GEOMETRY')
        resolved = resolve_face_id(raw, expected_sha256=ref.sha256,
                                   asset_version_id=binding.asset_version_id,
                                   source_artifact_id=binding.source.artifact_id,
                                   source_sha256=binding.source.sha256,
                                   canvas_hash=binding.canvas_hash, observation_id=observation_id)
        if resolved != face_id:
            raise ValueError('DONOR_FACE_BINDING')
        document = FaceObservations.model_validate_json(raw)
        face = next(f for f in document.faces if f.observation_id == observation_id)
        width, height = size
        box = face.box
        edges = ((box.x1 - box.x0) * width, (box.y1 - box.y0) * height)
        if min(edges) < min_face_edge_px:
            raise ValueError('DONOR_FACE_SIZE')
        points = np.array([(p.x * width, p.y * height) for p in face.landmarks], dtype=np.float64)
        if any(not (box.x0 <= p.x <= box.x1 and box.y0 <= p.y <= box.y1) for p in face.landmarks):
            raise ValueError('DONOR_LANDMARK_ROI')
        points.setflags(write=False)
        geometries.append((points, math.hypot(*edges)))
    return DonorFaceGeometry(geometries[0][0], geometries[1][0], geometries[0][1], geometries[1][1])
