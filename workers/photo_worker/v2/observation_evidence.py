"""Validate immutable V22 face evidence before a trusted confirmation transaction.

The caller must obtain the bytes through an authorized artifact download and
compare the returned face_id inside the same transaction that checks consent.
"""
from __future__ import annotations

import hashlib
from uuid import UUID

from photo_worker.contracts.base import parse_json
from photo_worker.operations.session.contracts import FaceObservations


def resolve_face_id(
    raw: bytes,
    *,
    expected_sha256: str,
    asset_version_id: UUID,
    source_artifact_id: UUID,
    source_sha256: str,
    canvas_hash: str,
    observation_id: UUID,
) -> UUID:
    """Return a face ID only for exact, unambiguous source-bound evidence.

    This is semantic validation, not authorization: the DB must recheck the
    tenant/project, ready artifact, active grant/epoch, person CAS and actor.
    """
    if hashlib.sha256(raw).hexdigest() != expected_sha256:
        raise ValueError("face evidence digest mismatch")
    parse_json(raw)  # Reject duplicate keys and non-finite/unsafe JSON numbers.
    evidence = FaceObservations.model_validate_json(raw)
    binding = evidence.binding
    if (binding.asset_version_id != asset_version_id
            or binding.source.artifact_id != source_artifact_id
            or binding.source.sha256 != source_sha256
            or binding.canvas_hash != canvas_hash):
        raise ValueError("face evidence source binding mismatch")
    seen_observations: set[UUID] = set()
    seen_faces: set[UUID] = set()
    selected: UUID | None = None
    for face in evidence.faces:
        if face.observation_id in seen_observations or face.face_id in seen_faces:
            raise ValueError("duplicate face identity in evidence")
        seen_observations.add(face.observation_id)
        seen_faces.add(face.face_id)
        if face.person_id is not None:
            raise ValueError("worker evidence cannot assert a confirmed person")
        if face.observation_id == observation_id:
            selected = face.face_id
    if selected is None:
        raise ValueError("face observation not found in evidence")
    return selected
