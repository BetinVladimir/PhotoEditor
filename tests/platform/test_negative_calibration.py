from hashlib import sha256
import json
from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import NegativeCalibration
from photo_worker.operations.optical.negative_calibration import verify_negative_calibration
from test_optical_negative import calibration
from test_optical_negative_params import intent


def test_original_calibration_hash_schema_and_owned_bytes():
    p=intent();raw=bytearray(canonical_bytes(calibration()));p['calibration']['sha256']=sha256(raw).hexdigest()
    got=verify_negative_calibration(p,raw,input_icc_sha='c'*64)
    raw[0]=0
    assert got.input_icc_sha=='c'*64
    expected=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/schemas.json').read_text())['$defs']['NegativeCalibration']
    assert set(NegativeCalibration.model_fields)==set(expected['required'])==set(expected['properties'])


@pytest.mark.parametrize('mode',['hash','whitespace','duplicate','icc','oversize','matrix','range','extra'])
def test_calibration_tampering_and_invalid_density_refused(mode):
    p=intent();c=calibration();icc='c'*64
    if mode=='matrix':c['density_matrix'][0]=[1,0]
    if mode=='range':c['density_range'][0]=0
    if mode=='extra':c['unknown']=True
    raw=canonical_bytes(c)
    if mode=='whitespace':raw+=b' '
    if mode=='duplicate':raw=raw[:-1]+b',"schema_version":1}'
    if mode=='icc':icc='e'*64
    if mode=='oversize':raw=b' '*65537
    p['calibration']['sha256']=sha256(raw).hexdigest() if mode!='hash' else '0'*64
    with pytest.raises(ValueError):verify_negative_calibration(p,raw,input_icc_sha=icc)


def test_pinned_kernel_uses_verified_calibration_before_pixel_access():
    import numpy as np
    from photo_worker.operations.optical.negative import invert_pinned_negative_tile
    p=intent();raw=canonical_bytes(calibration());p['calibration']['sha256']=sha256(raw).hexdigest()
    out=invert_pinned_negative_tile(np.asarray(p['film_base_rgb'],np.float32).reshape(1,1,3),p,raw,
        input_icc_sha='c'*64,epsilon=1e-6)
    np.testing.assert_allclose(out,0,atol=1e-7)
    with pytest.raises(ValueError,match='HASH_MISMATCH'):
        invert_pinned_negative_tile(None,p,raw+b' ',input_icc_sha='c'*64,epsilon=1e-6)
