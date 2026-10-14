"""Donorless V28 glare requires a source-only confirmed face."""

import hashlib
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait.glare_generative_evidence import verify_generative_glare_face
from photo_worker.operations.session.contracts import FaceObservations


def ref(sha='a' * 64):
    return {'artifact_id': uuid4(), 'sha256': sha}


def fixture():
    binding = {'asset_version_id': uuid4(), 'source': ref(),
               'canvas_hash': 'b' * 64}
    face_id, observation_id = uuid4(), uuid4()
    faces = FaceObservations(schema_version=1, binding=binding,
        faces=[{'face_id': face_id, 'observation_id': observation_id,
                'box': {'x0': .25, 'y0': .25, 'x1': .75, 'y1': .75},
                'landmarks': [{'x': .5, 'y': .5}] * 68,
                'embedding': None, 'quality': 1, 'person_id': None}])
    faces_raw = canonical_bytes(faces)
    proof = DonorConfirmation(schema_version=1, person_id=uuid4(),
        source=binding, donor=None, source_face_id=face_id,
        donor_face_id=None, source_observation_id=observation_id,
        donor_observation_id=None, consent_id=uuid4(), epoch='1',
        actor_id=uuid4())
    proof_raw = canonical_bytes(proof)
    params = PortraitGlareParams(schema_version=1, base=binding, strength=.5,
        donor=None, glasses_mask=ref(), protected=ref(), mode='generative',
        confirmation=ref(hashlib.sha256(proof_raw).hexdigest()),
        model_lock_sha='c' * 64, calibration=ref(),
        source_faces=ref(hashlib.sha256(faces_raw).hexdigest()),
        donor_faces=None)
    return params, proof_raw, faces_raw


def test_generative_glare_face_requires_source_only_confirmation():
    params, proof, faces = fixture()
    result = verify_generative_glare_face(params, proof, faces, (128, 128), 32)
    assert result.diagonal_px > 90
    with pytest.raises(ValueError, match='GLARE_GENERATIVE_FACE_TOO_SMALL'):
        verify_generative_glare_face(params, proof, faces, (64, 64), 40)
