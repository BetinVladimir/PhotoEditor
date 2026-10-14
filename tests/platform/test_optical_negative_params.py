import json
from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams


def intent():
    return dict(schema_version=1,base=dict(asset_version_id=str(uuid4()),canvas_hash='c'*64,
        source=dict(artifact_id=str(uuid4()),sha256='a'*64)),strength=1,
        film_base_rgb=[0.8,0.5,0.2],calibration=dict(artifact_id=str(uuid4()),sha256='b'*64),exposure_ev=0)


def test_negative_params_matches_required_normative_fields():
    schemas=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/schemas.json').read_text())
    expected=schemas['$defs']['OpticalNegativeParams']
    assert set(OpticalNegativeParams.model_fields)==set(expected['required'])==set(expected['properties'])
    raw=canonical_bytes(intent())
    params=OpticalNegativeParams.model_validate_json(raw)
    assert canonical_bytes(params)==raw


@pytest.mark.parametrize('channels',[[0,0.5,1],[1e-7,0.5,1],[1.01,0.5,1],[0.5,0.5],[0.5]*4,[True,0.5,1]])
def test_negative_requires_three_measured_positive_film_base_channels(channels):
    data=intent();data['film_base_rgb']=channels
    with pytest.raises(ValueError):OpticalNegativeParams.model_validate_json(canonical_bytes(data))


@pytest.mark.parametrize('field,value',[('strength',-0.1),('strength',1.1),('exposure_ev',-3.01),
    ('exposure_ev',3.01),('exposure_ev',True),('unknown',1)])
def test_negative_rejects_invalid_edit_intent(field,value):
    data=intent();data[field]=value
    with pytest.raises(ValueError):OpticalNegativeParams.model_validate_json(canonical_bytes(data))


@pytest.mark.parametrize('exposure',[-3,3])
def test_negative_boundary_exposure_and_strength_zero_preserved(exposure):
    data=intent();data.update(exposure_ev=exposure,strength=0,film_base_rgb=[1e-6,1,0.5])
    parsed=OpticalNegativeParams.model_validate_json(canonical_bytes(data))
    assert parsed.exposure_ev==exposure and parsed.strength==0



def test_backend_negative_params_original_fixture_matches_worker_contract():
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-params.json'
    raw=path.read_bytes()
    assert canonical_bytes(OpticalNegativeParams.model_validate_json(raw))==raw



def test_backend_negative_calibration_fixture_original_bytes():
    from hashlib import sha256
    from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
    root=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata'
    raw=(root/'negative-calibration.json').read_bytes()
    params=json.loads((root/'negative-params.json').read_bytes())
    params['calibration']['sha256']=sha256(raw).hexdigest()
    calibration=verify_negative_calibration(params,raw,input_icc_sha='c'*64)
    assert canonical_bytes(calibration)==raw
