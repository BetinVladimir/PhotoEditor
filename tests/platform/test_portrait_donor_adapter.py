from types import SimpleNamespace
import pytest
from photo_worker.operations.portrait import donor_adapter as module
from photo_worker.runtime.executor import OperationFailure
from test_portrait_donor_runtime_lock import current_runtime


def test_installed_donor_operation_refuses_misrouted_profile_before_artifacts(tmp_path,monkeypatch):
    pinned=current_runtime()
    monkeypatch.setattr(module,'verify_installed_runtime',lambda *args:None)
    options=module.DonorRuntimeOptions(pinned,'a'*64,tmp_path,tmp_path,100,100)
    database=SimpleNamespace(**{name:lambda *args:True for name in
        ('donor_input_evidence','donor_portrait_lease_ready','donor_source_ready','donor_qualification_evidence','donor_manual_origin')})
    operations=module.donor_portrait_operations(SimpleNamespace(max_scratch_bytes=200),database,SimpleNamespace(get_object=lambda *args:None),bucket='bucket',options=options)
    assert [(op.name,op.version,op.resource_class) for op in operations]==[('portrait.open_eyes','1','cpu')]
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',resource_class='cpu'),execution_profile={'backend':'cuda'}))
    with pytest.raises(OperationFailure) as exc:operations[0].execute(ctx)
    assert exc.value.error.code=='CONFIG_INVALID'


def test_donor_installation_requires_concrete_current_sql_gates():
    with pytest.raises(ValueError):module.donor_portrait_operations(None,None,None,bucket='bucket',options=None)


@pytest.mark.parametrize('fail',[False,True])
def test_installed_operation_passes_owned_runtime_and_closes_artifacts(tmp_path,monkeypatch,fail):
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.v2 import portrait_open_eyes_execute as execution
    pinned=current_runtime().model_copy(deep=True);digest=canonical_hash(pinned);calls=[]
    options=module.DonorRuntimeOptions(pinned,digest,tmp_path,tmp_path,100,200)
    database=SimpleNamespace(**{name:lambda *args:True for name in
        ('donor_input_evidence','donor_portrait_lease_ready','donor_source_ready','donor_qualification_evidence','donor_manual_origin')})
    objects=SimpleNamespace(get_object=lambda *args:None)
    class Artifacts:
        def __enter__(self):calls.append('enter');return self
        def __exit__(self,*args):calls.append('close')
    artifacts=Artifacts();config=SimpleNamespace(max_scratch_bytes=300)
    def create(ctx,config,roles):
        assert config.max_scratch_bytes==300
        assert roles==frozenset({'pixels','working_image','mask_pixels','mask','report','document'})
        calls.append('create');return artifacts
    def execute(ctx,session,db,**kwargs):
        assert session is artifacts and db is database and kwargs['qualification_objects'] is objects
        assert canonical_hash(kwargs['runtime_lock'])==digest
        assert kwargs['expected_runtime_sha']==digest
        assert kwargs['source_origin'] is None and kwargs['patch_size'] is None
        assert (kwargs['mask_max_bytes'],kwargs['material_max_bytes'])==(100,200)
        calls.append('execute')
        if fail:raise OperationFailure('INPUT_INVALID')
        return 'result'
    monkeypatch.setattr(TaskArtifacts,'create',create)
    monkeypatch.setattr(execution,'execute_declared_sql_open_eyes',execute)
    operation=module.donor_portrait_operations(config,database,objects,bucket='bucket',options=options)[0]
    # Mutating caller-owned configuration cannot change the installed lock.
    pinned.components.clear()
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation='portrait.open_eyes',operation_version='1',resource_class='cpu'),
        execution_profile={'backend':'cpu','renderer_version':'v2.2','portrait_runtime_sha':digest}))
    if fail:
        with pytest.raises(OperationFailure):operation.execute(ctx)
    else:assert operation.execute(ctx)=='result'
    assert calls==['create','enter','execute','close']


@pytest.mark.parametrize('total',[None,True,0,2**53,199])
def test_direct_donor_installation_refuses_invalid_aggregate_budget(tmp_path,monkeypatch,total):
    calls=[]
    monkeypatch.setattr(module,'verify_installed_runtime',lambda *args:calls.append('runtime'))
    options=module.DonorRuntimeOptions(current_runtime(),'a'*64,tmp_path,tmp_path,100,100)
    db=SimpleNamespace(**{name:lambda *args:True for name in
        ('donor_input_evidence','donor_portrait_lease_ready','donor_source_ready','donor_qualification_evidence','donor_manual_origin')})
    with pytest.raises(ValueError,match='SCRATCH_INVALID'):
        module.donor_portrait_operations(SimpleNamespace(max_scratch_bytes=total),db,
            SimpleNamespace(get_object=lambda *args:None),bucket='bucket',options=options)
    assert calls==[]
