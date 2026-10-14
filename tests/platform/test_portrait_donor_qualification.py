from copy import deepcopy
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.v2.portrait_donor_qualification import verify_donor_qualification,donor_runtime_authority


def fixture():
    calibration=dict(schema_version=1,model_lock_sha='a'*64,dataset_sha='d'*64,qualified=True,
        scene_cosine_min=.3,scene_log_luma_max=1,cluster_diameter_max=1,sharpness_scale=1,
        clipping_penalty=1,match_inlier_min=.3,match_error_px_max=5,identity_similarity_min=.4,
        landmark_drift_max=.2,normalization_scales=[1]*6,loss_weights=[.4,.1,.1,.1,.2,.1],
        focus_warning=.5,focus_critical=.2,eye_open_min=.5,glare_warning_fraction=.1,
        clipping_warning_fraction=.1,clipping_critical_fraction=.3,min_face_edge_px=64,
        depth_sigma=.1,registration_error_px=2,motion_threshold=.1)
    craw=canonical_bytes(calibration)
    policy=dict(schema_version=1,policy_id=str(uuid4()),operation='portrait.open_eyes',operation_version='1',backend='cpu',
        model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,
        issuer_id=str(uuid4()),required_checks=['identity'],test_fixture_shas=['e'*64],calibration_shas=[sha256(craw).hexdigest()],evidence_shas=['f'*64],license_cleared=True)
    report=dict(schema_version=1,operation='portrait.open_eyes',version='1',model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,
        issuer_id=policy['issuer_id'],input_fixture_shas=['e'*64],checks=[dict(id='identity',passed=True,evidence_sha='f'*64)],metrics=dict(p50_ms=1,p95_ms=2,peak_vram_bytes=0,peak_rss_bytes=1024,scratch_bytes=1024),status='passed',created_at='2026-10-07T00:00:00Z')
    rraw=canonical_bytes(report)
    pin=dict(policy_id=policy['policy_id'],qualification_id=str(uuid4()),policy_sha=canonical_hash(policy),report=dict(artifact_id=str(uuid4()),sha256=sha256(rraw).hexdigest()),
        calibration=dict(artifact_id=str(uuid4()),sha256=sha256(craw).hexdigest()),model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64,issuer_id=policy['issuer_id'],backend='cpu')
    profile=dict(backend='cpu',renderer_version='v2.2',portrait_runtime_sha='b'*64,resource_estimate={},donor_qualification=pin)
    return profile,rraw,craw,policy


def test_donor_exact_qualification_and_current_authority():
    profile,rraw,craw,policy=fixture()
    original=deepcopy(profile)
    pin,calibration=verify_donor_qualification(profile,rraw,craw,policy,check_active=lambda:None)
    assert calibration.min_face_edge_px==64 and profile==original
    calls=[]
    def load():calls.append(True);return deepcopy(profile['donor_qualification'])
    authority=donor_runtime_authority(load,frozen_pin=pin,check_active=lambda:None)
    assert authority()==pin and authority()==pin and len(calls)==2
    profile['donor_qualification']['qualification_id']=str(uuid4())
    with pytest.raises(ValueError):authority()


@pytest.mark.parametrize('mode',['report','calibration','runtime','backend','extra','pin_extra','alias','policy','schema_bool','unqualified','size'])
def test_donor_qualification_rejects_changed_evidence(mode):
    profile,rraw,craw,policy=fixture()
    if mode=='report':rraw=b' '+rraw
    if mode=='calibration':craw=b' '+craw
    if mode=='runtime':profile['portrait_runtime_sha']='0'*64
    if mode=='backend':profile['backend']='cuda'
    if mode=='extra':profile['extra']=1
    if mode=='pin_extra':profile['donor_qualification']['extra']=1
    if mode=='alias':profile['donor_qualification']['report']['artifact_id']=profile['donor_qualification']['calibration']['artifact_id']
    if mode=='policy':policy['required_checks']=['missing']
    if mode=='size':craw=b' '*(1+(1<<20))
    if mode in ('schema_bool','unqualified'):
        import json
        value=json.loads(craw)
        value['schema_version' if mode=='schema_bool' else 'qualified']=True if mode=='schema_bool' else False
        craw=canonical_bytes(value)
        changed_sha=sha256(craw).hexdigest()
        profile['donor_qualification']['calibration']['sha256']=changed_sha
        policy['calibration_shas']=[changed_sha]
        profile['donor_qualification']['policy_sha']=canonical_hash(policy)
    with pytest.raises(ValueError):verify_donor_qualification(profile,rraw,craw,policy,check_active=lambda:None)


def test_qualified_manual_entry_derives_threshold_and_repeats_authority(monkeypatch):
    from photo_worker.v2 import portrait_open_eyes_manual_stage as stage
    profile,rraw,craw,policy=fixture()
    current=deepcopy(profile['donor_qualification'])
    calls=[]
    def execute(*args,**kwargs):
        calls.append(True)
        assert kwargs['min_face_edge_px']==64
        kwargs['check_active']()
        current['qualification_id']=str(uuid4())
        with pytest.raises(ValueError):kwargs['check_active']()
        return 'staged'
    monkeypatch.setattr(stage,'prepare_and_stage_manual_open_eyes',execute)
    assert stage.prepare_and_stage_qualified_manual_open_eyes(profile=profile,report_raw=rraw,calibration_raw=craw,
        qualification_policy=policy,load_qualification_pin=lambda:deepcopy(current),check_active=lambda:None,expected_runtime_sha='b'*64)=='staged'
    assert calls==[True]
    with pytest.raises(ValueError):
        stage.prepare_and_stage_qualified_manual_open_eyes(profile=profile,report_raw=rraw,calibration_raw=craw,
            qualification_policy=policy,load_qualification_pin=lambda:deepcopy(current),check_active=lambda:None,
            expected_runtime_sha='b'*64,min_face_edge_px=1)
    assert calls==[True]
