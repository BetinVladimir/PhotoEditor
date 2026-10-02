from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_result import StagedRenderResult
from photo_worker.v2 import render_task
from test_v26_render_authored import fixture


@pytest.mark.parametrize('underbudget',[False,True])
def test_task_requires_admission_and_resources_before_renderer(tmp_path,monkeypatch,underbudget):
    artifacts,inputs,_,_,arguments,_=fixture(tmp_path)
    events=[]
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    profile=dict(arguments['execution_profile'])
    profile.update(backend='cuda',renderer_version='v2.2',render_runtime_sha='b'*64,
        model_lock_sha=inputs.recipe.scalars.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        tile_size=512,alpha_mode='require_float32')
    if underbudget:profile['resource_estimate']=dict(profile['resource_estimate'],ram_bytes=0)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile,command=SimpleNamespace(params=ref)))
    monkeypatch.setattr(render_task,'read_render_document',lambda *args:canonical_bytes(inputs.recipe))
    def load(*args,**kwargs):events.append('metadata');return inputs
    monkeypatch.setattr(render_task,'load_frozen_render_inputs',load)
    admission=SimpleNamespace(admit=lambda *args:events.append('admission'))
    def execute(*args):events.append('execute');return StagedRenderResult(ref,(ref,),123,True)
    renderer=SimpleNamespace(backend='cuda',runtime_sha256='b'*64,
        model_lock_sha=inputs.recipe.scalars.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        provider_peak=arguments['provider_peak'],verify_runtime=lambda:events.append('runtime'),execute=execute)
    try:
        if underbudget:
            with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
                render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert events==['runtime','admission','metadata']
        else:
            result=render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert events==['runtime','admission','metadata','execute']
            assert result.outputs==[ref] and result.review_required and result.bytes_written==123
    finally:artifacts.scratch.cleanup()


def test_unconfigured_task_rejects_before_context_or_artifacts():
    with pytest.raises(OperationFailure,match='RENDER_RUNTIME_UNAVAILABLE'):
        render_task.execute_render_task(None,None,None,None)

@pytest.mark.parametrize('field,value',[('model_lock_sha','d'*64),('icc_lock_sha','d'*64),
    ('runtime_sha256',None),('backend','unsupported')])
def test_provider_pin_mismatch_rejects_before_runtime_and_artifacts(field,value):
    profile=dict(backend='cuda',render_runtime_sha='a'*64,model_lock_sha='b'*64,icc_lock_sha='c'*64)
    def forbidden():raise AssertionError('provider runtime called before pin validation')
    renderer=SimpleNamespace(backend='cuda',runtime_sha256='a'*64,model_lock_sha='b'*64,
        icc_lock_sha='c'*64,verify_runtime=forbidden)
    setattr(renderer,field,value)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile))
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        render_task.execute_render_task(ctx,None,object(),renderer)


@pytest.mark.parametrize('override',['execution_profile','runtime_lock_sha','provider_peak','tile_size'])
def test_provider_loader_cannot_override_admitted_configuration(override):
    supplied=dict(operators=(),scalar_overrides=(),render_base=None,render_proposal=None,
        measure=None,preparation={override:None})
    renderer=render_task.ProfiledTaskRenderer(backend='cuda',runtime_path='unused',runtime_sha256='a'*64,
        model_lock_sha='b'*64,icc_lock_sha='c'*64,provider_peak={},
        verify_qualification=lambda:None,load_providers=lambda *args:supplied)
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        renderer.execute(SimpleNamespace(check_active=lambda:None),None,None)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_full_operation_factory_requires_both_dependencies(backend):
    assert render_task.render_task_operations(None)==[]
    with pytest.raises(OperationFailure,match='RENDER_RUNTIME_UNAVAILABLE'):
        render_task.render_task_operations(None,admission=object())
    called=[]
    renderer=SimpleNamespace(backend=backend,verify_runtime=lambda:called.append(True))
    operations=render_task.render_task_operations(None,admission=object(),renderer=renderer)
    assert called==[True] and len(operations)==1
    assert operations[0].resource_class=='gpu'

@pytest.mark.parametrize('accepted',[True,False])
def test_sql_admission_passes_exact_recipe_bytes_and_rejects_denial(tmp_path,accepted):
    from hashlib import sha256
    artifacts,inputs,_,_,_,_=fixture(tmp_path)
    try:
        raw=canonical_bytes(inputs.recipe)
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        lease=SimpleNamespace(command=SimpleNamespace(operation='render.compose',operation_version='1',resource_class='gpu',params=ref))
        calls=[]
        def admit(actual_lease,actual_raw):
            calls.append((actual_lease,actual_raw));return accepted
        adapter=render_task.SQLRenderTaskAdmission(SimpleNamespace(admit_render_recipe=admit))
        ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
        if accepted:adapter.admit(ctx,ref,inputs.recipe,raw)
        else:
            with pytest.raises(OperationFailure,match='INPUT_INVALID'):adapter.admit(ctx,ref,inputs.recipe,raw)
        assert calls==[(lease,raw)]
        calls.clear()
        with pytest.raises(OperationFailure,match='INPUT_INVALID'):adapter.admit(ctx,ref,inputs.recipe,raw+b' ')
        assert calls==[]
    finally:artifacts.scratch.cleanup()

@pytest.mark.parametrize('row,expected',[(None,False),((False,),False),((True,),True),((1,),False)])
def test_render_database_admission_preserves_exact_attempt_and_fence(row,expected):
    from photo_worker.runtime.database import TaskDatabase
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=7,
        params=SimpleNamespace(artifact_id=uuid4(),sha256='a'*64))
    lease=SimpleNamespace(command=command,fence=19,worker_id=uuid4(),input_digest='b'*64)
    db=TaskDatabase('unused');calls=[]
    def query(sql,args):calls.append((sql,args));return row
    db._query=query
    raw=b'{"test":true}'
    assert db.admit_render_recipe(lease,raw) is expected
    assert len(calls)==1 and 'admit_v26_render_recipe' in calls[0][0]
    assert calls[0][1]==(command.tenant_id,command.run_id,command.task_id,7,19,lease.worker_id,
        command.params.artifact_id,'a'*64,raw.decode(),'b'*64)


def test_render_runtime_pins_sql_admission_transport():
    from photo_worker.v2.render_runtime import component_paths
    assert component_paths()['runtime_database'].name=='database.py'
