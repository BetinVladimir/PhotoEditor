import numpy as np
import pytest
from test_v27_approved_semantic_edit_contract import fixture
from test_v27_approved_edit_binding import documents
from photo_worker.operations.semantic.execute_exposure import LocalExposureArrays,execute_local_exposure


def test_execution_owns_dependencies_and_rechecks_admission():
    edit,candidate,intent=documents(ev=1.,strength=1.)
    rgb=np.full((1,1,3),.25,dtype=np.float32)
    mask=np.ones((1,1),dtype=np.float32);protection=np.zeros_like(mask)
    checks=0
    def verify(value):
        nonlocal checks
        checks+=1
        if checks==2:rgb[:]=0;mask[:]=0
        value.ev=-1.
        return True
    result=execute_local_exposure(edit,candidate,intent,verify_admission=verify,
        load_verified_arrays=lambda value,**kwargs:LocalExposureArrays(rgb,mask,protection,loader_peak_bytes=20,retained_loader_bytes=0),
        max_bytes=96,check_active=lambda:None)
    np.testing.assert_array_equal(result,np.full((1,1,3),.5,dtype=np.float32))
    assert checks==3 and edit['ev']==1.


@pytest.mark.parametrize('denied_at',[1,2,3])
def test_revocation_at_each_boundary_returns_no_output(denied_at):
    checks=0;loads=0
    def verify(value):
        nonlocal checks
        checks+=1
        return checks!=denied_at
    def load(value,*,max_bytes):
        assert max_bytes==96
        nonlocal loads
        loads+=1
        return LocalExposureArrays(np.ones((1,1,3),dtype=np.float32),np.ones((1,1),dtype=np.float32),np.zeros((1,1),dtype=np.float32),loader_peak_bytes=20,retained_loader_bytes=0)
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        execute_local_exposure(*documents(),verify_admission=verify,load_verified_arrays=load,max_bytes=96,check_active=lambda:None)
    assert loads==(0 if denied_at==1 else 1)


def test_missing_installation_and_wrong_semantic_kind_are_denied():
    with pytest.raises(ValueError,match='INSTALLATION_REQUIRED'):
        execute_local_exposure(*documents(),verify_admission=None,load_verified_arrays=None,max_bytes=96,check_active=lambda:None)
    with pytest.raises(ValueError,match='KIND_INVALID'):
        execute_local_exposure(fixture(),b'{}',b'{}',verify_admission=lambda value:True,load_verified_arrays=lambda value,**kwargs:None,max_bytes=96,check_active=lambda:None)


def test_substituted_intent_parameters_never_reach_array_loader():
    edit,candidate,intent=documents()
    edit['ev']=-.75
    loads=[]
    with pytest.raises(ValueError,match='INTENT_BINDING'):
        execute_local_exposure(edit,candidate,intent,verify_admission=lambda value:True,
            load_verified_arrays=lambda value,**kwargs:loads.append(value),max_bytes=96,check_active=lambda:None)
    assert loads==[]


@pytest.mark.parametrize('peak,retained,budget',[(20,1,96),(97,0,96),(19,0,96),(True,0,96),(20,False,96),(20,21,200)])
def test_complete_loader_budget_denied_before_executor_copies(peak,retained,budget):
    class NoCopy(np.ndarray):
        def copy(self,*args,**kwargs):
            raise AssertionError('allocation before full memory admission')
    rgb=np.ones((1,1,3),dtype=np.float32).view(NoCopy)
    mask=np.ones((1,1),dtype=np.float32).view(NoCopy)
    protection=np.zeros((1,1),dtype=np.float32).view(NoCopy)
    with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
        execute_local_exposure(*documents(),verify_admission=lambda value:True,
            load_verified_arrays=lambda value,**kwargs:LocalExposureArrays(rgb,mask,protection,peak,retained),
            max_bytes=budget,check_active=lambda:None)


def test_retained_loader_memory_is_subtracted_from_kernel_budget(monkeypatch):
    from photo_worker.operations.semantic import execute_exposure
    original=execute_exposure.apply_local_exposure
    budgets=[]
    def kernel(*args,**kwargs):
        budgets.append(kwargs['max_bytes'])
        return original(*args,**kwargs)
    monkeypatch.setattr(execute_exposure,'apply_local_exposure',kernel)
    rgb=np.ones((1,1,3),dtype=np.float32)
    mask=np.ones((1,1),dtype=np.float32);protection=np.zeros_like(mask)
    execute_local_exposure(*documents(),verify_admission=lambda value:True,
        load_verified_arrays=lambda value,**kwargs:LocalExposureArrays(rgb,mask,protection,30,10),
        max_bytes=106,check_active=lambda:None)
    assert budgets==[96]
