import copy
import hashlib
import json
from pathlib import Path
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams

FIXTURE = Path(__file__).resolve().parents[2] / 'backend/internal/v2portrait/testdata/open-eyes-params-parity.json'


def test_backend_worker_open_eyes_fingerprint():
    fixture = json.loads(FIXTURE.read_text())
    params = PortraitOpenEyesParams.model_validate_json(json.dumps(fixture['params']))
    assert hashlib.sha256(canonical_bytes(params)).hexdigest() == fixture['sha256']


@pytest.mark.parametrize('field', ['donor', 'confirmation', 'source_faces', 'source_face_id'])
def test_missing_required_donor_evidence(field):
    raw = json.loads(FIXTURE.read_text())['params']
    del raw[field]
    with pytest.raises(ValidationError):
        PortraitOpenEyesParams.model_validate_json(json.dumps(raw))


@pytest.mark.parametrize('case', ['same_donor', 'same_mask', 'nil_face', 'nil_confirmation', 'unknown', 'bad_strength'])
def test_invalid_open_eyes_intent(case):
    raw = copy.deepcopy(json.loads(FIXTURE.read_text())['params'])
    if case == 'same_donor': raw['donor'] = raw['base']
    elif case == 'same_mask': raw['protected'] = raw['eyes_mask']
    elif case == 'nil_face': raw['donor_face_id'] = '00000000-0000-0000-0000-000000000000'
    elif case == 'nil_confirmation': raw['confirmation']['artifact_id'] = '00000000-0000-0000-0000-000000000000'
    elif case == 'unknown': raw['synthetic'] = True
    else: raw['strength'] = 1.01
    with pytest.raises(ValidationError):
        PortraitOpenEyesParams.model_validate_json(json.dumps(raw))
