"""V28 donorless glare source-person proof; live consent remains SQL-owned."""

from dataclasses import dataclass
import hashlib
import math

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.session.contracts import FaceObservations
from photo_worker.v2.observation_evidence import resolve_face_id


@dataclass(frozen=True)
class GlareSourceFace:
    face_id: object
    observation_id: object
    diagonal_px: float


def verify_generative_glare_face(params, confirmation_raw, source_faces_raw,
                                  source_size, min_face_edge_px) -> GlareSourceFace:
    """Bind source-only confirmation and a sufficiently resolved face ROI."""
    if (not isinstance(params, PortraitGlareParams)
            or params.mode != 'generative' or params.donor is not None
            or params.donor_faces is not None
            or not isinstance(source_size, tuple) or len(source_size) != 2
            or any(type(n) is not int or n < 1 for n in source_size)
            or source_size[0] * source_size[1] > 200_000_000
            or type(min_face_edge_px) is not int
            or not 32 <= min_face_edge_px <= 512):
        raise ValueError('GLARE_GENERATIVE_EVIDENCE_INVALID')
    raw = bytes(confirmation_raw)
    faces_raw = bytes(source_faces_raw)
    parse_json(raw, max_bytes=65_536)
    proof = DonorConfirmation.model_validate_json(raw)
    if (canonical_bytes(proof) != raw
            or hashlib.sha256(raw).hexdigest() != params.confirmation.sha256
            or proof.source != params.base or proof.donor is not None
            or proof.donor_face_id is not None
            or proof.donor_observation_id is not None):
        raise ValueError('GLARE_GENERATIVE_CONFIRMATION_INVALID')
    resolved = resolve_face_id(faces_raw,
        expected_sha256=params.source_faces.sha256,
        asset_version_id=params.base.asset_version_id,
        source_artifact_id=params.base.source.artifact_id,
        source_sha256=params.base.source.sha256,
        canvas_hash=params.base.canvas_hash,
        observation_id=proof.source_observation_id)
    if resolved != proof.source_face_id:
        raise ValueError('GLARE_GENERATIVE_FACE_MISMATCH')
    observations = FaceObservations.model_validate_json(faces_raw)
    face = next(f for f in observations.faces
                if f.observation_id == proof.source_observation_id)
    width, height = source_size
    box = face.box
    edges = ((box.x1 - box.x0) * width, (box.y1 - box.y0) * height)
    if min(edges) < min_face_edge_px:
        raise ValueError('GLARE_GENERATIVE_FACE_TOO_SMALL')
    if any(not (box.x0 <= point.x <= box.x1
                and box.y0 <= point.y <= box.y1) for point in face.landmarks):
        raise ValueError('GLARE_GENERATIVE_LANDMARK_ROI')
    diagonal = math.hypot(*edges)
    if not math.isfinite(diagonal) or diagonal <= 0:
        raise ValueError('GLARE_GENERATIVE_FACE_INVALID')
    return GlareSourceFace(resolved, proof.source_observation_id, diagonal)
