from copy import deepcopy
from uuid import uuid4
import pytest
from test_relight_inputs import inputs
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.relight_qualification import verify_relight_qualification,relight_runtime_authority


def evidence():
    params,_,_,_,_,calibration,_=inputs()
    policy=dict(schema_version=1,policy_id=str(uuid4()),operation='image.relight',operation_version='1',
        backend='cpu',model_lock_sha=calibration.model_lock_sha,runtime_lock_sha='b'*64,
        device_profile_sha='c'*64,dataset_sha=calibration.dataset_sha,issuer_id=str(uuid4()),
        required_checks=['depth_edges','protected'],test_fixture_shas=['e'*64],
        calibration_shas=[canonical_hash(calibration)],evidence_shas=['f'*64],license_cleared=True)
    report=dict(schema_version=1,operation='image.relight',version='1',model_lock_sha=calibration.model_lock_sha,
        runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha=calibration.dataset_sha,
        issuer_id=policy['issuer_id'],input_fixture_shas=['e'*64],
        checks=[dict(id=name,passed=True,evidence_sha='f'*64) for name in policy['required_checks']],
        metrics=dict(p50_ms=1,p95_ms=2,peak_vram_bytes=0,peak_rss_bytes=1024,scratch_bytes=1024),
        status='passed',created_at='2026-10-09T00:00:00Z')
    pin=dict(policy_id=policy['policy_id'],qualification_id=str(uuid4()),policy_sha=canonical_hash(policy),
        report=dict(artifact_id=str(uuid4()),sha256=canonical_hash(report)),
        calibration=params.calibration.model_dump(mode='json'),model_lock_sha=calibration.model_lock_sha,
        runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha=calibration.dataset_sha,
        issuer_id=policy['issuer_id'],backend='cpu')
    return params,pin,report,calibration,policy


def test_exact_relight_evidence_and_live_revalidation():
    params,pin,report,calibration,policy=evidence()
    frozen,actual=verify_relight_qualification(pin,canonical_bytes(report),canonical_bytes(calibration),policy,
        params=params,expected_runtime_sha='b'*64,check_active=lambda:None)
    assert actual==calibration
    current=deepcopy(pin);calls=[]
    def load():calls.append(True);return deepcopy(current)
    authority=relight_runtime_authority(load,frozen_pin=frozen,check_active=lambda:None)
    assert authority()==frozen and authority()==frozen and len(calls)==2
    current['qualification_id']=str(uuid4())
    with pytest.raises(ValueError):authority()


@pytest.mark.parametrize('mode',['operation','backend','runtime','locator','report_bytes','calibration_bytes',
    'missing_check','failed_check','unlicensed','model','dataset','unqualified','alias','schema_bool'])
def test_relight_evidence_refuses_substitution(mode):
    params,pin,report,calibration,policy=evidence();runtime='b'*64
    if mode=='operation':report['operation']='portrait.open_eyes'
    if mode=='backend':pin['backend']='cuda'
    if mode=='runtime':runtime='0'*64
    if mode=='locator':params=params.model_copy(update={'calibration':params.calibration.model_copy(update={'artifact_id':uuid4()})})
    if mode=='missing_check':report['checks']=report['checks'][:1]
    if mode=='failed_check':report['checks'][0]['passed']=False
    if mode=='unlicensed':policy['license_cleared']=False;pin['policy_sha']=canonical_hash(policy)
    if mode=='alias':pin['report']['artifact_id']=pin['calibration']['artifact_id']
    if mode=='schema_bool':report['schema_version']=True
    if mode in ('model','dataset','unqualified'):
        calibration=calibration.model_copy(update={ {'model':'model_lock_sha','dataset':'dataset_sha','unqualified':'qualified'}[mode]:False if mode=='unqualified' else '0'*64})
        digest=canonical_hash(calibration);pin['calibration']['sha256']=digest
        params=params.model_copy(update={'calibration':params.calibration.model_copy(update={'sha256':digest})})
        policy['calibration_shas']=[digest];pin['policy_sha']=canonical_hash(policy)
    pin['report']['sha256']=canonical_hash(report)
    rraw=canonical_bytes(report);craw=canonical_bytes(calibration)
    if mode=='report_bytes':rraw=b' '+rraw
    if mode=='calibration_bytes':craw=b' '+craw
    with pytest.raises(ValueError):
        verify_relight_qualification(pin,rraw,craw,policy,params=params,expected_runtime_sha=runtime,
                                    check_active=lambda:None)
