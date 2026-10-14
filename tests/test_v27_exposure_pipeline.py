from types import SimpleNamespace
import numpy as np
import pytest
from photo_worker.operations.semantic.exposure_pipeline import execute_encoded_local_exposure
from test_v27_exposure_loader import loader_fixture
from photo_worker.contracts.base import parse_json


def inputs(tmp_path):
    edit,root,artifacts,events=loader_fixture(tmp_path)
    candidate=artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['candidate']['artifact_id']),None)
    intent=artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['intent']['artifact_id']),None)
    mask=parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=edit['mask']['artifact_id']),None))
    weights=np.zeros((8,8),np.float32);weights[:,4:]=.5
    np.save(tmp_path/(mask['artifact']['artifact_id']+'.npy'),weights,allow_pickle=False)
    return edit,root,artifacts,events,candidate,intent


def test_complete_encoded_pipeline_exact_outside_and_expected_gamma_gain(tmp_path):
    edit,root,artifacts,events,candidate,intent=inputs(tmp_path)
    result=execute_encoded_local_exposure(artifacts,edit,candidate,intent,protection_ref=root,
        verify_origin=lambda *args:True,verify_color=lambda value:True,verify_admission=lambda edit:True,
        max_bytes=64*1024**2+204*64,check_active=lambda:None)
    np.testing.assert_array_equal(result[:,:4],np.full((8,4,3),.5,np.float32))
    np.testing.assert_allclose(result[:,4:],.5*2**(.25*.5*.5/2.4),rtol=1e-6)
    assert events.count('open')==events.count('close')==4


def test_full_budget_denial_precedes_array_materialization(tmp_path):
    edit,root,artifacts,events,candidate,intent=inputs(tmp_path)
    with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
        execute_encoded_local_exposure(artifacts,edit,candidate,intent,protection_ref=root,
            verify_origin=lambda *args:True,verify_color=lambda value:True,verify_admission=lambda edit:True,
            max_bytes=64*1024**2+204*64-1,check_active=lambda:None)
    assert events==[]


def test_final_admission_revocation_returns_no_encoded_result(tmp_path):
    edit,root,artifacts,events,candidate,intent=inputs(tmp_path)
    calls=[]
    def admission(edit):
        calls.append(True)
        return len(calls)!=10
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        execute_encoded_local_exposure(artifacts,edit,candidate,intent,protection_ref=root,
            verify_origin=lambda *args:True,verify_color=lambda value:True,verify_admission=admission,
            max_bytes=64*1024**2+204*64,check_active=lambda:None)
    assert len(calls)==10 and events.count('open')==events.count('close')==4


def test_full_encoded_memory_bounds_and_24mp_cost():
    from photo_worker.operations.semantic.exposure_pipeline import encoded_local_exposure_memory
    assert encoded_local_exposure_memory((4000,6000))==4_963_108_864
    assert encoded_local_exposure_memory((1,200_000_000))==40_867_108_864
    for shape in ((0,1),(1,-1),(True,1),(1,200_000_001),(2**63,2**63)):
        with pytest.raises(ValueError):encoded_local_exposure_memory(shape)
