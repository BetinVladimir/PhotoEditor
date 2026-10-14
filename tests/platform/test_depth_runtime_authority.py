from types import SimpleNamespace
import pytest
from photo_worker.operations.depth.authority import depth_runtime_authority
from photo_worker.runtime.executor import OperationFailure


def test_depth_runtime_requires_actual_measurement_and_exact_profile():
    expected=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='mps')
    calls=[]
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(expected)),check_active=lambda:calls.append(True))
    verify=depth_runtime_authority(lambda:dict(expected),**expected)
    assert verify(ctx) is True and len(calls)==2
    ctx.lease.execution_profile['runtime_lock_sha']='d'*64
    with pytest.raises(OperationFailure):verify(ctx)


def test_depth_runtime_refuses_changed_device_measurement():
    expected=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='mps')
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(expected)),check_active=lambda:None)
    verify=depth_runtime_authority(lambda:{**expected,'device_profile_sha':'d'*64},**expected)
    with pytest.raises(OperationFailure):verify(ctx)
    with pytest.raises(ValueError):depth_runtime_authority(None,**expected)
