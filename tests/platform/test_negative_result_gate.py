from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.operations.optical import negative_adapter as module
from photo_worker.runtime.executor import OperationFailure


@pytest.mark.parametrize('drift',[False,True])
@pytest.mark.parametrize('invalid_profile',[False,True])
def test_negative_rechecks_runtime_before_returning_result(monkeypatch,drift,invalid_profile):
    import photo_worker.runtime.artifacts as artifacts
    import photo_worker.v2.negative_execute as executor
    events=[]
    class Lock:
        @staticmethod
        def model_validate_json(raw):return Lock()
        def model_copy(self,deep):return Lock()
    monkeypatch.setattr(module,'NegativeRuntimeLock',Lock)
    def verify(*args):
        events.append('verify')
        if drift and 'execute' in events:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    monkeypatch.setattr(module,'verify_installed_runtime',verify)
    @contextmanager
    def create(*args):
        events.append('open')
        try:yield object()
        finally:events.append('close')
    monkeypatch.setattr(artifacts.TaskArtifacts,'create',create)
    def run(*args,**kwargs):events.append('execute');return 'produced'
    monkeypatch.setattr(executor,'execute_declared_sql_negative',run)
    db=SimpleNamespace(**{name:lambda *args:None for name in ('negative_input_evidence','negative_lease_ready','negative_source_ready','negative_qualification_evidence')})
    config=SimpleNamespace(max_scratch_bytes=1000)
    options=module.NegativeRuntimeOptions({'schema_version':1},'a'*64,100,200)
    operation=module.negative_operations(config,db,SimpleNamespace(get_object=lambda *args:None),bucket='photos',options=options,verify_color=lambda *args:None)[0]
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(operation='optical.negative',operation_version='1',resource_class='cpu'),execution_profile=dict(backend='cpu',renderer_version='v2.2',negative_runtime_sha='a'*64)))
    if invalid_profile:
        ctx.lease.execution_profile=[]
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):operation.execute(ctx)
        assert events==['verify']
        return
    if drift:
        with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):operation.execute(ctx)
    else:assert operation.execute(ctx)=='produced'
    assert events==['verify','verify','open','execute','verify','close']
