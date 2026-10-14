"""V28 glare donor path binds proof and current authority before pixel work."""

import hashlib
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.portrait import glare_donor


def ref():
    return {'artifact_id': uuid4(), 'sha256': 'a' * 64}


def fixture():
    base = {'asset_version_id': uuid4(), 'source': ref(), 'canvas_hash': 'b' * 64}
    donor = {'asset_version_id': uuid4(), 'source': ref(), 'canvas_hash': 'c' * 64}
    proof = DonorConfirmation(schema_version=1, person_id=uuid4(),
        source=base, donor=donor, source_face_id=uuid4(), donor_face_id=uuid4(),
        source_observation_id=uuid4(), donor_observation_id=uuid4(),
        consent_id=uuid4(), epoch='1', actor_id=uuid4())
    raw = canonical_bytes(proof)
    confirmation = {'artifact_id': uuid4(),
                    'sha256': hashlib.sha256(raw).hexdigest()}
    params = PortraitGlareParams(schema_version=1, base=base, strength=.5,
        donor=donor, glasses_mask=ref(), protected=ref(), mode='donor',
        confirmation=confirmation, model_lock_sha='d' * 64,
        calibration=ref(), source_faces=ref(), donor_faces=ref())
    return params, raw


def invoke(params, raw, authority):
    source = np.zeros((8, 8, 3), np.float32)
    mask = np.ones((8, 8), np.float32)
    return glare_donor.prepare_glare_donor_patch(
        params, raw, b'faces', b'faces', source, source, mask,
        np.zeros_like(mask), (8, 8), (8, 8), (0, 0), 32,
        lambda: None, authority)


def test_glare_donor_rejects_missing_live_authority_before_pixels(monkeypatch):
    params, raw = fixture()
    monkeypatch.setattr(glare_donor, 'prepare_donor_patch',
                        lambda *a, **kw: pytest.fail('pixel work must not run'))
    with pytest.raises(ValueError, match='GLARE_DONOR_AUTHORITY_UNAVAILABLE'):
        invoke(params, raw, lambda *_: False)


def test_glare_donor_binds_proof_and_uses_existing_patch_math(monkeypatch):
    params, raw = fixture()
    geometry = SimpleNamespace(source_landmarks=np.zeros((68, 2)),
        donor_landmarks=np.zeros((68, 2)), source_diagonal=80.)
    monkeypatch.setattr(glare_donor, 'verify_open_eyes_faces',
                        lambda *a, **kw: geometry)
    marker = object()
    monkeypatch.setattr(glare_donor, 'prepare_donor_patch',
                        lambda *a, **kw: marker)
    assert invoke(params, raw, lambda *_: True) is marker
    with pytest.raises(ValueError, match='GLARE_DONOR_CONFIRMATION_INVALID'):
        invoke(params, raw.replace(b'"epoch":"1"', b'"epoch":"2"'),
               lambda *_: True)
