from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import portrait_open_eyes_manual_stage as stage
from test_portrait_donor_qualification import fixture
from test_portrait_open_eyes_prepare import arguments


def inputs():
    profile,report,calibration,policy=fixture()
    params=arguments()[0]
    raw=canonical_bytes(params)
    lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',params=SimpleNamespace(sha256=sha256(raw).hexdigest())),execution_profile=deepcopy(profile))
    return lease,raw,report,calibration,policy


def test_leased_manual_path_owns_inputs_and_rechecks_attempt(monkeypatch):
    lease,raw,report,calibration,policy=inputs()
    current=deepcopy(lease.execution_profile['donor_qualification'])
    calls=[];ready=[True]
    def live(owned,body):
        calls.append('lease')
        assert body==raw and owned.execution_profile['renderer_version']=='v2.2'
        owned.execution_profile.clear()
        return ready[0]
    def execute(params,*args,**kwargs):
        calls.append('execute')
        assert canonical_bytes(params)==raw and kwargs['min_face_edge_px']==64
        lease.execution_profile.clear();lease.command.params.sha256='0'*64
        kwargs['check_active']()
        ready[0]=False
        with pytest.raises(ValueError):kwargs['check_active']()
        return 'result'
    monkeypatch.setattr(stage,'prepare_and_stage_manual_open_eyes',execute)
    with pytest.raises(ValueError):
        stage.prepare_and_stage_leased_manual_open_eyes(SimpleNamespace(donor_portrait_lease_ready=live,donor_source_ready=lambda *args:True),lease,raw,
            report_raw=report,calibration_raw=calibration,qualification_policy=policy,
            load_qualification_pin=lambda:deepcopy(current),check_active=lambda:None,expected_runtime_sha='b'*64)
    assert calls==['lease','execute','lease','lease','lease']


@pytest.mark.parametrize("sealed",[False,True])
def test_leased_stage_rechecks_before_return(monkeypatch,sealed):
    lease,raw,report,calibration,policy=inputs()
    current=deepcopy(lease.execution_profile['donor_qualification']);calls=[]
    if sealed:
        from uuid import uuid4
        from photo_worker.v2.contract_lock import SQL_TRANSPORT_SCHEMAS_SHA256
        lease.execution_profile.update(contracts_sha=SQL_TRANSPORT_SCHEMAS_SHA256,
            v2_input_manifest={'artifact_id':str(uuid4()),'sha256':'a'*64},v2_entry_count=20,v2_entries_sha='c'*64)
    def live(owned,*args):
        assert ('contracts_sha' in owned.execution_profile)==sealed
        calls.append('lease');return True
    def execute(*args,**kwargs):
        assert set(kwargs['profile'])=={'backend','renderer_version','portrait_runtime_sha','resource_estimate','donor_qualification'}
        calls.append('stage');return 'result' 
    monkeypatch.setattr(stage,'prepare_and_stage_manual_open_eyes',execute)
    assert stage.prepare_and_stage_leased_manual_open_eyes(SimpleNamespace(donor_portrait_lease_ready=live,donor_source_ready=lambda *args:True),lease,raw,
        report_raw=report,calibration_raw=calibration,qualification_policy=policy,
        load_qualification_pin=lambda:deepcopy(current),check_active=lambda:None,expected_runtime_sha='b'*64)=='result'
    assert calls==['lease','stage','lease']



@pytest.mark.parametrize('mode',['lease','params','profile_override','threshold_override','runtime'])
def test_leased_manual_failure_cannot_reach_processing(monkeypatch,mode):
    lease,raw,report,calibration,policy=inputs()
    current=deepcopy(lease.execution_profile['donor_qualification'])
    def forbidden(*args,**kwargs):raise AssertionError('processing reached')
    monkeypatch.setattr(stage,'prepare_and_stage_manual_open_eyes',forbidden)
    kwargs=dict(expected_runtime_sha='b'*64)
    if mode=='params':lease.command.params.sha256='0'*64
    if mode=='profile_override':kwargs['profile']=lease.execution_profile
    if mode=='threshold_override':kwargs['min_face_edge_px']=1
    if mode=='runtime':kwargs['expected_runtime_sha']='0'*64
    with pytest.raises((ValueError,stage.OperationFailure)):
        stage.prepare_and_stage_leased_manual_open_eyes(SimpleNamespace(donor_portrait_lease_ready=lambda *args:False if mode=='lease' else True,donor_source_ready=lambda *args:True),lease,raw,
            report_raw=report,calibration_raw=calibration,qualification_policy=policy,
            load_qualification_pin=lambda:deepcopy(current),check_active=lambda:None,**kwargs)


@pytest.mark.parametrize('mode',['oversize','empty','hash','cancelled'])
def test_leased_params_preflight_precedes_json_and_sql(monkeypatch,mode):
    lease,raw,report,calibration,policy=inputs()
    def forbidden(*args,**kwargs):raise AssertionError('preflight reached decoder or SQL')
    monkeypatch.setattr(stage,'PortraitOpenEyesParams',SimpleNamespace(model_validate_json=forbidden))
    if mode=='oversize':raw=b' '*(65536+1)
    if mode=='empty':raw=b''
    if mode=='hash':lease.command.params.sha256='0'*64
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
    with pytest.raises((ValueError,RuntimeError)):
        stage.prepare_and_stage_leased_manual_open_eyes(SimpleNamespace(donor_portrait_lease_ready=forbidden),lease,raw,
            report_raw=report,calibration_raw=calibration,qualification_policy=policy,
            load_qualification_pin=forbidden,check_active=active,expected_runtime_sha='b'*64)
