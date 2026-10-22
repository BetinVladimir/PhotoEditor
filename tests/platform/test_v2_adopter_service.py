"""The adopter processes only V2 candidates and keeps invalid results fenced."""
from contextlib import nullcontext
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.service import Adopter, AdopterConfig


def role_connection():
    def execute(query, params=()):
        assert "current_user=" in query
        return SimpleNamespace(fetchone=lambda:(True,))
    return SimpleNamespace(autocommit=True,execute=execute)


def test_service_installs_render_replay_into_adopter(tmp_path, monkeypatch):
    from threading import Event
    from photo_worker.v2 import service
    config=AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret',
        layer_scratch_root=tmp_path,layer_scratch_bytes=4096)
    connections=[SimpleNamespace(execute=lambda query:Cursor([(True,)])),SimpleNamespace(execute=lambda query:Cursor([(True,)]))]
    def connect(*args,**kwargs):
        assert kwargs['autocommit'] is True
        assert 'role=photo_scheduler' in kwargs['options']
        return nullcontext(connections.pop(0))
    monkeypatch.setattr(service.psycopg,'connect',connect)
    objects=object()
    monkeypatch.setattr(service,'Minio',lambda *args,**kwargs:objects)
    installed=[]
    monkeypatch.setattr(service,'Adopter',lambda *args,**kwargs:installed.append((args,kwargs)))
    replay=lambda *args:None
    stopped=Event();stopped.set()
    service.run_adopter(config,stopped,render_replay=replay,render_max_tile_bytes=2048,region_target_icc_sha="a"*64)
    assert len(installed)==1 and not connections
    assert installed[0][0][2:] == (objects,'bucket')
    assert installed[0][1] == dict(layer_scratch_root=tmp_path,layer_scratch_bytes=4096,
        render_replay=replay,render_max_tile_bytes=2048,region_target_icc_sha="a"*64,region_replay=None,stop=stopped)


@pytest.mark.parametrize('replay,budget,scratch',[
    (None,2048,4096),(lambda:None,None,4096),(object(),2048,4096),
    (lambda:None,True,4096),(lambda:None,0,4096),(lambda:None,2**53,4096),
    (lambda:None,2048,None),(lambda:None,2048,True),
])
def test_service_invalid_render_runtime_fails_before_external_io(tmp_path,monkeypatch,replay,budget,scratch):
    from photo_worker.v2 import service
    config=AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret',
        layer_scratch_root=tmp_path,layer_scratch_bytes=scratch)
    def forbidden(*args,**kwargs):raise AssertionError('external IO before configuration validation')
    monkeypatch.setattr(service,'Minio',forbidden)
    monkeypatch.setattr(service.psycopg,'connect',forbidden)
    with pytest.raises(ValueError,match='Render'):
        service.run_adopter(config,render_replay=replay,render_max_tile_bytes=budget)


class Cursor:
    def __init__(self, rows):
        self.rows = rows

    def fetchall(self):
        return self.rows

    def fetchone(self):
        return self.rows[0]


class ReadConnection:
    autocommit = True

    def __init__(self, tenant, run, task):
        self.tenant, self.run, self.task = tenant, run, task
        self.unlocked = False

    def execute(self, query, params=()):
        if "pending_v29_negative_reviews" in query:
            return Cursor([])
        if "editor_effect_pending_seal_adoptions" in query:
            return Cursor([])
        if "v2_pending_adoptions" in query:
            return Cursor([(self.tenant, self.run, self.task)])
        if "pg_try_advisory_lock" in query:
            return Cursor([(True,)])
        if "pg_advisory_unlock" in query:
            self.unlocked = True
            return Cursor([(True,)])
        if "v2_pending_finalizations" in query:
            return Cursor([(self.tenant, self.run)])
        raise AssertionError(query)


class WriteConnection:
    def __init__(self, state="completed"):
        self.state = state
        self.rejected = False

    def transaction(self):
        return nullcontext()

    def execute(self, query, params):
        if "reject_v2_adoption" in query:
            self.rejected = True
            return Cursor([(True,)])
        if "finalize_v2_run" in query:
            return Cursor([(self.state,)])
        raise AssertionError(query)


def test_adopter_processes_candidate_and_finalizes():
    tenant, run, task = uuid4(), uuid4(), uuid4()
    read, write = ReadConnection(tenant, run, task), WriteConnection()
    adopter = Adopter(read, write, object(), "bucket")
    adopter.coordinator = SimpleNamespace(adopt=lambda *_: True)
    assert adopter.run_once() == {"adopted": 1, "rejected": 0, "finalized": 1, "retry": 0}
    assert read.unlocked


def test_invalid_result_is_rejected_then_failed():
    tenant, run, task = uuid4(), uuid4(), uuid4()
    read, write = ReadConnection(tenant, run, task), WriteConnection("failed")
    adopter = Adopter(read, write, object(), "bucket")
    result = uuid4()

    def invalid(*_):
        raise AdoptionInvalid("ARTIFACT_SCHEMA_INVALID")

    adopter.coordinator = SimpleNamespace(
        adopt=invalid, context=lambda *_: SimpleNamespace(result_id=result))
    assert adopter.run_once() == {"adopted": 0, "rejected": 1, "finalized": 1, "retry": 0}
    assert write.rejected and read.unlocked


def test_adopter_config_requires_production_https():
    values = {"DATABASE_URL": "postgres://unused", "S3_ENDPOINT": "http://minio:9000",
              "S3_BUCKET": "bucket", "S3_ACCESS_KEY": "key", "S3_SECRET_KEY": "secret"}
    with pytest.raises(ValueError, match="endpoint"):
        AdopterConfig.from_env(values)
    values["APP_ENV"] = "development"
    values["S3_ENDPOINT"] = "http://127.0.0.1:9000"
    assert AdopterConfig.from_env(values).development


@pytest.mark.parametrize('code',['LAYER_ADOPTION_SCRATCH_REQUIRED','ADOPTION_SCRATCH_EXCEEDED',
    'RENDER_ADOPTION_REPLAY_REQUIRED','RENDER_ADOPTION_SCRATCH_REQUIRED','RENDER_ADOPTION_INPUTS_STALE','RENDER_REPLAY_QUALIFICATION_MISMATCH','REGION_ADOPTION_ICC_REQUIRED',
    'SEMANTIC_REPLAY_AUTHORITY_REQUIRED','SEMANTIC_ADOPTION_SCRATCH_REQUIRED','SEMANTIC_REPLAY_RAM_EXCEEDED'])
def test_scratch_configuration_failure_retries_without_rejecting_result(code):
    tenant,run,task=uuid4(),uuid4(),uuid4()
    read=ReadConnection(tenant,run,task);write=WriteConnection()
    adopter=Adopter(read,write,object(),'test')
    def fail(*args):raise AdoptionInvalid(code)
    adopter.coordinator=SimpleNamespace(adopt=fail)
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':1,'retry':1}
    assert not write.rejected and read.unlocked


def test_layer_scratch_configuration_is_paired_and_bounded(tmp_path):
    values={'DATABASE_URL':'postgresql://example','S3_ENDPOINT':'http://localhost:9000',
        'S3_BUCKET':'test','S3_ACCESS_KEY':'test','S3_SECRET_KEY':'test','APP_ENV':'development'}
    for budget in ('','0','-1','9007199254740992'):
        with pytest.raises(ValueError):AdopterConfig.from_env({**values,'PHOTO_ADOPTER_LAYER_SCRATCH_ROOT':str(tmp_path),'PHOTO_ADOPTER_LAYER_SCRATCH_BYTES':budget})
    config=AdopterConfig.from_env({**values,'PHOTO_ADOPTER_LAYER_SCRATCH_ROOT':str(tmp_path),'PHOTO_ADOPTER_LAYER_SCRATCH_BYTES':'1000000'})
    assert config.layer_scratch_root==tmp_path and config.layer_scratch_bytes==1000000


@pytest.mark.parametrize('pin',['', 'a'*63, 'A'*64, 'g'*64, 123, True])
def test_service_invalid_region_pin_fails_before_external_io(monkeypatch,pin):
    from photo_worker.v2 import service
    config=AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret')
    def forbidden(*args,**kwargs):raise AssertionError('external IO before Region validation')
    monkeypatch.setattr(service,'Minio',forbidden)
    monkeypatch.setattr(service.psycopg,'connect',forbidden)
    with pytest.raises(ValueError,match='Region'):
        service.run_adopter(config,region_target_icc_sha=pin)


def test_adopter_forwards_region_pin_to_coordinator():
    read=SimpleNamespace(autocommit=True)
    write=SimpleNamespace(autocommit=True)
    adopter=Adopter(read,write,object(),'bucket',region_target_icc_sha='a'*64)
    assert adopter.coordinator.region_target_icc_sha=='a'*64


def test_region_without_installed_icc_stops_before_object_reader(monkeypatch):
    from photo_worker.v2 import adoption_service
    coordinator=adoption_service.AdoptionCoordinator(SimpleNamespace(autocommit=True),SimpleNamespace(autocommit=True),object(),'bucket')
    monkeypatch.setattr(coordinator,'context',lambda *args:SimpleNamespace(operation='region.preview'))
    def forbidden(*args,**kwargs):raise AssertionError('object reader created without trusted ICC pin')
    monkeypatch.setattr(adoption_service,'PostgresObjectEvidenceReader',forbidden)
    with pytest.raises(AdoptionInvalid,match='REGION_ADOPTION_ICC_REQUIRED'):
        coordinator.adopt(uuid4(),uuid4())


def test_real_region_coordinator_missing_icc_retries_without_rejection(monkeypatch):
    tenant,run,task=uuid4(),uuid4(),uuid4()
    read=ReadConnection(tenant,run,task);write=WriteConnection()
    adopter=Adopter(read,write,object(),'bucket')
    monkeypatch.setattr(adopter.coordinator,'context',lambda *args:SimpleNamespace(operation='region.preview'))
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':1,'retry':1}
    assert not write.rejected and read.unlocked


def test_stopped_adopter_does_not_query_candidates():
    from threading import Event
    stop = Event(); stop.set()
    read = SimpleNamespace(autocommit=True, execute=lambda *args: pytest.fail('query after stop'))
    adopter = Adopter(read, object(), object(), 'bucket', stop=stop)
    assert adopter.run_once() == dict(adopted=0, rejected=0, finalized=0, retry=0)


@pytest.mark.parametrize('invalid', [False, True])
def test_stop_during_preparation_unlocks_without_rejecting_or_finalizing(invalid):
    from threading import Event
    tenant, run, task = uuid4(), uuid4(), uuid4()
    stop = Event()
    read, write = ReadConnection(tenant, run, task), WriteConnection()
    adopter = Adopter(read, write, object(), 'bucket', stop=stop)
    def prepare(*args):
        stop.set()
        if invalid:
            raise AdoptionInvalid('ARTIFACT_SCHEMA_INVALID')
        adopter.check_active()
    adopter.coordinator = SimpleNamespace(adopt=prepare)
    assert adopter.run_once() == dict(adopted=0, rejected=0, finalized=0, retry=1)
    assert read.unlocked and not write.rejected


def test_adopter_config_loads_target_pin_from_verified_installed_profiles():
    from pathlib import Path
    from photo_worker.color.icc import ICCRegistry
    root = Path(__file__).resolve().parents[2] / 'resources/icc'
    env = dict(DATABASE_URL='postgres://unused', S3_ENDPOINT='https://objects.example',
        S3_BUCKET='bucket', S3_ACCESS_KEY='key', S3_SECRET_KEY='secret', PHOTO_ICC_ROOT=str(root))
    config = AdopterConfig.from_env(env)
    assert config.region_target_icc_sha == ICCRegistry(root).resolve('srgb_v1').sha256
    assert config.derivative_work_icc_sha == ICCRegistry(root).resolve('work_rec2020_gamma24_v1').sha256


def test_derivative_receipt_loop_retries_and_wraps_cursor():
    tenant,run,task,other=uuid4(),uuid4(),uuid4(),uuid4()
    cursors=[]
    class DerivativeRead(ReadConnection):
        def execute(self,query,params=()):
            if 'v2_pending_' in query:return Cursor([])
            if 'pending_v26_render_derivative_receipts' in query:
                cursors.append(params[0])
                return Cursor([(tenant,run,'preview1600',task),(tenant,run,'preview2560',other)] if params[0] is None else [])
            return super().execute(query,params)
    read,write=DerivativeRead(tenant,run,task),WriteConnection()
    write.autocommit=True
    adopter=Adopter(read,write,object(),'bucket',region_target_icc_sha='a'*64,derivative_work_icc_sha='b'*64)
    assert adopter.derivative_coordinator is not None
    calls=[]
    def adopt(tenant_id,run_id,variant):
        calls.append((tenant_id,run_id,variant))
        if variant=='preview2560':raise AdoptionInvalid('RENDER_DERIVATIVE_COMMIT_STALE')
        return True
    adopter.derivative_coordinator=SimpleNamespace(adopt=adopt)
    assert adopter.run_once()==dict(adopted=1,rejected=0,finalized=0,retry=1)
    assert len(calls)==2 and read.unlocked and adopter.derivative_after==other
    assert adopter.run_once()==dict(adopted=0,rejected=0,finalized=0,retry=0)
    assert cursors==[None,other] and adopter.derivative_after is None


@pytest.mark.parametrize('kind', ['missing', 'relative', 'symlink', 'tampered'])
def test_adopter_config_rejects_invalid_installed_icc(tmp_path, kind):
    from pathlib import Path
    import shutil
    source = Path(__file__).resolve().parents[2] / 'resources/icc'
    root = tmp_path / 'icc'
    if kind == 'symlink':
        root.symlink_to(source, target_is_directory=True)
    elif kind == 'tampered':
        shutil.copytree(source, root)
        profile = next(root.glob('*.icc'))
        profile.write_bytes(b'tampered')
    elif kind == 'relative':
        root = Path('resources/icc')
    env = dict(DATABASE_URL='postgres://unused', S3_ENDPOINT='https://objects.example',
        S3_BUCKET='bucket', S3_ACCESS_KEY='key', S3_SECRET_KEY='secret', PHOTO_ICC_ROOT=str(root))
    with pytest.raises(ValueError, match='PHOTO_ICC_ROOT'):
        AdopterConfig.from_env(env)


def test_service_installs_configured_region_pin(monkeypatch):
    from threading import Event
    from photo_worker.v2 import service
    config = AdopterConfig('postgres://unused', 'https://objects.example', 'bucket',
        'key', 'secret', region_target_icc_sha='e' * 64)
    monkeypatch.setattr(service.psycopg, 'connect', lambda *args, **kwargs: nullcontext(role_connection()))
    monkeypatch.setattr(service, 'Minio', lambda *args, **kwargs: object())
    installed = []
    monkeypatch.setattr(service, 'Adopter', lambda *args, **kwargs: installed.append(kwargs))
    stop = Event(); stop.set()
    service.run_adopter(config, stop)
    assert installed[0]['region_target_icc_sha'] == config.region_target_icc_sha


def test_service_conflicting_region_pin_fails_before_external_io(monkeypatch):
    from photo_worker.v2 import service
    config = AdopterConfig('postgres://unused', 'https://objects.example', 'bucket',
        'key', 'secret', region_target_icc_sha='e' * 64)
    def forbidden(*args, **kwargs):
        pytest.fail('IO before ICC conflict validation')
    monkeypatch.setattr(service.psycopg, 'connect', forbidden)
    monkeypatch.setattr(service, 'Minio', forbidden)
    with pytest.raises(ValueError, match='conflicts'):
        service.run_adopter(config, region_target_icc_sha='f' * 64)


@pytest.mark.parametrize('failed_connection', [None, 0, 1])
def test_derivative_startup_checks_both_connections_before_install(monkeypatch, failed_connection):
    from threading import Event
    from photo_worker.v2 import service, render_derivative_schema
    config = AdopterConfig('postgres://unused', 'https://objects.example',
        'bucket', 'key', 'secret', region_target_icc_sha='a'*64,
        derivative_work_icc_sha='b'*64)
    connections = [role_connection(), role_connection()]
    pending = list(connections)
    monkeypatch.setattr(service.psycopg, 'connect',
        lambda *args, **kwargs: nullcontext(pending.pop(0)))
    monkeypatch.setattr(service, 'Minio', lambda *args, **kwargs: object())
    checked, installed = [], []
    def check(connection):
        checked.append(connection)
        if failed_connection is not None and connection is connections[failed_connection]:
            raise ValueError('schema unavailable')
    monkeypatch.setattr(render_derivative_schema, 'require_render_derivative_schema', check)
    monkeypatch.setattr(service, 'Adopter', lambda *args, **kwargs: installed.append(kwargs))
    stopped = Event(); stopped.set()
    if failed_connection is None:
        service.run_adopter(config, stopped)
        assert checked == connections and len(installed) == 1
        assert installed[0]['derivative_work_icc_sha'] == 'b'*64
    else:
        with pytest.raises(ValueError, match='schema unavailable'):
            service.run_adopter(config, stopped)
        assert installed == []
        assert checked == connections[:failed_connection+1]


def test_adopter_installs_semantic_plan_sql_authorities(monkeypatch):
    from types import SimpleNamespace
    from photo_worker.v2 import semantic_snapshot_authority,semantic_producer_authority
    calls=[];snapshot=lambda *args:None;provenance=lambda *args:None
    read=SimpleNamespace(autocommit=True);write=object();objects=object()
    def load(connection,*,check_active):
        assert connection is read;calls.append(check_active);return snapshot
    def producers(connection,store,bucket,*,check_active):
        assert connection is read and store is objects and bucket=='owned'
        calls.append(check_active);return provenance
    monkeypatch.setattr(semantic_snapshot_authority,'semantic_snapshot_authority',load)
    monkeypatch.setattr(semantic_producer_authority,'adopted_semantic_producer_authority',producers)
    adopter=Adopter(read,write,objects,'owned')
    assert adopter.coordinator.semantic_plan_snapshot is snapshot
    assert adopter.coordinator.semantic_plan_provenance is provenance
    assert calls==[adopter.check_active,adopter.check_active]


def test_adopter_forwards_explicit_semantic_transfer_authority():
    read=SimpleNamespace(autocommit=True)
    authority=lambda *args:None
    adopter=Adopter(read,object(),object(),'owned',semantic_authority=authority,
        semantic_ram_bytes=256*1024*1024)
    assert adopter.coordinator.semantic_authority is authority
    assert adopter.coordinator.semantic_ram_bytes==256*1024*1024


@pytest.mark.parametrize('kwargs',[{'semantic_authority':object()},{'semantic_ram_bytes':True},
    {'semantic_ram_bytes':0}])
def test_adopter_rejects_invalid_semantic_configuration(kwargs):
    with pytest.raises(ValueError):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',**kwargs)


def test_adopter_installs_composed_transfer_authority(monkeypatch):
    from photo_worker.v2 import semantic_transfer_authority
    calls=[];authority=lambda *args:None;scene=lambda *args:None
    read=SimpleNamespace(autocommit=True);objects=object()
    def install(connection,store,bucket,**kwargs):
        assert connection is read and store is objects and bucket=='owned'
        assert kwargs['working_icc_sha']=='a'*64 and kwargs['verify_scene'] is scene
        calls.append(kwargs['check_active']);return authority
    monkeypatch.setattr(semantic_transfer_authority,'sql_semantic_adoption_authority',install)
    adopter=Adopter(read,object(),objects,'owned',semantic_scene_verifier=scene,
        semantic_working_icc_sha='a'*64,semantic_ram_bytes=256*1024*1024)
    assert adopter.coordinator.semantic_authority is authority and calls==[adopter.check_active]


@pytest.mark.parametrize('kwargs',[{'semantic_working_icc_sha':'a'*64},
    {'semantic_scene_verifier':lambda *args:None},
    {'semantic_scene_verifier':lambda *args:None,'semantic_authority':lambda *args:None}])
def test_adopter_rejects_incomplete_or_conflicting_scene_installation(kwargs):
    with pytest.raises(ValueError):Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',**kwargs)


@pytest.mark.parametrize('scene,ram',[(None,1024),(lambda *args:None,None),(object(),1024),(lambda *args:None,True),(lambda *args:None,0)])
def test_service_invalid_semantic_installation_fails_before_io(monkeypatch,scene,ram):
    from photo_worker.v2 import service
    def forbidden(*args,**kwargs):raise AssertionError('External IO before admission')
    monkeypatch.setattr(service,'Minio',forbidden);monkeypatch.setattr(service.psycopg,'connect',forbidden)
    config=AdopterConfig('unused','https://objects.example','bucket','key','secret')
    with pytest.raises(ValueError):service.run_adopter(config,semantic_scene_verifier=scene,semantic_ram_bytes=ram)


def test_service_forwards_semantic_dependencies_from_trusted_icc_config(tmp_path,monkeypatch):
    from threading import Event
    from photo_worker.v2 import service,render_derivative_schema
    config=AdopterConfig('unused','https://objects.example','bucket','key','secret',layer_scratch_root=tmp_path,
        layer_scratch_bytes=33554432,region_target_icc_sha='a'*64,derivative_work_icc_sha='b'*64)
    monkeypatch.setattr(service.psycopg,'connect',lambda *args,**kwargs:nullcontext(role_connection()))
    monkeypatch.setattr(service,'Minio',lambda *args,**kwargs:object())
    monkeypatch.setattr(render_derivative_schema,'require_render_derivative_schema',lambda *args:None)
    installed=[];monkeypatch.setattr(service,'Adopter',lambda *args,**kwargs:installed.append(kwargs))
    scene=lambda *args:None;stop=Event();stop.set()
    service.run_adopter(config,stop,semantic_scene_verifier=scene,semantic_ram_bytes=268435456)
    assert installed[0]['semantic_scene_verifier'] is scene
    assert installed[0]['semantic_working_icc_sha']=='b'*64 and installed[0]['semantic_ram_bytes']==268435456


def test_adopter_installs_versioned_cpu_exposure_authority(tmp_path,monkeypatch):
    from photo_worker.v2 import service,exposure_installation
    from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
    installed=[];authority=lambda *args:None
    def factory(connection,objects,bucket,**kwargs):
        installed.append((connection,objects,bucket,kwargs))
        return authority
    monkeypatch.setattr(exposure_installation,'versioned_exposure_adoption_installation',factory)
    read=SimpleNamespace(autocommit=True);write=SimpleNamespace(autocommit=True);objects=object()
    adopter=service.Adopter(read,write,objects,'owned',layer_scratch_root=tmp_path,
        layer_scratch_bytes=128*1024**2,exposure_working_icc_sha=WORKING_PROFILE_SHA)
    assert adopter.coordinator.exposure_installation is authority
    assert installed[0][:3]==(read,objects,'owned')
    assert installed[0][3]['working_icc_sha']==WORKING_PROFILE_SHA
    assert installed[0][3]['check_active'].__self__ is adopter
    baseline=service.Adopter(read,write,objects,'owned')
    assert baseline.coordinator.exposure_installation is None


@pytest.mark.parametrize('sha,scratch,budget',[('a'*64,True,4096),(None,False,4096),
    (None,True,True),(None,True,0)])
def test_bad_exposure_installation_denied_before_io(tmp_path,sha,scratch,budget):
    from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
    with pytest.raises(ValueError,match='exposure adopter'):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',
            layer_scratch_root=tmp_path if scratch else None,layer_scratch_bytes=budget,
            exposure_working_icc_sha=sha or WORKING_PROFILE_SHA)


@pytest.mark.parametrize('variant',['valid','disabled','flag','icc','scratch'])
def test_exposure_adopter_environment_enablement(tmp_path,variant):
    from pathlib import Path
    values=dict(DATABASE_URL='postgres://unused',S3_ENDPOINT='https://objects.example',
        S3_BUCKET='bucket',S3_ACCESS_KEY='key',S3_SECRET_KEY='secret',
        PHOTO_ICC_ROOT=str(Path(__file__).resolve().parents[2]/'resources/icc'),
        PHOTO_ADOPTER_LAYER_SCRATCH_ROOT=str(tmp_path),PHOTO_ADOPTER_LAYER_SCRATCH_BYTES='134217728',
        PHOTO_ADOPTER_EXPOSURE_ENABLED='true')
    if variant=='disabled':values['PHOTO_ADOPTER_EXPOSURE_ENABLED']='false'
    elif variant=='flag':values['PHOTO_ADOPTER_EXPOSURE_ENABLED']='yes'
    elif variant=='icc':del values['PHOTO_ICC_ROOT']
    elif variant=='scratch':
        del values['PHOTO_ADOPTER_LAYER_SCRATCH_ROOT'];del values['PHOTO_ADOPTER_LAYER_SCRATCH_BYTES']
    if variant in ('flag','icc','scratch'):
        with pytest.raises(ValueError,match='[Ee]xposure'):
            AdopterConfig.from_env(values)
    else:
        assert AdopterConfig.from_env(values).exposure_enabled is (variant=='valid')


def test_run_adopter_forwards_enabled_exposure(tmp_path,monkeypatch):
    from threading import Event
    from photo_worker.v2 import service,render_derivative_schema
    from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
    from photo_worker.color.icc import ICCRegistry
    from pathlib import Path
    srgb=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1').sha256
    config=AdopterConfig('postgres://unused','https://objects.example','bucket','key','secret',
        layer_scratch_root=tmp_path,layer_scratch_bytes=128*1024**2,
        region_target_icc_sha=srgb,derivative_work_icc_sha=WORKING_PROFILE_SHA,exposure_enabled=True)
    monkeypatch.setattr(service.psycopg,'connect',lambda *a,**k:nullcontext(role_connection()))
    monkeypatch.setattr(service,'Minio',lambda *a,**k:object())
    monkeypatch.setattr(render_derivative_schema,'require_render_derivative_schema',lambda *a:None)
    calls=[]
    monkeypatch.setattr(service,'Adopter',lambda *a,**k:calls.append(k))
    stop=Event();stop.set();service.run_adopter(config,stop)
    assert len(calls)==1 and calls[0]['exposure_working_icc_sha']==WORKING_PROFILE_SHA
    assert calls[0]['layer_scratch_bytes']==128*1024**2


@pytest.mark.parametrize('enabled,pixels,valid',[
    ('false','',True),('true','12',True),('true','',False),
    ('true','0',False),('true','200000001',False),('yes','12',False),
])
def test_delivery_encode_adopter_explicit_bounded_config(tmp_path,enabled,pixels,valid):
    env=dict(DATABASE_URL='postgres://unused',S3_ENDPOINT='https://objects.example',
        S3_BUCKET='photos',S3_ACCESS_KEY='key',S3_SECRET_KEY='secret',
        PHOTO_ADOPTER_LAYER_SCRATCH_ROOT=str(tmp_path),PHOTO_ADOPTER_LAYER_SCRATCH_BYTES='100000',
        PHOTO_ADOPTER_DELIVERY_ENCODE_ENABLED=enabled,PHOTO_ADOPTER_DELIVERY_ENCODE_MAX_PIXELS=pixels)
    if valid:
        config=AdopterConfig.from_env(env)
        assert config.delivery_encode_max_pixels==(12 if enabled=='true' else None)
    else:
        with pytest.raises(ValueError):AdopterConfig.from_env(env)


def test_delivery_encode_runtime_processed_before_finalizations():
    from threading import Event
    trace=[]
    class Read:
        def execute(self,sql,args=None):
            trace.append(sql)
            return SimpleNamespace(fetchall=lambda:[])
    adopter=object.__new__(Adopter)
    adopter.stop=Event();adopter.read=Read();adopter.write=object();adopter.exposure_enabled=False;adopter.shown_exposure_enabled=False;adopter.shown_removal_enabled=False;adopter.shown_background_cleanup_enabled=False;adopter.shown_retouch_enabled=False;adopter.lens_preview_runner=None
    def pending(limit):
        trace.append('delivery encode')
        assert limit==10
        return SimpleNamespace(adopted=2,rejected=((uuid4(),uuid4(),'refused'),))
    adopter.delivery_encode=SimpleNamespace(process_pending=pending)
    adopter.restoration_preview_runner=None
    adopter.derivative_coordinator=None
    adopter.multi_quality_runner=None
    adopter.multi_preview_runner=None
    adopter.perspective_preview_runner=None
    adopter.perspective_quality_runner=None
    counts=adopter.run_once()
    assert counts['adopted']==2 and counts['retry']==1
    assert trace.index('delivery encode')<trace.index('SELECT * FROM photo.v2_pending_finalizations()')


def test_adopter_installs_shown_copy_report_authority_independently(monkeypatch):
    from photo_worker.v2 import service,shown_exposure_installation
    installed=[];authority=lambda *args:None
    def factory(connection,objects,bucket,**kwargs):
        installed.append((connection,objects,bucket,kwargs))
        return authority
    monkeypatch.setattr(shown_exposure_installation,'versioned_shown_exposure_adoption_installation',factory)
    read=SimpleNamespace(autocommit=True);write=SimpleNamespace(autocommit=True);objects=object()
    adopter=service.Adopter(read,write,objects,'owned',shown_exposure_enabled=True)
    assert adopter.coordinator.shown_exposure_installation is authority
    assert adopter.shown_exposure_enabled is True
    assert adopter.exposure_enabled is False
    assert installed[0][:3]==(read,objects,'owned')
    baseline=service.Adopter(read,write,objects,'owned')
    assert baseline.coordinator.shown_exposure_installation is None
    assert baseline.shown_exposure_enabled is False


@pytest.mark.parametrize('value',[None,1,'true'])
def test_adopter_rejects_ambiguous_shown_copy_enablement(value):
    with pytest.raises(ValueError,match='shown exposure adopter'):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',shown_exposure_enabled=value)


def test_adopter_installs_shown_removal_copy_report_authority_independently(monkeypatch):
    from photo_worker.v2 import service,shown_removal_installation
    installed=[];authority=lambda *args:None
    def factory(connection,objects,bucket,**kwargs):
        installed.append((connection,objects,bucket,kwargs))
        return authority
    monkeypatch.setattr(shown_removal_installation,'versioned_shown_removal_adoption_installation',factory)
    read=SimpleNamespace(autocommit=True);write=SimpleNamespace(autocommit=True);objects=object()
    adopter=service.Adopter(read,write,objects,'owned',shown_removal_enabled=True)
    assert adopter.coordinator.shown_removal_installation is authority
    assert adopter.shown_removal_enabled is True
    assert adopter.exposure_enabled is False
    assert installed[0][:3]==(read,objects,'owned')
    baseline=service.Adopter(read,write,objects,'owned')
    assert baseline.coordinator.shown_removal_installation is None
    assert baseline.shown_removal_enabled is False


@pytest.mark.parametrize('value',[None,1,'true'])
def test_adopter_rejects_ambiguous_shown_removal_copy_enablement(value):
    with pytest.raises(ValueError,match='shown removal adopter'):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',shown_removal_enabled=value)


def test_adopter_installs_shown_background_cleanup_copy_report_authority_independently(monkeypatch,tmp_path):
    from photo_worker.v2 import service,shown_background_cleanup_installation
    installed=[];authority=lambda *args:None
    def factory(connection,objects,bucket,**kwargs):
        installed.append((connection,objects,bucket,kwargs))
        return authority
    monkeypatch.setattr(shown_background_cleanup_installation,'versioned_shown_background_cleanup_adoption_installation',factory)
    read=SimpleNamespace(autocommit=True);write=SimpleNamespace(autocommit=True);objects=object()
    adopter=service.Adopter(read,write,objects,'owned',shown_background_cleanup_enabled=True,
        layer_scratch_root=tmp_path,layer_scratch_bytes=1024)
    assert adopter.coordinator.shown_background_cleanup_installation is authority
    assert adopter.shown_background_cleanup_enabled is True
    assert adopter.exposure_enabled is False
    assert installed[0][:3]==(read,objects,'owned')
    baseline=service.Adopter(read,write,objects,'owned')
    assert baseline.coordinator.shown_background_cleanup_installation is None
    assert baseline.shown_background_cleanup_enabled is False


@pytest.mark.parametrize('value',[None,1,'true'])
def test_adopter_rejects_ambiguous_shown_background_cleanup_copy_enablement(value):
    with pytest.raises(ValueError,match='shown background cleanup adopter'):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',shown_background_cleanup_enabled=value)


@pytest.mark.parametrize('enabled',[False,True])
def test_cleanup_adopter_discovers_own_results_only_when_installed(tmp_path,enabled):
    queries=[]
    def execute(query,*args):
        queries.append(query)
        return SimpleNamespace(fetchall=lambda:[])
    read=SimpleNamespace(autocommit=True,execute=execute)
    write=SimpleNamespace(autocommit=True)
    adopter=Adopter(read,write,object(),'owned',shown_background_cleanup_enabled=enabled,
        layer_scratch_root=tmp_path if enabled else None,layer_scratch_bytes=1024 if enabled else None)
    assert (adopter.coordinator.shown_background_cleanup_installation is not None)==enabled
    assert queries==[]
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':0,'retry':0}
    own='SELECT * FROM photo.v27_pending_shown_background_cleanup_adoptions()'
    assert queries.count(own)==(1 if enabled else 0)
    assert queries.count('SELECT * FROM photo.v2_pending_adoptions()')==1
    assert 'SELECT * FROM photo.v27_pending_shown_removal_adoptions()' not in queries
    assert 'SELECT * FROM photo.v27_pending_shown_exposure_adoptions()' not in queries


def test_adopter_installs_shown_retouch_copy_report_authority_independently(monkeypatch,tmp_path):
    from photo_worker.v2 import service,shown_retouch_installation
    installed=[];authority=lambda *args:None
    def factory(connection,objects,bucket,**kwargs):
        installed.append((connection,objects,bucket,kwargs))
        return authority
    monkeypatch.setattr(shown_retouch_installation,'versioned_shown_retouch_adoption_installation',factory)
    read=SimpleNamespace(autocommit=True);write=SimpleNamespace(autocommit=True);objects=object()
    adopter=service.Adopter(read,write,objects,'owned',shown_retouch_enabled=True,
        layer_scratch_root=tmp_path,layer_scratch_bytes=1024)
    assert adopter.coordinator.shown_retouch_installation is authority
    assert adopter.shown_retouch_enabled is True
    assert adopter.exposure_enabled is False
    assert installed[0][:3]==(read,objects,'owned')
    baseline=service.Adopter(read,write,objects,'owned')
    assert baseline.coordinator.shown_retouch_installation is None
    assert baseline.shown_retouch_enabled is False


@pytest.mark.parametrize('value',[None,1,'true'])
def test_adopter_rejects_ambiguous_shown_retouch_copy_enablement(value):
    with pytest.raises(ValueError,match='shown retouch adopter'):
        Adopter(SimpleNamespace(autocommit=True),object(),object(),'owned',shown_retouch_enabled=value)


@pytest.mark.parametrize('enabled',[False,True])
def test_retouch_adopter_discovers_own_results_only_when_installed(tmp_path,enabled):
    queries=[]
    def execute(query,*args):
        queries.append(query)
        return SimpleNamespace(fetchall=lambda:[])
    read=SimpleNamespace(autocommit=True,execute=execute)
    write=SimpleNamespace(autocommit=True)
    adopter=Adopter(read,write,object(),'owned',shown_retouch_enabled=enabled,
        layer_scratch_root=tmp_path if enabled else None,layer_scratch_bytes=1024 if enabled else None)
    assert (adopter.coordinator.shown_retouch_installation is not None)==enabled
    assert queries==[]
    assert adopter.run_once()=={'adopted':0,'rejected':0,'finalized':0,'retry':0}
    own='SELECT * FROM photo.v27_pending_shown_retouch_adoptions()'
    assert queries.count(own)==(1 if enabled else 0)
    assert queries.count('SELECT * FROM photo.v2_pending_adoptions()')==1
    assert 'SELECT * FROM photo.v27_pending_shown_removal_adoptions()' not in queries
    assert 'SELECT * FROM photo.v27_pending_shown_exposure_adoptions()' not in queries


@pytest.mark.parametrize('lane', ['exposure', 'removal', 'background_cleanup', 'retouch'])
@pytest.mark.parametrize('failure', ['permanent', 'stale', 'transport'])
def test_shown_lane_failure_routing_keeps_result_scope_and_releases_lock(lane, failure, tmp_path):
    # Service boundary only: operation-specific physical adoption is covered by
    # the separate real-SQL child operation fixtures, not this fake coordinator.
    tenant, run, task, result = uuid4(), uuid4(), uuid4(), uuid4()
    suffix = 'retouch' if lane == 'retouch' else lane
    own_query = f'SELECT * FROM photo.v27_pending_shown_{suffix}_adoptions()'
    queries, writes = [], []
    def read_execute(query, params=()):
        queries.append(query)
        if query == own_query: return Cursor([(tenant, run, task)])
        if 'pg_try_advisory_lock' in query or 'pg_advisory_unlock' in query: return Cursor([(True,)])
        return Cursor([])
    def write_execute(query, params):
        writes.append((query, params))
        assert query == 'SELECT photo.reject_v2_adoption(%s,%s,%s,%s)'
        return Cursor([(True,)])
    read = SimpleNamespace(autocommit=True, execute=read_execute)
    write = SimpleNamespace(autocommit=True, execute=write_execute, transaction=lambda:nullcontext())
    adopter = Adopter(read, write, object(), 'owned', layer_scratch_root=tmp_path, layer_scratch_bytes=1024, **{f'shown_{lane}_enabled':True})
    contexts = []
    def invalid(actual_tenant, actual_task):
        assert (actual_tenant, actual_task) == (tenant, task)
        if failure == 'transport': raise OSError('temporary object outage')
        raise AdoptionInvalid('V2_ADOPTION_CONTEXT_STALE' if failure == 'stale' else 'ARTIFACT_SCHEMA_INVALID')
    def context(actual_tenant, actual_task):
        contexts.append((actual_tenant, actual_task))
        return SimpleNamespace(result_id=result)
    adopter.coordinator = SimpleNamespace(adopt=invalid, context=context)
    counts = adopter.run_once()
    assert counts == {'adopted':0,'rejected':int(failure == 'permanent'),'finalized':0,'retry':int(failure != 'permanent')}
    if failure == 'permanent':
        assert contexts == [(tenant, task)]
        assert writes[0][1] == (tenant, task, result, 'ARTIFACT_SCHEMA_INVALID')
    else:
        assert contexts == [] and writes == []
    assert queries.count(own_query) == 1
    assert sum('pg_try_advisory_lock' in query for query in queries) == 1
    assert sum('pg_advisory_unlock' in query for query in queries) == 1
