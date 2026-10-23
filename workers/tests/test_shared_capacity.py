from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace
import json,os
import pytest
from photo_worker.runtime import shared_capacity as shared
from photo_worker.runtime.capacity import Capacity
from photo_worker.runtime.capacity_policy import CapacityPolicy
from photo_worker.runtime.service import WorkerConfig,WorkerConfigurationError,_capacity_sample
G=1<<30

@pytest.fixture
def installation(tmp_path,monkeypatch):
    root=tmp_path/'cgroup';parent=root/'workers.slice';child=parent/'cpu.service';child.mkdir(parents=True)
    membership=tmp_path/'membership';membership.write_text('0::/workers.slice/cpu.service\n')
    mountinfo=tmp_path/'mountinfo';mountinfo.write_text(f'42 1 0:42 / {root} rw - cgroup2 cgroup rw\n')
    for n,v in [('memory.max',8*G),('memory.current',128<<20),('memory.swap.max',0)]: (parent/n).write_text(str(v)+'\n')
    monkeypatch.setattr(shared,'_CGROUP_ROOT',root);monkeypatch.setattr(shared,'_PROC_CGROUP',membership);monkeypatch.setattr(shared,'_MOUNTINFO',mountinfo)
    payload=json.loads((Path(__file__).parents[2]/'configs/capacity-rtx3060.json').read_text());payload.update(photo_ram_budget_bytes=8*G,control_ram_budget_bytes=8*G)
    policy=CapacityPolicy.model_validate(payload);config=shared.SharedParentConfig(parent,8*G,256<<20,policy.sha256)
    return parent,membership,mountinfo,policy,config

def env(config):
    return dict(PHOTO_SHARED_CAPACITY_PARENT=str(config.parent),PHOTO_SHARED_CAPACITY_RAM_BYTES=str(config.ram_bytes),PHOTO_SHARED_CAPACITY_IDLE_MARGIN_BYTES=str(config.idle_margin_bytes),PHOTO_SHARED_CAPACITY_CONFIG_SHA256=config.sha256)

def test_cpu_and_archive_share_actual_parent_not_individual_child_caps(installation):
    parent,_,_,policy,c=installation
    for kind in ['cpu','archive']:
        parsed=shared.from_environment(env(c),resource_class=kind,backend='cpu',policy=policy);assert parsed==c
        config=SimpleNamespace(resource_class=kind,backend='cpu',capacity_policy=policy,scratch_root=parent,shared_parent_capacity=c)
        result=_capacity_sample(config,Capacity(30*G,10*G));assert result.ram_bytes==8*G-(128<<20)-(256<<20)
    (parent/'memory.current').write_text(str(7*G))
    assert shared.cap_measurement(c,Capacity(30*G,123)).ram_bytes==G-(256<<20)
    assert shared.cap_measurement(c,Capacity(17,123))==Capacity(17,123,0)

@pytest.mark.parametrize('name,value',[('memory.max','max'),('memory.max',str(9*G)),('memory.swap.max','1'),('memory.current',str(9*G)),('memory.current','-1'),('memory.current','NaN')])
def test_limit_or_usage_drift_refuses_every_report(installation,name,value):
    parent,_,_,_,c=installation;(parent/name).write_text(value)
    with pytest.raises(ValueError):shared.cap_measurement(c,Capacity(30*G,1))

def test_exhaustion_reports_zero_never_negative(installation):
    parent,_,_,_,c=installation;(parent/'memory.current').write_text(str(8*G));assert shared.cap_measurement(c,Capacity(30*G,1)).ram_bytes==0

@pytest.mark.parametrize('membership',['0::/other/cpu.service\n','0::/workers.slice\n','0::/workers.slice/../escape\n','1:memory:/workers.slice/cpu.service\n'])
def test_real_strict_unified_ancestor_required(installation,membership):
    _,path,_,_,c=installation;path.write_text(membership)
    with pytest.raises(ValueError):shared.cap_measurement(c,Capacity(G,1))

def test_symlink_control_and_parent_refused(installation,tmp_path):
    parent,_,_,_,c=installation;real=parent/'real';(parent/'memory.max').rename(real);(parent/'memory.max').symlink_to(real)
    with pytest.raises(OSError):shared.cap_measurement(c,Capacity(G,1))
    (parent/'memory.max').unlink();real.rename(parent/'memory.max');alias=parent.parent/'alias';alias.symlink_to(parent,target_is_directory=True)
    # Membership must point through alias, otherwise ancestor validation itself refuses it.
    shared._PROC_CGROUP.write_text('0::/alias/cpu.service\n')
    with pytest.raises(OSError):shared.cap_measurement(replace(c,parent=alias),Capacity(G,1))

def test_untrusted_owner_mode_and_non_cgroup2_refused(installation,monkeypatch):
    parent,_,mount,_,c=installation
    parent.chmod(0o777)
    with pytest.raises(ValueError,match='OWNER_OR_MODE'):shared.cap_measurement(c,Capacity(G,1))
    parent.chmod(0o755);mount.write_text('42 1 0:42 / /other rw - tmpfs tmpfs rw\n')
    with pytest.raises(ValueError,match='CGROUP2'):shared.cap_measurement(c,Capacity(G,1))
    with pytest.raises(ValueError,match='OWNER_OR_MODE'):shared._trusted(SimpleNamespace(st_uid=987654,st_mode=0o755))

def test_membership_change_during_sample_refused(installation,monkeypatch):
    _,_,_,_,c=installation;values=iter([Path('/workers.slice/cpu.service'),Path('/elsewhere/cpu.service')]);monkeypatch.setattr(shared,'_membership',lambda:next(values))
    with pytest.raises(ValueError,match='MEMBERSHIP_CHANGED'):shared.cap_measurement(c,Capacity(G,1))

@pytest.mark.parametrize('kind,backend',[('media','cpu'),('gpu','cuda'),('gpu','cpu')])
def test_opt_in_is_only_cpu_archive(installation,kind,backend):
    _,_,_,policy,c=installation
    with pytest.raises(ValueError):shared.from_environment(env(c),resource_class=kind,backend=backend,policy=policy)

def test_config_sha_separate_from_policy_and_all_fields_required(installation):
    _,_,_,policy,c=installation
    assert shared.from_environment({},resource_class='cpu',backend='cpu',policy=policy)is None
    for key in env(c):
        values=env(c);values.pop(key)
        with pytest.raises(ValueError):shared.from_environment(values,resource_class='cpu',backend='cpu',policy=policy)
    values=env(c);values['PHOTO_SHARED_CAPACITY_IDLE_MARGIN_BYTES']=str(128<<20)
    with pytest.raises(ValueError,match='SHA_MISMATCH'):shared.from_environment(values,resource_class='cpu',backend='cpu',policy=policy)
    mismatch=policy.model_copy(update={'photo_ram_budget_bytes':16*G})
    with pytest.raises(ValueError,match='BUDGET_MISMATCH'):shared.from_environment(env(c),resource_class='cpu',backend='cpu',policy=mismatch)
    with pytest.raises(ValueError):shared.from_environment(env(c),resource_class='cpu',backend='cpu',policy=None)

def test_worker_config_default_and_explicit_shared_roundtrip(installation,tmp_path,monkeypatch):
    parent,_,_,policy,c=installation;root=tmp_path/'scratch';root.mkdir();policyfile=tmp_path/'policy.json';policyfile.write_text(json.dumps(policy.model_dump(mode='json')))
    values=dict(APP_ENV='development',DATABASE_URL='postgresql://worker:password@localhost/db',AMQP_URL='amqp://localhost',PHOTO_WORKER_ID='99999999-0000-4000-8000-000000000001',PHOTO_WORKER_CLASS='cpu',PHOTO_WORKER_BACKEND='cpu',PHOTO_SCRATCH_ROOT=str(root),GATEWAY_ORIGIN='https://localhost:443',GATEWAY_CA_FILE='ca',WORKER_CERT_FILE='cert',WORKER_KEY_FILE='key',WORKER_MAX_SCRATCH_BYTES='100',WORKER_MAX_DOWNLOAD_BYTES='100',WORKER_MAX_UPLOAD_BYTES='100',PHOTO_CAPACITY_POLICY=str(policyfile))
    assert WorkerConfig.from_env(values).shared_parent_capacity is None
    actual=WorkerConfig.from_env({**values,**env(c)});assert actual.shared_parent_capacity==c
    with pytest.raises(WorkerConfigurationError,match='PHOTO_SHARED_CAPACITY'):WorkerConfig.from_env({**values,**env(c),'PHOTO_SHARED_CAPACITY_CONFIG_SHA256':'0'*64})
    # Shared module is never sampled by ordinary workers.
    monkeypatch.setattr(shared,'_state',lambda _:(_ for _ in ()).throw(AssertionError('unexpected shared sampling')))
    config=WorkerConfig.from_env(values);assert _capacity_sample(config,Capacity(77,100)).ram_bytes==77


@pytest.mark.parametrize('measured',[Capacity(True,1,0),Capacity(-1,1,0),Capacity(1,1,1)])
def test_invalid_or_gpu_measurement_never_advertises_shared_cpu_capacity(installation,measured):
    with pytest.raises(ValueError,match='INVALID_MEASUREMENT'):
        shared.cap_measurement(installation[-1],measured)
