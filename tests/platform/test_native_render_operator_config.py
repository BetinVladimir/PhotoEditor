"""Operator files/calibration tests; these do not qualify rendering hardware."""
from hashlib import sha256
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.native_render_config import _document, _calibration, native_render_from_environment
from photo_worker.v2.render_qualification_pin import RenderQualificationPin


def pin():
    return RenderQualificationPin('a'*64,'b'*64,'cpu','c'*64,'d'*64,'e'*64)


def calibration():
    p=pin()
    return {'qualification':{k:v for k,v in p.__dict__.items() if k!='calibration_sha'},
        'evidence_sha':'f'*64,'icc_lock_sha':'0'*64,'tile_size':512,'alpha_mode':'require_float32',
        'samples':[{'width':32,'height':64,'provider_peak':{'ram_bytes':100,'scratch_bytes':10,'vram_bytes':0,'output_bytes':11}}]}


def test_calibration_preserves_full_qualification_and_cost_envelope():
    assert _calibration(calibration(),pin(),'0'*64)=={'ram_bytes':100,'scratch_bytes':12,'vram_bytes':0,'output_bytes':14}


@pytest.mark.parametrize('field,value',[('icc_lock_sha','1'*64),('tile_size',True),('alpha_mode','discard'),('evidence_sha','')])
def test_calibration_refuses_altered_identity(field,value):
    value_doc=calibration();value_doc[field]=value
    with pytest.raises(ValueError):_calibration(value_doc,pin(),'0'*64)


def test_calibration_refuses_foreign_runtime_and_duplicate_sample():
    value=calibration();value['qualification']['runtime_lock_sha']='1'*64
    with pytest.raises(ValueError):_calibration(value,pin(),'0'*64)
    value=calibration();value['samples'].append(value['samples'][0].copy())
    with pytest.raises(ValueError):_calibration(value,pin(),'0'*64)


def test_pinned_document_physical_hash_regular_file_and_canonical_json(tmp_path):
    path=tmp_path/'installation.json';raw=canonical_bytes({'schema_version':1});path.write_bytes(raw)
    descriptor={'path':str(path),'sha256':sha256(raw).hexdigest()}
    assert _document(descriptor)=={'schema_version':1}
    path.write_bytes(raw+b' ')
    with pytest.raises(ValueError):_document(descriptor)
    link=tmp_path/'link.json';link.symlink_to(path)
    with pytest.raises(ValueError):_document({**descriptor,'path':str(link)})
    path.write_bytes(b'x'*((1<<20)+1))
    with pytest.raises(ValueError):_document(descriptor)


def test_disabled_installation_does_not_touch_files_database_or_restore_authority():
    assert native_render_from_environment(object(),getenv=lambda key,default=None:default) is None
    with pytest.raises(ValueError):native_render_from_environment(object(),getenv=lambda key,default=None:'yes')


def test_selected_peak_matches_go_cover_tie_and_integer_ceil():
    from photo_worker.runtime.native_render_config import _selected_peak
    value=calibration();sample=value['samples'][0]
    value['samples']=[
        {'width':64,'height':32,'provider_peak':{'ram_bytes':200,'scratch_bytes':7,'vram_bytes':0,'output_bytes':9}},
        sample,
    ]
    profile={'tile_size':512,'alpha_mode':'require_float32'}
    # Equal covering area: smaller width wins, without rotating the sample.
    assert _selected_peak(value,16,16,profile)=={'ram_bytes':100,'scratch_bytes':2,'vram_bytes':0,'output_bytes':2}
    assert _selected_peak(value,64,32,profile)=={'ram_bytes':200,'scratch_bytes':9,'vram_bytes':0,'output_bytes':11}
    with pytest.raises(ValueError):_selected_peak(value,65,32,profile)
    with pytest.raises(ValueError):_selected_peak(value,16,16,{**profile,'tile_size':256})
    with pytest.raises(ValueError):_selected_peak(value,16,16,{**profile,'alpha_mode':'normalize_uint16'})


def configured_files(tmp_path,backend='cpu',profile='gpu_render'):
    """Real canonical pins/files; SQL rows below are explicit unit-test doubles."""
    from importlib import metadata
    import platform
    from photo_worker.v2.render_runtime import RenderRuntimeLock,installed_component_hashes
    runtime=RenderRuntimeLock(schema_version=1,python_version=platform.python_version(),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    def write(name,value):
        raw=canonical_bytes(value);path=tmp_path/name;path.write_bytes(raw)
        return {'path':str(path),'sha256':sha256(raw).hexdigest()}
    runtime_ref=write('runtime.json',runtime)
    qualification=dict(model_lock_sha='a'*64,runtime_lock_sha=runtime_ref['sha256'],backend=backend,device_profile_sha='c'*64,dataset_sha='d'*64)
    profile_doc=calibration();profile_doc['qualification']=qualification
    calibration_ref=write('calibration.json',profile_doc)
    descriptor=dict(schema_version=1,profile=profile,tenant_id='11111111-1111-4111-8111-111111111111',project_id='22222222-2222-4222-8222-222222222222',qualification={**qualification,'calibration_sha':calibration_ref['sha256']},runtime=runtime_ref,calibration=calibration_ref,icc_lock_sha='0'*64,max_input_bytes=1<<30,max_mask_bytes=1<<28,max_tile_bytes=512*512*256,max_document_bytes=1<<20)
    descriptor_ref=write('installation.json',descriptor)
    env={'PHOTO_NATIVE_RENDER_ENABLED':'true','PHOTO_NATIVE_RENDER_CONFIG':descriptor_ref['path'],'PHOTO_NATIVE_RENDER_CONFIG_SHA':descriptor_ref['sha256']}
    return env,descriptor


@pytest.mark.parametrize('profile,resource,backend,operation',[
    ('gpu_render','gpu','cpu','render.compose'),
    ('gpu_render','gpu','cuda','render.compose'),
    ('gpu_render','gpu','mps','render.compose'),
    ('cpu_precompose','cpu','cpu','editor.layers_precompose'),
])
def test_exact_profile_constructs_native_installation_and_checks_live_sql(tmp_path,monkeypatch,profile,resource,backend,operation):
    from contextlib import contextmanager
    from types import SimpleNamespace
    from uuid import UUID
    from photo_worker.v2.render_native_installation import NativeRenderInstallation
    env,descriptor=configured_files(tmp_path,backend,profile)
    calls=[];answer=[True]
    class Connection:
        def execute(self,query,args):calls.append((query,args));return SimpleNamespace(fetchone=lambda:(answer[0],))
    @contextmanager
    def connect(*args,**kwargs):yield Connection()
    # Explicit mocked SQL transport exercises loader wiring, not hardware/SQL qualification.
    monkeypatch.setattr('photo_worker.runtime.native_render_config.psycopg.connect',connect)
    monkeypatch.setattr('photo_worker.runtime.native_render_config.require_application_role',lambda connection,role:None)
    config=SimpleNamespace(resource_class=resource,backend=backend,worker_id=UUID(int=3),database_url='unit-test-no-database')
    installed=native_render_from_environment(config,getenv=lambda key,default=None:env.get(key,default))
    assert type(installed) is NativeRenderInstallation and installed.backend==backend
    assert len(calls)==1 and 'photo.native_render_installation_ready' in calls[0][0]
    assert calls[0][1][2:4]==(config.worker_id,operation)
    assert calls[0][1][4]==canonical_bytes(descriptor['qualification']).decode()
    # A withdrawn or nonliteral SQL result still stops this actual installation.
    answer[0]=False
    with pytest.raises(ValueError,match='QUALIFICATION_UNAVAILABLE'):installed.verify()
    answer[0]=1
    with pytest.raises(ValueError,match='QUALIFICATION_UNAVAILABLE'):installed.verify()


@pytest.mark.parametrize('profile,resource,backend,configured_backend',[
    ('cpu_precompose','cpu','cuda','cuda'),
    ('cpu_precompose','cpu','mps','mps'),
    ('gpu_render','cpu','cpu','cpu'),
    ('cpu_precompose','gpu','cpu','cpu'),
    ('gpu_render','gpu','cuda','cpu'),
    ('gpu_render','gpu','cpu','cuda'),
    ('unknown','gpu','cpu','cpu'),
])
def test_profile_class_backend_mismatch_refused_before_sql(tmp_path,monkeypatch,profile,resource,backend,configured_backend):
    from types import SimpleNamespace
    env,_=configured_files(tmp_path,configured_backend,profile)
    def forbidden(*a,**k):raise AssertionError('mismatch reached SQL')
    monkeypatch.setattr('photo_worker.runtime.native_render_config.psycopg.connect',forbidden)
    config=SimpleNamespace(resource_class=resource,backend=backend)
    with pytest.raises(ValueError,match='NATIVE_RENDER_(PROFILE_INVALID|WORKER_PROFILE_MISMATCH)'):
        native_render_from_environment(config,getenv=lambda key,default=None:env.get(key,default))
