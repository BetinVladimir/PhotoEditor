from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.render_result import StagedRenderResult
from photo_worker.v2 import render_task
from photo_worker.v2.render_qualification_pin import RenderQualificationPin
from test_v26_render_authored import fixture


@pytest.mark.parametrize('underbudget',[False,True])
@pytest.mark.parametrize('cache_mode',['disabled','hit','miss','corrupt','missing_version','lease_lost'])
def test_task_requires_admission_and_resources_before_renderer(tmp_path,monkeypatch,underbudget,cache_mode):
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
    pin=RenderQualificationPin(inputs.recipe.scalars.model_lock_sha,'b'*64,'cuda','c'*64,'d'*64,'e'*64)
    admission=SimpleNamespace(admit=lambda *args:events.append('admission'),qualification=lambda *args:pin)
    if cache_mode!='disabled':
        class Admission(render_task.SQLRenderTaskAdmission):
            def admit(self,*args):events.append('admission')
            def qualification(self,*args):return pin
        admission=Admission(object())
        from photo_worker.v2 import render_cache_worker
        class Cache:
            def __init__(self,database,*,max_bytes):assert max_bytes>0
            def try_publish(self,*args):
                events.append('cache')
                if cache_mode=='corrupt':
                    from photo_worker.v2.adoption import AdoptionInvalid
                    raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
                if cache_mode in ('missing_version','lease_lost'):
                    from photo_worker.runtime.gateway import GatewayError
                    raise GatewayError('ARTIFACT_MISSING' if cache_mode=='missing_version' else 'LEASE_LOST')
                return StagedRenderResult(ref,(ref,),123,True) if cache_mode=='hit' else None
        monkeypatch.setattr(render_cache_worker,'WorkerRenderCache',Cache)
    def execute(*args):events.append('execute');return StagedRenderResult(ref,(ref,),123,True)
    renderer=SimpleNamespace(backend='cuda',runtime_sha256='b'*64,
        model_lock_sha=inputs.recipe.scalars.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        qualification_pin=pin,provider_peak=arguments['provider_peak'],verify_runtime=lambda:events.append('runtime'),execute=execute)
    try:
        if underbudget:
            with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
                render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert events==['runtime','admission','metadata']
        elif cache_mode in ('corrupt','missing_version'):
            with pytest.raises(OperationFailure) as failure:
                render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert failure.value.error.code=='DEPENDENCY_UNAVAILABLE' and failure.value.error.retryable
            assert events==['runtime','admission','metadata','cache']
        elif cache_mode=='lease_lost':
            from photo_worker.runtime.gateway import GatewayError
            with pytest.raises(GatewayError) as failure:
                render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert failure.value.code=='LEASE_LOST'
            assert events==['runtime','admission','metadata','cache']
        else:
            result=render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert events==['runtime','admission','metadata']+([] if cache_mode=='disabled' else ['cache'])+([] if cache_mode=='hit' else ['execute'])
            assert result.outputs==[ref] and result.review_required and result.bytes_written==123
    finally:artifacts.scratch.cleanup()


def test_unconfigured_task_rejects_before_context_or_artifacts():
    with pytest.raises(OperationFailure,match='RENDER_RUNTIME_UNAVAILABLE'):
        render_task.execute_render_task(None,None,None,None)


@pytest.mark.parametrize('race',[None,'lease_lost','profile_changed','qualification_changed'])
def test_cache_miss_rechecks_primary_context_before_profiled_render(tmp_path,monkeypatch,race):
    from dataclasses import asdict
    from hashlib import sha256
    from photo_worker.v2 import render_cache_worker
    artifacts,inputs,_,_,arguments,_=fixture(tmp_path)
    raw=canonical_bytes(inputs.recipe)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    pin=RenderQualificationPin(inputs.recipe.scalars.model_lock_sha,'b'*64,'cuda','c'*64,'d'*64,'e'*64)
    profile=dict(arguments['execution_profile'],backend='cuda',renderer_version='v2.2',render_runtime_sha='b'*64,
        model_lock_sha=pin.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,tile_size=512,alpha_mode='require_float32')
    command=SimpleNamespace(operation='render.compose',operation_version='1',resource_class='gpu',params=ref)
    lease=SimpleNamespace(command=command,execution_profile=profile)
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    events=[]
    row=[ref.artifact_id,ref.sha256,dict(profile),[],asdict(pin)]
    if race=='lease_lost':row=None
    elif race=='profile_changed':row[2]['v2_entries_sha']='f'*64
    elif race=='qualification_changed':row[4]['device_profile_sha']='f'*64
    def primary(actual_lease,actual_raw):
        assert actual_lease is lease and actual_raw==raw
        events.append('primary');return row
    database=SimpleNamespace(admit_render_recipe=lambda *args:True,render_qualification=lambda *args:asdict(pin),
        render_cache_context=primary,render_executed_base_receipt=lambda *args:None)
    admission=render_task.SQLRenderTaskAdmission(database)
    monkeypatch.setattr(render_task,'read_render_document',lambda *args:raw)
    monkeypatch.setattr(render_task,'load_frozen_render_inputs',lambda *args,**kwargs:inputs)
    class Cache:
        def __init__(self,*args,**kwargs):pass
        def try_publish(self,*args):events.append('cache-miss');return None
    monkeypatch.setattr(render_cache_worker,'WorkerRenderCache',Cache)
    renderer=render_task.ProfiledTaskRenderer(backend='cuda',runtime_path='unused',runtime_sha256='b'*64,
        model_lock_sha=pin.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,
        provider_peak=arguments['provider_peak'],qualification_pin=pin,
        verify_qualification=lambda *args:None,load_providers=lambda *args:None)
    monkeypatch.setattr(renderer,'verify_runtime',lambda:None)
    def execute(actual_ctx,actual_artifacts,actual_inputs,*,authoritative_inputs):
        assert actual_ctx is ctx and actual_artifacts is artifacts and actual_inputs is inputs
        assert authoritative_inputs.execution_profile()==profile and authoritative_inputs.qualification_pin==pin
        events.append('render');return StagedRenderResult(ref,(ref,),123,False)
    monkeypatch.setattr(renderer,'execute',execute)
    try:
        if race is None:
            result=render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert result.outputs==[ref] and events==['cache-miss','primary','render']
        else:
            with pytest.raises(OperationFailure) as rejected:
                render_task.execute_render_task(ctx,artifacts,admission,renderer)
            assert rejected.value.error.code=='DEPENDENCY_UNAVAILABLE' and rejected.value.error.retryable
            assert events==['cache-miss','primary']
    finally:artifacts.scratch.cleanup()

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


@pytest.mark.parametrize('override',['execution_profile','runtime_lock_sha','provider_peak','tile_size','source_result'])
def test_provider_loader_cannot_override_admitted_configuration(override):
    supplied=dict(operators=(),scalar_overrides=(),render_base=None,render_proposal=None,
        measure=None,preparation={override:None})
    from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
    from photo_worker.v2.adoption import ArtifactRef as ScopedRef
    ref=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    pin=RenderQualificationPin('b'*64,'a'*64,'cuda','c'*64,'d'*64,'e'*64)
    scoped=RenderAdoptionInputs(ScopedRef(ref.artifact_id,ref.sha256),canonical_bytes({}),(),pin)
    renderer=render_task.ProfiledTaskRenderer(backend='cuda',runtime_path='unused',runtime_sha256='a'*64,
        model_lock_sha='b'*64,icc_lock_sha='c'*64,provider_peak={},
        qualification_pin=pin,verify_qualification=lambda supplied:None,load_providers=lambda *args:supplied)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={},command=SimpleNamespace(params=ref)))
    inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(source=SimpleNamespace(source=ref))))
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        renderer.execute(ctx,None,inputs,authoritative_inputs=scoped)


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


@pytest.mark.parametrize('failure',[None,'missing','shape','recipe','profile','qualification','parent_duplicate'])
def test_worker_reads_primary_parents_with_exact_recipe_profile_and_qualification(failure):
    from dataclasses import asdict
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    pin=RenderQualificationPin('b'*64,'c'*64,'cuda','d'*64,'e'*64,'f'*64)
    profile={'backend':'cuda','v2_entries_sha':'1'*64}
    parent={'artifact_id':str(uuid4()),'sha256':'2'*64}
    row=[ref.artifact_id,ref.sha256,dict(profile),[parent],asdict(pin)]
    if failure=='missing':row=None
    elif failure=='shape':row.pop()
    elif failure=='recipe':row[0]=uuid4()
    elif failure=='profile':row[2]['v2_entries_sha']='3'*64
    elif failure=='qualification':row[4]['device_profile_sha']='3'*64
    elif failure=='parent_duplicate':row[3].append(dict(parent))
    lease=SimpleNamespace(command=SimpleNamespace(params=ref),execution_profile=profile)
    calls=[]
    def primary(actual_lease,raw):
        calls.append((actual_lease,raw));return row
    admission=render_task.SQLRenderTaskAdmission(SimpleNamespace(render_cache_context=primary,render_executed_base_receipt=lambda *args:None))
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    if failure is None:
        scoped=admission.inputs(ctx,b'{"exact":"recipe"}',pin)
        assert scoped.qualification_pin==pin and scoped.execution_profile()==profile
        assert scoped.parent_roots[0].sha256==parent['sha256']
        row[3][0]['sha256']='4'*64
        assert scoped.parent_roots[0].sha256=='2'*64
    else:
        with pytest.raises(OperationFailure) as rejected:
            admission.inputs(ctx,b'{"exact":"recipe"}',pin)
        assert rejected.value.error.code=='DEPENDENCY_UNAVAILABLE' and rejected.value.error.retryable
    assert calls==[(lease,b'{"exact":"recipe"}')]

@pytest.mark.parametrize('field',['device_profile_sha','dataset_sha','calibration_sha'])
def test_worker_full_pin_drift_rejects_before_array_loading(tmp_path,monkeypatch,field):
    from dataclasses import replace
    artifacts,inputs,_,_,options,_=fixture(tmp_path)
    pin=RenderQualificationPin(inputs.recipe.scalars.model_lock_sha,'b'*64,'cuda','c'*64,'d'*64,'e'*64)
    profile=dict(options['execution_profile'],backend='cuda',renderer_version='v2.2',render_runtime_sha='b'*64,
        model_lock_sha=pin.model_lock_sha,icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,tile_size=512,alpha_mode='require_float32')
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile,
        command=SimpleNamespace(params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))))
    monkeypatch.setattr(render_task,'read_render_document',lambda *args:canonical_bytes(inputs.recipe))
    def forbidden(*args,**kwargs):raise AssertionError('arrays reached before full pin matching')
    monkeypatch.setattr(render_task,'load_frozen_render_inputs',forbidden)
    admission=SimpleNamespace(admit=lambda *args:None,qualification=lambda *args:pin)
    renderer=SimpleNamespace(backend='cuda',runtime_sha256='b'*64,model_lock_sha=pin.model_lock_sha,
        icc_lock_sha=inputs.recipe.scalars.icc_lock_sha,qualification_pin=replace(pin,**{field:'f'*64}),verify_runtime=lambda:None)
    try:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            render_task.execute_render_task(ctx,artifacts,admission,renderer)
    finally:artifacts.scratch.cleanup()


@pytest.mark.parametrize('mutate_profile',[False,True])
def test_provider_loader_cannot_mutate_execution_metadata(monkeypatch,mutate_profile):
    from photo_worker.v2.render_adoption_inputs import RenderAdoptionInputs
    from photo_worker.v2.adoption import ArtifactRef as ScopedRef
    import photo_worker.v2.render_execute as execute
    reference=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    pin=RenderQualificationPin('b'*64,'a'*64,'cuda','c'*64,'d'*64,'e'*64)
    profile=dict(tile_size=512,alpha_mode='require_float32')
    scoped=RenderAdoptionInputs(ScopedRef(reference.artifact_id,reference.sha256),canonical_bytes(profile),(),pin)
    inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(source=SimpleNamespace(source=reference))))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile,command=SimpleNamespace(params=reference)))
    def load(ctx,artifacts,owned):
        owned.recipe.recipe.source.source=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
        if mutate_profile:ctx.lease.execution_profile['tile_size']=256
        return dict(operators=(),scalar_overrides=(),render_base=None,render_proposal=None,measure=None,preparation={})
    captured=[]
    def run(artifacts,owned,*args,**kwargs):
        captured.append(owned.recipe.recipe.source.source)
        assert kwargs['execution_profile']==dict(tile_size=512,alpha_mode='require_float32')
        return 'rendered'
    monkeypatch.setattr(execute,'execute_profiled_authored_render',run)
    renderer=render_task.ProfiledTaskRenderer(backend='cuda',runtime_path='unused',runtime_sha256='a'*64,
        model_lock_sha='b'*64,icc_lock_sha='c'*64,provider_peak={},qualification_pin=pin,
        verify_qualification=lambda supplied:None,load_providers=load)
    if mutate_profile:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            renderer.execute(ctx,None,inputs,authoritative_inputs=scoped)
        assert not captured
    else:
        assert renderer.execute(ctx,None,inputs,authoritative_inputs=scoped)=='rendered'
        assert captured==[reference]
    assert inputs.recipe.recipe.source.source==reference


def test_profiled_renderer_retains_owned_provider_resource_peak():
    peak=dict(ram_bytes=123,scratch_bytes=456,vram_bytes=789,output_bytes=1011)
    renderer=render_task.ProfiledTaskRenderer(backend='cuda',runtime_path='unused',
        runtime_sha256='a'*64,model_lock_sha='b'*64,icc_lock_sha='c'*64,
        provider_peak=peak,verify_qualification=lambda *args:None,
        load_providers=lambda *args:None)
    peak['ram_bytes']=0
    observed=renderer.provider_peak
    observed['scratch_bytes']=0
    assert renderer.provider_peak==dict(ram_bytes=123,scratch_bytes=456,
        vram_bytes=789,output_bytes=1011)
    with pytest.raises(AttributeError):
        renderer.provider_peak={}
