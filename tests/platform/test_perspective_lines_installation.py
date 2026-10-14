from types import SimpleNamespace

import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical import perspective_lines_installation as module
from photo_worker.runtime.executor import OperationFailure


def installed(monkeypatch):
    checks = []
    executions = []
    class Lock:
        @staticmethod
        def model_validate_json(raw):
            return bytes(raw)
    monkeypatch.setattr(module, 'PerspectiveLinesRuntimeLock', Lock)
    monkeypatch.setattr(module, 'verify_installed_runtime', lambda lock, digest: checks.append((lock,digest)))
    monkeypatch.setattr(module, 'perspective_line_detection_operations',
        lambda *args, **kwargs: [SimpleNamespace(execute=lambda ctx: executions.append(ctx) or 'produced')])
    installation = module.PerspectiveLinesInstallation({'schema_version':1}, 'a'*64, 'b'*64)
    operation = module.installed_perspective_line_operations(None,installation)[0]
    policy = module.execution_policy('b'*64,'a'*64)
    ctx = SimpleNamespace(check_active=lambda: None,lease=SimpleNamespace(
        command=SimpleNamespace(operation='optical.perspective_lines',operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend='cpu',precision='float32',renderer_version='v2.2',
            runtime_lock_sha='a'*64,execution_policy_sha=canonical_hash(policy))))
    return operation,ctx,checks,executions,installation


def test_installed_factory_checks_runtime_at_startup_and_each_task(monkeypatch):
    operation,ctx,checks,executions,installation = installed(monkeypatch)
    assert len(checks)==1
    installation.runtime_lock['schema_version']=2
    assert operation.execute(ctx)=='produced'
    assert len(checks)==3 and checks[0]==checks[1]==checks[2]
    assert executions==[ctx]


@pytest.mark.parametrize('field,value',[
    ('runtime_lock_sha','c'*64),('execution_policy_sha','c'*64),
    ('backend','cuda'),('precision','float16'),('renderer_version','v2.1')])
def test_wrong_task_pins_refused_before_runtime_and_artifact_io(monkeypatch,field,value):
    operation,ctx,checks,executions,_ = installed(monkeypatch)
    ctx.lease.execution_profile[field]=value
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        operation.execute(ctx)
    assert len(checks)==1 and not executions


def test_changed_runtime_and_cancelled_task_never_reach_producer(monkeypatch):
    operation,ctx,checks,executions,_ = installed(monkeypatch)
    def changed(*args):
        raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    monkeypatch.setattr(module,'verify_installed_runtime',changed)
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        operation.execute(ctx)
    assert not executions
    def cancelled():
        raise RuntimeError('cancelled')
    ctx.check_active=cancelled
    with pytest.raises(RuntimeError,match='cancelled'):
        operation.execute(ctx)
    assert len(checks)==1 and not executions


def test_execution_policy_matches_backend_contract_and_refuses_unpinned_values():
    policy=module.execution_policy('b'*64,'a'*64)
    assert policy==dict(schema_version=1,operation='optical.perspective_lines',operation_version=1,
        backend='cpu',precision='float32',renderer_version='v2.2',working_icc_sha='b'*64,
        runtime_lock_sha='a'*64,max_pixels=200000000,proxy_edge=1600,max_lines=128)
    for value in ('',None,'A'*64,'x'*64,'a'*63):
        with pytest.raises(ValueError): module.execution_policy(value,'a'*64)
        with pytest.raises(ValueError): module.execution_policy('b'*64,value)
    with pytest.raises(ValueError): module.installed_perspective_line_operations(None,None)


def test_runtime_changed_during_detection_refuses_produced_result(monkeypatch):
    operation,ctx,checks,executions,_=installed(monkeypatch)
    original=module.verify_installed_runtime
    def verify(lock,digest):
        if executions:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
        return original(lock,digest)
    monkeypatch.setattr(module,'verify_installed_runtime',verify)
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        operation.execute(ctx)
    assert executions==[ctx]


def test_cancellation_after_detection_refuses_result_before_runtime_recheck(monkeypatch):
    operation,ctx,checks,executions,_=installed(monkeypatch)
    def active():
        if executions:raise RuntimeError('cancelled after detection')
    ctx.check_active=active
    with pytest.raises(RuntimeError,match='cancelled after detection'):
        operation.execute(ctx)
    assert executions==[ctx] and len(checks)==2
