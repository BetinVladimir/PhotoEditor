import hashlib
import json
from pathlib import Path
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.donor_confirmation import DonorConfirmation, verify_open_eyes_confirmation
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams


def evidence():
    path = Path(__file__).resolve().parents[2] / 'backend/internal/v2portrait/testdata/open-eyes-params-parity.json'
    params = json.loads(path.read_text())['params']
    uid = lambda n: f'00000000-0000-0000-0000-{n:012d}'
    proof = {'schema_version': 1, 'person_id': uid(20), 'source': params['base'], 'donor': params['donor'],
             'source_face_id': params['source_face_id'], 'donor_face_id': params['donor_face_id'],
             'source_observation_id': uid(21), 'donor_observation_id': uid(22), 'consent_id': uid(23),
             'epoch': '1', 'actor_id': uid(24)}
    raw = canonical_bytes(proof)
    params['confirmation']['sha256'] = hashlib.sha256(raw).hexdigest()
    return PortraitOpenEyesParams.model_validate_json(json.dumps(params)), proof, raw


def test_confirmation_bytes_bind_both_versions_and_faces():
    params, proof, raw = evidence()
    assert verify_open_eyes_confirmation(params, raw).epoch == '1'
    with pytest.raises(ValueError, match='BYTES'):
        verify_open_eyes_confirmation(params, raw + b' ')
    proof['donor_face_id'] = proof['source_face_id']
    changed = canonical_bytes(proof)
    params.confirmation.sha256 = hashlib.sha256(changed).hexdigest()
    with pytest.raises(ValueError, match='BINDING'):
        verify_open_eyes_confirmation(params, changed)


@pytest.mark.parametrize('field', ['donor', 'donor_face_id', 'donor_observation_id'])
def test_partial_donor_proof_is_rejected(field):
    _, proof, _ = evidence();proof[field] = None
    with pytest.raises(ValidationError):
        DonorConfirmation.model_validate_json(json.dumps(proof))


def test_null_donor_for_generative_schema_does_not_authorize_open_eyes():
    params, proof, _ = evidence()
    for key in ['donor', 'donor_face_id', 'donor_observation_id']: proof[key] = None
    raw = canonical_bytes(proof)
    DonorConfirmation.model_validate_json(raw)
    params.confirmation.sha256 = hashlib.sha256(raw).hexdigest()
    with pytest.raises(ValueError, match='BINDING'):
        verify_open_eyes_confirmation(params, raw)
