from uuid import uuid4
from pathlib import Path
import pytest
from typer.testing import CliRunner
from photo_worker.cli import app
from photo_worker.runtime.service import WorkerConfig,WorkerConfigurationError


@pytest.fixture(autouse=True)
def installed_startup_schema(monkeypatch):
    # These construction tests isolate current migration installation from live DB IO.
    # Dedicated installation tests cover the fail-closed SQL/readiness predicates.
    from photo_worker.runtime.database import TaskDatabase
    monkeypatch.setattr(TaskDatabase, 'media_renderer_policy_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'task_result_fence_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'editor_internal_dispatch_installed', lambda self, operation: True)


def metal_policy(backend):
    return {'PHOTO_CAPACITY_POLICY':str(Path(__file__).resolve().parents[2]/'configs'/'capacity-metal-verification.json')} if backend=='mps' else {}


def environment(tmp_path):
    return dict(PHOTO_WORKER_ID=str(uuid4()),PHOTO_WORKER_CLASS='media',DATABASE_URL='postgres://user:secret@example.test/database',AMQP_URL='amqps://user:secret@example.test',PHOTO_SCRATCH_ROOT=str(tmp_path),GATEWAY_ORIGIN='https://localhost:8443',GATEWAY_CA_FILE='ca.pem',WORKER_CERT_FILE='worker.pem',WORKER_KEY_FILE='worker.key',WORKER_MAX_SCRATCH_BYTES='4096',WORKER_MAX_DOWNLOAD_BYTES='4096',WORKER_MAX_UPLOAD_BYTES='4096')

def test_worker_config_limits_and_secret_redaction(tmp_path):
    env=environment(tmp_path);config=WorkerConfig.from_env(env)
    assert config.resource_class=='media' and 'secret' not in repr(config)
    for key,value in [('PHOTO_WORKER_ID','bad'),('PHOTO_WORKER_CLASS','gpu'),('WORKER_MAX_UPLOAD_BYTES','0'),('WORKER_MAX_SCRATCH_BYTES','9007199254740992'),('WORKER_MAX_DOWNLOAD_BYTES','1.5'),('APP_ENV','unknown'),('GATEWAY_ORIGIN','http://localhost'),('GATEWAY_ORIGIN','https://user:secret@example.test/path')]:
        with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,key:value})
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_SCRATCH_ROOT':str(tmp_path/'missing')})


def test_archive_worker_class_is_explicitly_supported(tmp_path):
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'archive'})
    assert config.resource_class=='archive' and config.backend=='cpu'


def test_learning_fit_is_registered_only_on_cpu_worker(monkeypatch,tmp_path):
    import ssl
    import photo_worker.runtime.service as service
    from photo_worker.operations.learning.fit import learning_fit_operations
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    assert [op.name for op in learning_fit_operations(None)]==['learning.fit']
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,transport=service.build_worker(config)
    assert 'learning.fit' in transport.executor.operations
    assert transport.executor.operations['learning.fit'].resource_class=='cpu'


def test_worker_cli_does_not_expose_dependency_secrets(monkeypatch,tmp_path):
    import photo_worker.runtime.service as service
    monkeypatch.setattr(service.WorkerConfig,'from_env',lambda:None)
    def fail(config):raise RuntimeError('password=private-test-secret')
    monkeypatch.setattr(service,'run_worker',fail)
    result=CliRunner().invoke(app,['run'])
    assert result.exit_code==1 and 'private-test-secret' not in result.output
    assert 'Worker stopped' in result.output


def test_registration_preflight_and_signal_cleanup(monkeypatch,tmp_path):
    import signal
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    state=dict(registered=frozenset({'missing.operation'}),started=False,aborts=0)
    class Database:
        def ping(self):pass
        def heartbeat_cpu_worker(self,*args):return True
        def runtime_operations(self,*args):return state['registered']
    class Executor:
        operations={'asset.verify':object()}
        def abort_active(self):state['aborts']+=1
    class Transport:
        executor=Executor()
        def run(self,stop):
            state['started']=True
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM,None)
            assert stop.is_set()
    monkeypatch.setattr(service,'build_worker',lambda config:(Database(),Transport()))
    config=WorkerConfig.from_env(environment(tmp_path))
    with pytest.raises(WorkerConfigurationError,match='registration'):service.run_worker(config)
    assert not state['started']
    previous=signal.getsignal(signal.SIGTERM)
    state['registered']=frozenset({'asset.verify'})
    service.run_worker(config)
    assert state['started'] and state['aborts']>=1 and signal.getsignal(signal.SIGTERM)==previous


def test_worker_icc_registry_is_explicit_and_validated(tmp_path):
    from pathlib import Path
    from photo_worker.operations.asset.adapters import asset_operations
    root=Path(__file__).resolve().parents[2]/'resources/icc'
    env=environment(tmp_path)
    assert WorkerConfig.from_env(env).icc_root is None
    config=WorkerConfig.from_env({**env,'PHOTO_ICC_ROOT':str(root)})
    assert config.icc_root==root
    assert 'asset.decode' in {op.name for op in asset_operations(None,config.icc_root)}
    assert 'asset.decode' not in {op.name for op in asset_operations(None)}
    with pytest.raises(WorkerConfigurationError,match='PHOTO_ICC_ROOT'):
        WorkerConfig.from_env({**env,'PHOTO_ICC_ROOT':str(tmp_path)})


def test_gpu_backend_is_explicit_and_bound_to_executor(monkeypatch,tmp_path):
    import ssl
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.runtime.executor import OperationFailure
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')}
    config=WorkerConfig.from_env(env)
    assert config.backend=='mps' and config.resource_class=='gpu'
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_WORKER_BACKEND':'auto'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,transport=service.build_worker(config)
    assert set(transport.executor.operations)=={'pair.fit.color'}
    wrong=SimpleNamespace(lease=SimpleNamespace(execution_profile={'backend':'cpu'}))
    with pytest.raises(OperationFailure):transport.executor.operations['pair.fit.color'].execute(wrong)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_installed_render_operation_uses_worker_database_admission(monkeypatch,tmp_path,backend):
    import ssl
    from contextlib import nullcontext
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.v2 import render_task
    from photo_worker.runtime.artifacts import TaskArtifacts
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu',
        'PHOTO_WORKER_BACKEND':backend,**metal_policy(backend)})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    verified=[]
    renderer=SimpleNamespace(backend=backend,verify_runtime=lambda:verified.append(True))
    database,transport=service.build_worker(config,render_renderer=renderer)
    assert verified==[True]
    assert set(transport.executor.operations)=={'pair.fit.color','render.compose'}
    actual_artifacts=object();calls=[]
    def create(ctx,artifact_config,roles):
        assert artifact_config.max_scratch_bytes==config.max_scratch_bytes
        assert roles==frozenset({'document','working_image','pixels','alpha','mask_pixels','mask','report'})
        return nullcontext(actual_artifacts)
    monkeypatch.setattr(TaskArtifacts,'create',create)
    def execute(ctx,artifacts,admission,installed_renderer):
        assert isinstance(admission,render_task.SQLRenderTaskAdmission)
        assert admission.database is database and installed_renderer is renderer
        assert artifacts is actual_artifacts
        calls.append(ctx);return 'render-result'
    monkeypatch.setattr(render_task,'execute_render_task',execute)
    command=SimpleNamespace(operation='render.compose',operation_version='1',resource_class='gpu')
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command))
    assert transport.executor.operations['render.compose'].execute(ctx)=='render-result'
    assert calls==[ctx]


@pytest.mark.parametrize('worker_class,backend,renderer_backend',[
    ('cpu','cpu','cpu'),('media','cpu','cpu'),('gpu','cuda','mps'),('gpu','cuda',None),
])
def test_render_installation_mismatch_refuses_before_tls(monkeypatch,tmp_path,worker_class,backend,renderer_backend):
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':worker_class,
        'PHOTO_WORKER_BACKEND':backend,**metal_policy(backend)})
    def forbidden(*args):raise AssertionError('TLS IO before Render configuration validation')
    monkeypatch.setattr(service,'client_tls',forbidden)
    with pytest.raises(WorkerConfigurationError,match='Render'):
        service.build_worker(config,render_renderer=SimpleNamespace(backend=renderer_backend))


def test_run_worker_forwards_render_installation_and_keeps_registration_gate(monkeypatch,tmp_path):
    monkeypatch.setattr('photo_worker.runtime.service._install_cuda_budget',lambda config:None)
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    renderer=object();calls=[]
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu',
        'PHOTO_WORKER_BACKEND':'cuda'})
    database=SimpleNamespace(ping=lambda:None,accelerator_operations=lambda *args:frozenset({'render.compose'}))
    transport=SimpleNamespace(executor=SimpleNamespace(operations={'pair.fit.color':object()}))
    def build(actual_config,*,render_renderer):
        assert actual_config is config and render_renderer is renderer
        calls.append(True);return database,transport
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(WorkerConfigurationError,match='registration'):
        service.run_worker(config,render_renderer=renderer)
    assert calls==[True]


def test_model_runtime_configuration_is_all_or_nothing(tmp_path):
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cpu'}
    with pytest.raises(WorkerConfigurationError,match='must be set together'):
        WorkerConfig.from_env({**env,'PHOTO_MODEL_ROOT':str(tmp_path)})
    lock=tmp_path/'lock.json';lock.write_text('{}')
    with pytest.raises(WorkerConfigurationError,match='PHOTO_MODEL'):
        WorkerConfig.from_env({**env,'PHOTO_MODEL_ROOT':str(tmp_path),'PHOTO_MODEL_LOCK':str(lock)})


def test_gpu_startup_uses_backend_preflight_and_capacity(monkeypatch,tmp_path):
    import signal
    import photo_worker.runtime.service as service
    from photo_worker.runtime.capacity import Capacity
    calls=[]
    class Database:
        def ping(self):pass
        def capacity_policy_timing_ready(self,*args):return True
        def accelerator_operations(self,worker,backend):
            calls.append(('preflight',backend));return frozenset({'pair.fit.color'})
        def heartbeat_policy_worker(self,worker,resource_class,backend,ops,ram,vram,scratch,policy_sha):
            assert len(policy_sha)==64
            return self.heartbeat_accelerator_worker(worker,backend,ops,ram,vram,scratch)
        def heartbeat_accelerator_worker(self,worker,backend,ops,ram,vram,scratch):
            calls.append(('capacity',backend,ram,vram,scratch));return True
    class Executor:
        operations={'pair.fit.color':object()}
        def abort_active(self):pass
    class Transport:
        executor=Executor()
        def run(self,stop):signal.getsignal(signal.SIGTERM)(signal.SIGTERM,None)
    monkeypatch.setattr(service,'build_worker',lambda config:(Database(),Transport()))
    monkeypatch.setattr(service,'measure_accelerator',lambda root,backend:Capacity(100,200,300))
    monkeypatch.setattr(service,'_install_metal_budget',lambda config:None)
    monkeypatch.setattr(service,'_capacity_sample',lambda config,sample,**kwargs:sample)
    from photo_worker.runtime import thread_budget
    from contextlib import nullcontext
    monkeypatch.setattr(thread_budget,'worker_thread_budget',lambda threads:nullcontext())
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')})
    service.run_worker(config)
    assert calls==[('preflight','mps'),('capacity','mps',100,300,200)]


def test_worker_lens_registry_config_is_paired_and_cpu_only(tmp_path):
    from hashlib import sha256
    from importlib import metadata
    import platform,sys
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.operations.optical.registry import LensRegistry
    from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,installed_component_hashes
    env={**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'}
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**env,'PHOTO_LENS_REGISTRY':'missing.json'})
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    raw=canonical_bytes(LensRegistry(schema_version=1,runtime=runtime,profiles=[]))
    path=tmp_path/'lens-registry.json';path.write_bytes(raw)
    configured={**env,'PHOTO_LENS_REGISTRY':str(path),'PHOTO_LENS_REGISTRY_SHA256':sha256(raw).hexdigest()}
    assert WorkerConfig.from_env(configured).lens_registry.profiles==[]
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**configured,'PHOTO_WORKER_CLASS':'media'})
    with pytest.raises(WorkerConfigurationError):WorkerConfig.from_env({**configured,'PHOTO_LENS_REGISTRY_SHA256':'a'*64})


def test_installed_region_operation_uses_worker_database_admission(monkeypatch,tmp_path):
    import ssl
    from contextlib import nullcontext
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.operations.region import adapter,admission
    from photo_worker.runtime.artifacts import TaskArtifacts
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cuda'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    verified=[]
    renderer=SimpleNamespace(backend='cuda',runtime_sha256='a'*64,renderer_version='v2.2',model_lock_sha='b'*64,icc_lock_sha='c'*64,icc_root=tmp_path,supported_modules=frozenset({'color'}),admit_resources=lambda *args:None,render=lambda *args:None,verify_runtime=lambda:verified.append(True))
    database,transport=service.build_worker(config,region_renderer=renderer)
    assert verified==[True]
    assert set(transport.executor.operations)=={'pair.fit.color','region.preview'}
    artifacts=object()
    def create(ctx,config,roles):
        assert roles==frozenset({'derivative','document'})
        return nullcontext(artifacts)
    monkeypatch.setattr(TaskArtifacts,'create',create)
    def execute(ctx,actual,installed_admission,installed_renderer):
        assert actual is artifacts and installed_renderer is renderer
        assert isinstance(installed_admission,admission.SQLRegionAdmission)
        assert installed_admission.database is database
        return 'region-result'
    monkeypatch.setattr(adapter,'execute',execute)
    assert transport.executor.operations['region.preview'].execute(object())=='region-result'


@pytest.mark.parametrize('worker_class,backend',[('cpu','cpu'),('media','cpu'),('gpu','cpu')])
def test_region_installation_requires_cuda_before_tls(monkeypatch,tmp_path,worker_class,backend):
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':worker_class,'PHOTO_WORKER_BACKEND':backend,**metal_policy(backend)})
    def forbidden(*args):raise AssertionError('TLS before Region configuration validation')
    monkeypatch.setattr(service,'client_tls',forbidden)
    with pytest.raises(WorkerConfigurationError,match='Region'):
        service.build_worker(config,region_renderer=SimpleNamespace(backend='cuda'))


def test_run_worker_forwards_region_installation_and_checks_registration(monkeypatch,tmp_path):
    monkeypatch.setattr('photo_worker.runtime.service._install_cuda_budget',lambda config:None)
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cuda'})
    renderer=object();calls=[]
    database=SimpleNamespace(ping=lambda:None,accelerator_operations=lambda *args:frozenset({'region.preview'}))
    transport=SimpleNamespace(executor=SimpleNamespace(operations={'pair.fit.color':object()}))
    def build(actual_config,*,region_renderer):
        assert actual_config is config and region_renderer is renderer
        calls.append(True);return database,transport
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(WorkerConfigurationError,match='registration'):
        service.run_worker(config,region_renderer=renderer)
    assert calls==[True]


@pytest.mark.parametrize("pinned_region", [False, True])
def test_render_and_region_install_together_with_one_admission_database(monkeypatch,tmp_path,pinned_region):
    import ssl
    from contextlib import nullcontext
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.v2 import render_task
    from photo_worker.operations.region import adapter,admission
    from photo_worker.runtime.artifacts import TaskArtifacts
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cuda'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    verified=[]
    render=SimpleNamespace(backend='cuda',verify_runtime=lambda:verified.append('render'))
    region=SimpleNamespace(backend='cuda',runtime_sha256='a'*64,renderer_version='v2.2',model_lock_sha='b'*64,icc_lock_sha='c'*64,icc_root=tmp_path,supported_modules=frozenset({'color'}),admit_resources=lambda *args:None,render=lambda *args:None,verify_runtime=lambda:verified.append('region'))
    if pinned_region:
        from photo_worker.operations.region.renderer import PinnedRegionRenderer
        from photo_worker.contracts.base import canonical_bytes,canonical_hash
        from test_region_runtime_lock import lock
        manifest=lock();path=tmp_path/'region-runtime.json';path.write_bytes(canonical_bytes(manifest))
        region.runtime_sha256=canonical_hash(manifest)
        region=PinnedRegionRenderer(region,path,region.runtime_sha256,
            render_pipeline=lambda *args:None,max_input_bytes=100000)
        # Constructor verification is independent of build-time registration.
        assert verified==['region']
        verified.clear()
    database,transport=service.build_worker(config,render_renderer=render,region_renderer=region)
    assert verified==['render','region']
    assert set(transport.executor.operations)=={'pair.fit.color','render.compose','region.preview'}
    roles_seen=[]
    def create(ctx,actual_config,roles):
        assert actual_config.max_scratch_bytes==config.max_scratch_bytes
        roles_seen.append(roles)
        return nullcontext(object())
    monkeypatch.setattr(TaskArtifacts,'create',create)
    def render_execute(ctx,artifacts,actual_admission,renderer):
        assert isinstance(actual_admission,render_task.SQLRenderTaskAdmission)
        assert actual_admission.database is database and renderer is render
        return 'render'
    def region_execute(ctx,artifacts,actual_admission,renderer):
        assert isinstance(actual_admission,admission.SQLRegionAdmission)
        assert actual_admission.database is database and renderer is region
        return 'region'
    monkeypatch.setattr(render_task,'execute_render_task',render_execute)
    monkeypatch.setattr(adapter,'execute',region_execute)
    command=SimpleNamespace(operation='render.compose',operation_version='1',resource_class='gpu')
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command))
    assert transport.executor.operations['render.compose'].execute(ctx)=='render'
    assert transport.executor.operations['region.preview'].execute(ctx)=='region'
    assert roles_seen==[frozenset({'document','working_image','pixels','alpha','mask_pixels','mask','report'}),frozenset({'derivative','document'})]


def test_run_worker_advertises_both_installed_renderers(monkeypatch,tmp_path):
    monkeypatch.setattr('photo_worker.runtime.service._install_cuda_budget',lambda config:None)
    from threading import Event
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'cuda'})
    render,region=object(),object();events=[]
    installed={'pair.fit.color':object(),'render.compose':object(),'region.preview':object()}
    database=SimpleNamespace(ping=lambda:events.append('ping'),
        accelerator_operations=lambda *args:frozenset({'render.compose','region.preview'}),
        heartbeat_accelerator_worker=lambda worker,backend,operations,ram,vram,scratch:events.append((backend,operations,ram,vram,scratch)))
    executor=SimpleNamespace(operations=installed,abort_active=lambda:events.append('abort'))
    transport=SimpleNamespace(executor=executor,run=lambda stop:events.append('run'))
    def build(actual_config,*,render_renderer,region_renderer):
        assert actual_config is config and render_renderer is render and region_renderer is region
        return database,transport
    monkeypatch.setattr(service,'build_worker',build)
    monkeypatch.setattr(service,'measure_accelerator',lambda *args:SimpleNamespace(ram_bytes=1,vram_bytes=2,scratch_bytes=3))
    monkeypatch.setattr(service,'_capacity_sample',lambda config,sample,**kwargs:sample)
    from photo_worker.runtime import thread_budget
    from contextlib import nullcontext
    monkeypatch.setattr(thread_budget,'worker_thread_budget',lambda threads:nullcontext())
    class Reporter:
        def __init__(self,report,measure,abort,**kwargs):self.report=report;self.measure=measure;self.failed=Event()
        def report_once(self):self.report(self.measure())
        def run(self,stop):raise AssertionError('unexpected background execution in controlled test')
    class Thread:
        ident=None
        def __init__(self,**kwargs):pass
        def start(self):events.append('capacity-thread')
        def is_alive(self):return False
    monkeypatch.setattr(service,'CapacityReporter',Reporter)
    monkeypatch.setattr(service,'Thread',Thread)
    service.run_worker(config,render_renderer=render,region_renderer=region)
    assert events==['ping',('cuda',frozenset(installed),1,2,3),'capacity-thread','run','abort']


def test_semantic_operation_installed_only_with_explicit_cuda_factory(monkeypatch,tmp_path):
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    base=WorkerConfig.from_env(environment(tmp_path))
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    cuda=replace(base,resource_class='gpu',backend='cuda')
    _,baseline=service.build_worker(cuda)
    assert 'semantic.transfer' not in baseline.executor.operations
    _,installed=service.build_worker(cuda,semantic_installation_factory=lambda ctx:None)
    assert installed.executor.operations['semantic.transfer'].resource_class=='gpu'
    _,installed=service.build_worker(cuda,semantic_artifact_installation_factory=lambda ctx,artifacts:None)
    assert installed.executor.operations['semantic.transfer'].resource_class=='gpu'
    with pytest.raises(WorkerConfigurationError,match='ambiguous'):
        service.build_worker(cuda,semantic_installation_factory=lambda ctx:None,
            semantic_artifact_installation_factory=lambda ctx,artifacts:None)
    for config,factory in [(base,lambda ctx:None),(cuda,object())]:
        with pytest.raises(WorkerConfigurationError,match='Semantic installation'):
            service.build_worker(config,semantic_installation_factory=factory)
        with pytest.raises(WorkerConfigurationError,match='Semantic installation'):
            service.build_worker(config,semantic_artifact_installation_factory=factory)


def test_semantic_assembly_installed_only_with_cpu_authority(monkeypatch,tmp_path):
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    cpu=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,baseline=service.build_worker(cpu)
    assert 'v2.semantic.assemble' not in baseline.executor.operations
    factory=lambda ctx,artifacts:None
    _,installed=service.build_worker(cpu,semantic_assembly_installation_factory=factory)
    assert installed.executor.operations['v2.semantic.assemble'].resource_class=='cpu'
    for config,owner in [(replace(cpu,resource_class='gpu',backend='cuda'),factory),(replace(cpu,resource_class='media'),factory),(cpu,object())]:
        with pytest.raises(WorkerConfigurationError,match='assembly installation'):
            service.build_worker(config,semantic_assembly_installation_factory=owner)


def test_local_exposure_installed_only_with_explicit_cpu_authority(monkeypatch,tmp_path):
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    cpu=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,baseline=service.build_worker(cpu)
    assert 'semantic.local_exposure' not in baseline.executor.operations
    factory=lambda ctx,artifacts:None
    _,installed=service.build_worker(cpu,local_exposure_installation_factory=factory)
    operation=installed.executor.operations['semantic.local_exposure']
    assert operation.resource_class=='cpu'
    for config,owner in [(replace(cpu,resource_class='gpu',backend='cuda'),factory),
            (replace(cpu,resource_class='media'),factory),(replace(cpu,backend='mps'),factory),(cpu,object())]:
        with pytest.raises(WorkerConfigurationError,match='Local exposure installation'):
            service.build_worker(config,local_exposure_installation_factory=owner)


def test_run_worker_forwards_local_exposure_installation(monkeypatch,tmp_path):
    import photo_worker.runtime.service as service
    cpu=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    factory=lambda ctx,artifacts:None
    class Captured(Exception):pass
    def build(config,**kwargs):
        assert config is cpu
        assert kwargs=={'local_exposure_installation_factory':factory}
        raise Captured()
    monkeypatch.setattr(service,'build_worker',build)
    with pytest.raises(Captured):
        service.run_worker(cpu,local_exposure_installation_factory=factory)


def test_negative_service_installs_concrete_icc_verifier(monkeypatch,tmp_path):
    import ssl
    from dataclasses import replace
    from pathlib import Path
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    from photo_worker.operations.optical import negative_adapter
    from photo_worker.runtime.executor import Operation
    cpu=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'cpu'})
    cpu=replace(cpu,icc_root=Path(__file__).resolve().parents[2]/'resources/icc')
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    calls=[]
    def install(config,database,objects,**kwargs):
        assert callable(kwargs['verify_color'])
        calls.append(kwargs['bucket'])
        return [Operation('optical.negative','cpu',lambda ctx:None)]
    monkeypatch.setattr(negative_adapter,'negative_operations',install)
    installation=negative_adapter.NegativeInstallation(object(),'qualified',None)
    _,baseline=service.build_worker(cpu)
    assert 'optical.negative' not in baseline.executor.operations
    _,installed=service.build_worker(cpu,negative_installation=installation)
    assert installed.executor.operations['optical.negative'].resource_class=='cpu'
    assert calls==['qualified']
    for config,owner in [(replace(cpu,resource_class='gpu'),installation),
            (replace(cpu,icc_root=None),installation),(cpu,object())]:
        with pytest.raises(WorkerConfigurationError):
            service.build_worker(config,negative_installation=owner)
    assert calls==['qualified']


def test_mps_configuration_requires_explicit_capacity_policy(tmp_path):
    env = {**environment(tmp_path), 'PHOTO_WORKER_CLASS':'gpu', 'PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')}
    env.pop('PHOTO_CAPACITY_POLICY')
    with pytest.raises(WorkerConfigurationError, match='PHOTO_CAPACITY_POLICY required for MPS'):
        WorkerConfig.from_env(env)


def test_mps_verification_policy_preserves_measured_device_capacity(tmp_path):
    config = WorkerConfig.from_env({**environment(tmp_path), 'PHOTO_WORKER_CLASS':'gpu', 'PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')})
    assert config.capacity_policy.gpu_budget_bytes == 2**53 - 1


@pytest.mark.parametrize('resource,backend,root,lock,callable_factory',[
    ('cpu','cpu',True,True,True),('gpu','metal',True,True,True),
    ('gpu','cuda',False,True,True),('gpu','mps',True,False,True),
    ('gpu','cpu',True,True,False),
])
def test_background_cleanup_preview_registration_refuses_invalid_configuration_before_tls(
        monkeypatch,tmp_path,resource,backend,root,lock,callable_factory):
    from dataclasses import replace
    import photo_worker.runtime.service as service
    base=WorkerConfig.from_env(environment(tmp_path))
    config=replace(base,resource_class=resource,backend=backend,
        model_root=tmp_path if root else None,model_lock=tmp_path/'lock.json' if lock else None)
    def unexpected(*args,**kwargs):raise AssertionError('TLS/DB accessed before configuration refusal')
    monkeypatch.setattr(service,'client_tls',unexpected)
    monkeypatch.setattr(service,'TaskDatabase',unexpected)
    factory=(lambda ctx,artifacts:None) if callable_factory else object()
    with pytest.raises(WorkerConfigurationError,match='Background cleanup preview requires matching installed model worker'):
        service.build_worker(config,background_cleanup_preview_installation_factory=factory)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
@pytest.mark.parametrize('mode',['baseline','installed','missing_model'])
def test_background_cleanup_preview_build_worker_with_real_model_pool_registration(monkeypatch,tmp_path,backend,mode):
    import json
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.models.pool import ModelPool
    entry={'id':'big_lama','architecture':'test-lock-only','weight_id':'fixture',
        'repository':'https://download.pytorch.org/models/','revision':'fixture1',
        'files':[{'path':'weights.bin','bytes':1,'sha256':'a'*64,
            'url':'https://download.pytorch.org/models/fixture.bin'}],
        'preprocessing_id':'test','packages_lock_hash':'b'*64,'code_license':'CC0-1.0',
        'weights_license':'CC0-1.0','notices':[],'redistribution_policy':'allowed',
        'approved_for_install':False,'backends':{value:{'status':'untested',
            'reason':'registration-only fixture','report_sha256':None} for value in ('cpu','cuda','mps')}}
    payload={'schema_version':1,'models':[] if mode=='missing_model' else [entry]}
    lock=tmp_path/'registration-lock.json'
    lock.write_text(json.dumps({**payload,'lock_id':canonical_hash(payload)}))
    config=replace(WorkerConfig.from_env(environment(tmp_path)),resource_class='gpu',backend=backend,
        model_root=tmp_path,model_lock=lock)
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    def unexpected(*args,**kwargs):raise AssertionError('startup must not load model or execute factory')
    monkeypatch.setattr(ModelPool,'acquire',unexpected)
    options={} if mode=='baseline' else {'background_cleanup_preview_installation_factory':unexpected}
    if mode=='missing_model':
        with pytest.raises(WorkerConfigurationError,match='Background cleanup preview requires big_lama model'):
            service.build_worker(config,**options)
        return
    if mode=='installed':
        # Explicit registration cannot turn untested model metadata into qualification.
        with pytest.raises(WorkerConfigurationError,match='qualified'):
            service.build_worker(config,**options)
        return
    _,transport=service.build_worker(config,**options)
    operation=transport.executor.operations.get('semantic.preview_background_cleanup')
    if mode=='baseline':assert operation is None
    else:
        assert operation is not None and operation.resource_class=='gpu' and operation.version=='1'
    # Registration does not fabricate passed backend evidence or residency.
    assert transport.executor.model_residency_snapshot()==()



def test_background_cleanup_copy_is_explicit_cpu_worker_installation(monkeypatch,tmp_path):
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    base=WorkerConfig.from_env(environment(tmp_path))
    cpu=replace(base,resource_class='cpu',backend='cpu')
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    def unexpected(*args,**kwargs):raise AssertionError('startup must not execute copy factory')
    _,baseline=service.build_worker(cpu)
    assert 'semantic.adopt_shown_background_cleanup' not in baseline.executor.operations
    _,installed=service.build_worker(cpu,shown_background_cleanup_installation_factory=unexpected)
    operation=installed.executor.operations['semantic.adopt_shown_background_cleanup']
    assert operation.resource_class=='cpu' and operation.version=='1'
    for config,factory in [(replace(cpu,backend='mps'),unexpected),
            (replace(cpu,resource_class='gpu',backend='cuda'),unexpected),(cpu,object())]:
        with pytest.raises(WorkerConfigurationError,match='Shown background cleanup installation requires CPU worker'):
            service.build_worker(config,shown_background_cleanup_installation_factory=factory)


@pytest.mark.parametrize('providers', ['installations', 'factories', 'both'])
def test_metal_build_omits_unsupported_restoration_without_losing_other_operations(monkeypatch, tmp_path, providers):
    import ssl
    import photo_worker.runtime.service as service
    config = WorkerConfig.from_env({**environment(tmp_path),
        'PHOTO_WORKER_CLASS': 'gpu', 'PHOTO_WORKER_BACKEND': 'mps', **metal_policy('mps')})
    monkeypatch.setattr(service, 'client_tls', lambda *args: ssl.create_default_context())
    def unsupported_provider(*args):
        pytest.fail('Metal must not instantiate unsupported restoration provider')
    options = {}
    if providers in ('installations', 'both'):
        options.update(glare_installation=object(), reflection_installation=object())
    if providers in ('factories', 'both'):
        options.update(glare_admission_factory=unsupported_provider,
                       reflection_admission_factory=unsupported_provider)
    _, transport = service.build_worker(config, **options)
    operations = set(transport.executor.operations)
    assert 'portrait.glare' not in operations
    assert 'image.reflection' not in operations
    assert operations == {'pair.fit.color'}


@pytest.mark.parametrize('provider',['region_renderer','semantic_installation_factory','semantic_artifact_installation_factory','learning_evaluation_installation'])
def test_metal_omits_cuda_only_installations_before_validation_and_keeps_base_operations(monkeypatch,tmp_path,provider):
    import ssl
    from types import SimpleNamespace
    import photo_worker.runtime.service as service
    config=WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')})
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    _,transport=service.build_worker(config,**{provider:SimpleNamespace(backend='cuda') if provider=='region_renderer' else object()})
    assert set(transport.executor.operations)=={'pair.fit.color'}


@pytest.mark.parametrize('metal_status,expected',[('unsupported',False),('untested',True)])
def test_model_backed_operations_follow_explicit_metal_support_before_loading(monkeypatch,tmp_path,metal_status,expected):
    import json
    import ssl
    from dataclasses import replace
    import photo_worker.runtime.service as service
    from photo_worker.contracts.base import canonical_hash
    from photo_worker.models.pool import ModelPool
    models=[]
    for model in ['keypointrcnn_resnet50_fpn','sfd','fan2d','bisenet_face19','mobilesam','scunet_color_real_psnr','realesrgan_x2','big_lama','sd15_inpaint']:
        models.append({'id':model,'architecture':'registration-only','weight_id':'fixture',
            'repository':'https://download.pytorch.org/models/','revision':'fixture1',
            'files':[{'path':'weights.bin','bytes':1,'sha256':'a'*64,'url':'https://download.pytorch.org/models/fixture.bin'}],
            'preprocessing_id':'fixture','packages_lock_hash':'b'*64,'code_license':'CC0-1.0','weights_license':'CC0-1.0',
            'notices':[],'redistribution_policy':'allowed','approved_for_install':False,
            'backends':{backend:{'status':metal_status if backend=='mps' else 'untested','reason':'registration fixture','report_sha256':None} for backend in ['cpu','cuda','mps']}})
    payload={'schema_version':1,'models':models};lock=tmp_path/'metal-registration-lock.json'
    lock.write_text(json.dumps({**payload,'lock_id':canonical_hash(payload)}))
    config=replace(WorkerConfig.from_env({**environment(tmp_path),'PHOTO_WORKER_CLASS':'gpu','PHOTO_WORKER_BACKEND':'mps',**metal_policy('mps')}),model_root=tmp_path,model_lock=lock)
    monkeypatch.setattr(service,'client_tls',lambda *args:ssl.create_default_context())
    monkeypatch.setattr(ModelPool,'acquire',lambda *args,**kwargs:pytest.fail('registration loaded a model'))
    _,transport=service.build_worker(config)
    for operation in ['pose.detect','face.detect','face.landmarks','face.parse','mask.prompt','image.denoise','image.upscale','image.inpaint','image.generate','image.outpaint']:
        assert (operation in transport.executor.operations)==expected
    assert 'pair.fit.color' in transport.executor.operations
    assert transport.executor.model_residency_snapshot()==()
