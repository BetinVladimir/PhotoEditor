from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical import negative_adapter as module
from photo_worker.runtime.executor import OperationFailure
from test_negative_runtime_lock import lock


def gates():return SimpleNamespace(**{name:lambda *args:True for name in
    ('negative_input_evidence','negative_lease_ready','negative_source_ready','negative_qualification_evidence')})


@pytest.mark.parametrize('fail',[False,True])
def test_negative_adapter_retains_runtime_and_closes_artifacts(monkeypatch,fail):
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.v2 import negative_execute
    runtime=lock();digest=canonical_hash(runtime);calls=[]
    class Artifacts:
        def __enter__(self):calls.append('enter');return self
        def __exit__(self,*args):calls.append('close')
    def create(ctx,config,roles):
        assert roles==frozenset({'pixels','working_image','mask_pixels','mask','report','document'})
        calls.append('create');return Artifacts()
    def execute(ctx,artifacts,database,**kwargs):
        assert canonical_hash(kwargs['runtime_lock'])==digest and kwargs['runtime_sha']==digest
        assert kwargs['source_max_bytes']==100 and kwargs['ram_max_bytes']==1<<30
        calls.append('execute')
        if fail:raise OperationFailure('INPUT_INVALID')
        return 'result'
    monkeypatch.setattr(TaskArtifacts,'create',create)
    monkeypatch.setattr(negative_execute,'execute_declared_sql_negative',execute)
    op=module.negative_operations(SimpleNamespace(max_scratch_bytes=200),gates(),SimpleNamespace(get_object=lambda *args:None),
        bucket='bucket',options=module.NegativeRuntimeOptions(runtime,digest,100,1<<30),verify_color=lambda image:True)[0]
    runtime.components.clear()
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=SimpleNamespace(
        operation='optical.negative',operation_version='1',resource_class='cpu'),execution_profile=dict(
        backend='cpu',renderer_version='v2.2',negative_runtime_sha=digest)))
    if fail:
        with pytest.raises(OperationFailure):op.execute(ctx)
    else:assert op.execute(ctx)=='result'
    assert calls==['create','enter','execute','close']
    ctx.lease.execution_profile['backend']='cuda'
    with pytest.raises(OperationFailure):op.execute(ctx)
    assert calls==['create','enter','execute','close']


@pytest.mark.parametrize('mode',['sql','color','scratch','ram','object'])
def test_negative_adapter_refuses_missing_installation(mode):
    runtime=lock();digest=canonical_hash(runtime)
    db=gates();objects=SimpleNamespace(get_object=lambda *args:None);color=lambda image:None
    if mode=='sql':db.negative_source_ready=None
    if mode=='color':color=None
    if mode=='object':objects=None
    with pytest.raises(ValueError):module.negative_operations(SimpleNamespace(max_scratch_bytes=100 if mode=='scratch' else 200),
        db,objects,bucket='bucket',options=module.NegativeRuntimeOptions(runtime,digest,100,True if mode=='ram' else 1<<30),verify_color=color)
