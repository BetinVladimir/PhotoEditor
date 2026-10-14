import json
from hashlib import sha256
from copy import deepcopy
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.negative_qualification import verify_negative_qualification
from test_portrait_donor_qualification import fixture as donor_fixture
from test_optical_negative import calibration
from test_optical_negative_params import intent


def fixture():
    profile,report,_,policy=donor_fixture();pin=profile['donor_qualification']
    policy['operation']='optical.negative'
    report=json.loads(report);report['operation']='optical.negative';report=canonical_bytes(report)
    pin['report']['sha256']=sha256(report).hexdigest()
    c=calibration();c['qualification']=deepcopy(pin['report']);raw=canonical_bytes(c)
    pin['calibration']['sha256']=sha256(raw).hexdigest();policy['calibration_shas']=[pin['calibration']['sha256']]
    pin['policy_sha']=canonical_hash(policy);p=intent();p['calibration']=deepcopy(pin['calibration'])
    return p,raw,report,pin,policy


@pytest.mark.parametrize('mode',['ok','runtime','report','policy','calibration_binding'])
def test_negative_original_qualification_report_policy_and_binding(mode):
    p,c,r,pin,policy=fixture()
    if mode=='runtime':pin['runtime_lock_sha']='0'*64
    if mode=='report':r+=b' '
    if mode=='policy':policy['license_cleared']=False
    if mode=='calibration_binding':p['calibration']['artifact_id']=p['base']['source']['artifact_id']
    def run():return verify_negative_qualification(p,c,r,pin,policy,input_icc_sha='c'*64,runtime_sha='b'*64,check_active=lambda:None)
    if mode=='ok':assert run()[0].backend=='cpu'
    else:
        with pytest.raises(ValueError):run()



def test_backend_negative_qualification_fixture_matches_worker():
    from pathlib import Path
    path=Path(__file__).resolve().parents[2]/'backend/internal/v2optical/testdata/negative-qualification.json'
    f=json.loads(path.read_text());policy=deepcopy(f['locks'])
    policy['operation_version']=policy.pop('version');policy['schema_version']=1;policy['policy_id']=f['pin']['policy_id']
    verify_negative_qualification(f['params'],f['calibration'].encode(),f['report'].encode(),f['pin'],policy,
        input_icc_sha='c'*64,runtime_sha=f['pin']['runtime_lock_sha'],check_active=lambda:None)
