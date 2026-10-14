from types import SimpleNamespace
import pytest
from photo_worker.runtime.depth_config import depth_runtime_locks_from_env,configured_depth_runtime
from photo_worker.runtime.service import WorkerConfigurationError


def test_depth_runtime_configuration_is_explicit_and_complete():
    assert depth_runtime_locks_from_env({},'gpu','mps',True) is None
    locks=dict(PHOTO_DEPTH_MODEL_LOCK_SHA='a'*64,PHOTO_DEPTH_RUNTIME_LOCK_SHA='b'*64,PHOTO_DEPTH_DEVICE_PROFILE_SHA='c'*64)
    with pytest.raises(WorkerConfigurationError):depth_runtime_locks_from_env(locks,'gpu','mps',True)
    enabled={**locks,'PHOTO_DEPTH_RUNTIME_ENABLED':'true'}
    assert depth_runtime_locks_from_env(enabled,'gpu','mps',True)==('a'*64,'b'*64,'c'*64)
    for resource,backend,models in [('cpu','cpu',True),('gpu','unknown',True),('gpu','mps',False)]:
        with pytest.raises(WorkerConfigurationError):depth_runtime_locks_from_env(enabled,resource,backend,models)


def test_depth_runtime_startup_refuses_measured_mismatch(monkeypatch):
    from photo_worker.operations.depth import measurement
    config=SimpleNamespace(depth_runtime_locks=('a'*64,'b'*64,'c'*64),backend='mps')
    monkeypatch.setattr(measurement,'installed_depth_measurements',lambda *_:dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='d'*64,backend='mps'))
    with pytest.raises(WorkerConfigurationError):configured_depth_runtime(config,object())


def test_depth_runtime_startup_installs_live_verifier(monkeypatch):
    from photo_worker.operations.depth import measurement
    expected=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,backend='mps')
    config=SimpleNamespace(depth_runtime_locks=('a'*64,'b'*64,'c'*64),backend='mps')
    monkeypatch.setattr(measurement,'installed_depth_measurements',lambda *_:dict(expected))
    verifier=configured_depth_runtime(config,object())
    ctx=SimpleNamespace(lease=SimpleNamespace(execution_profile=dict(expected)),check_active=lambda:None)
    assert verifier(ctx) is True
