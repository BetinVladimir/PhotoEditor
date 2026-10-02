import hashlib
import json
from uuid import uuid4

import pytest

from photo_worker.v2.observation_evidence import resolve_face_id


def test_resolve_face_evidence_checks_binding_digest_and_uniqueness():
    version, source, observation, face = (uuid4() for _ in range(4))
    payload = {
        "schema_version": 1,
        "binding": {"asset_version_id": str(version),
                    "source": {"artifact_id": str(source), "sha256": "a" * 64},
                    "canvas_hash": "b" * 64},
        "faces": [{"observation_id": str(observation), "face_id": str(face),
                   "box": {"x0": 0, "y0": 0, "x1": 1, "y1": 1},
                   "landmarks": [{"x": 0.5, "y": 0.5}] * 68,
                   "embedding": None, "quality": 0.9, "person_id": None}],
    }

    def verify(value, **changes):
        raw = json.dumps(value).encode()
        args = dict(expected_sha256=hashlib.sha256(raw).hexdigest(),
                    asset_version_id=version, source_artifact_id=source,
                    source_sha256="a" * 64, canvas_hash="b" * 64,
                    observation_id=observation)
        args.update(changes)
        return resolve_face_id(raw, **args)

    assert verify(payload) == face
    with pytest.raises(ValueError, match="digest"):
        verify(payload, expected_sha256="0" * 64)
    with pytest.raises(ValueError, match="binding"):
        verify(payload, asset_version_id=uuid4())
    with pytest.raises(ValueError, match="not found"):
        verify(payload, observation_id=uuid4())
    payload["faces"].append(dict(payload["faces"][0], observation_id=str(uuid4())))
    with pytest.raises(ValueError, match="duplicate"):
        verify(payload)
    payload["faces"].pop()
    payload["faces"][0]["person_id"] = str(uuid4())
    with pytest.raises(ValueError, match="cannot assert"):
        verify(payload)
