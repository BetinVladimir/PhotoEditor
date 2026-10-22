"""Actual operator/admission SQL on an explicit disposable clone only.

Credentials/cert/Doctor/proof rows are SYNTHETIC RELATIONAL FIXTURES, not physical
qualification. Existing restricted global roles are reused without changing them.
"""
import copy,hashlib,json,os,re,time
from datetime import datetime,timezone
from pathlib import Path
from uuid import uuid4
from concurrent.futures import ThreadPoolExecutor
import psycopg
from psycopg import errors,sql
from psycopg.types.json import Jsonb
import pytest
from photo_worker.runtime.capacity_policy import CapacityPolicy
from photo_worker.runtime.capacity_policy_installation import install_capacity_policy_timing
from test_database import tenant
ROOT=Path(__file__).resolve().parents[2]
G=1<<30
class HiddenDatabase(dict):
 def __repr__(self):return '<isolated shared archive database>'

EMPTY='e3c7b8502ee0d2b532f3a83de0f179e72179492104881c234c91ce1ccfb747be'

@pytest.fixture(scope='session')
def database():
 path=os.environ.get('PHOTO_SHARED_ARCHIVE_TEST_CONFIG')
 if not path:pytest.skip('Explicit isolated shared-archive database required')
 p=Path(path);assert p.stat().st_mode&0o777==0o600
 v=json.loads(p.read_text());assert re.fullmatch(r'photo_shared_archive_[a-f0-9]{32}',v['dbname'])
 return HiddenDatabase(v)

def apply(db,phase,payload):
 text=(ROOT/f'deploy/{phase}-shared-archive-worker.sql').read_text();text='\n'.join(l for l in text.splitlines()if not l.startswith('\\')and l.strip()not in('BEGIN;','COMMIT;'))
 db.execute(re.sub(":'shared_archive_json'",lambda _:sql.Literal(json.dumps(payload)).as_string(db),text))

def state(db,cpu):
 node=db.execute('SELECT to_jsonb(n) FROM photo.resource_nodes n WHERE node_id=%s',(cpu['node'],)).fetchone()[0]
 worker=db.execute('SELECT to_jsonb(w) FROM photo.worker_registrations w WHERE worker_id=%s',(cpu['worker'],)).fetchone()[0]
 slots=db.execute("SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY slot_name),'[]'::jsonb)FROM photo.resource_slots s WHERE node_id=%s",(cpu['node'],)).fetchone()[0]
 return dict(expected_node=node,expected_cpu=worker,expected_slots=slots)

def prepare(db):
 roles=[r[0]for r in db.execute("SELECT rolname FROM pg_roles WHERE rolcanlogin AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication AND pg_has_role(oid,'photo_worker','MEMBER') ORDER BY rolname").fetchall()];assert len(roles)>=2,'EXISTING_RESTRICTED_ROLES_REQUIRED_NO_GLOBAL_ROLE_CHANGES'
 tid,user=tenant(db,uuid4().hex+'@shared-fixture.invalid');db.execute('INSERT INTO photo.tenant_usage(tenant_id)VALUES(%s)',(tid,))
 policy=CapacityPolicy.model_validate({**json.loads((ROOT/'configs/capacity-rtx3060.json').read_text()),'photo_ram_budget_bytes':8*G,'control_ram_budget_bytes':8*G});install_capacity_policy_timing(db,policy)
 cpu=dict(tenant=tid,user=user,worker=uuid4(),node='shared-fixture-'+uuid4().hex,principal=roles[0]);archive=dict(worker_id=str(uuid4()),db_principal=roles[1],certificate_sha256='b'*64,lock_id=EMPTY)
 db.execute("INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,doctor_report_sha256)VALUES(%s,true,%s,0,%s,%s)",(cpu['node'],8*G,12*G,'a'*64))
 db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,db_principal,enabled,qualified_operations,lock_id,certificate_sha256,capacity_policy_sha,last_seen,capacity)VALUES(%s,%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,true,'{\"asset.inspect\":[\"cpu\"]}',%s,%s,%s,'epoch','{}')",(tid,cpu['worker'],cpu['node'],roles[0],EMPTY,'c'*64,policy.sha256))
 for i in (1,2):db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity)VALUES(%s,%s,'cpu','{}')",(cpu['node'],'cpu'+str(i)))
 report=json.dumps(dict(schema_version=1,backend='cpu',lock_id=EMPTY,models=[]),sort_keys=True,separators=(',',':'))
 proof=dict(status='OPERATOR_REVIEWED_SHARED_PARENT',reviewed_at=datetime.now(timezone.utc).isoformat(),cpu_worker_id=str(cpu['worker']),archive_worker_id=archive['worker_id'],policy_sha256=policy.sha256,cpu_stopped=True,archive_stopped=True,scheduler_stopped=True,rabbit_empty=True,shared_parent_kernel_reviewed=True,parent_memory_max_bytes=8*G,parent_swap_max_bytes=0,parent_cpu_quota_per_sec_usec=2000000,parent_cgroup_path='/sys/fs/cgroup/test-shared.slice',shared_config_sha256='1'*64,host_evidence_sha256='2'*64,source_manifest_sha256='3'*64,operation_evidence_sha256='4'*64,physical_ram_bytes=33514270720,total_group_caps_bytes=28*G,os_reserve_bytes=3*G)
 payload=dict(**state(db,cpu),archive=archive,doctor_report_jcs=report,doctor_report_file_sha=hashlib.sha256(report.encode()).hexdigest(),preflight=proof)
 return cpu,payload

def activated(db):
 cpu,p=prepare(db);apply(db,'attach',p);p.update(state(db,cpu));p['expected_archive']=db.execute('SELECT to_jsonb(w)FROM photo.worker_registrations w WHERE worker_id=%s',(p['archive']['worker_id'],)).fetchone()[0];apply(db,'activate',p);return cpu,p

def test_attach_disabled_activate_stale_preserves_class_node_budget_and_cpu_doctor(db):
 cpu,p=prepare(db);before=copy.deepcopy(p);apply(db,'attach',p);a=db.execute('SELECT to_jsonb(w)FROM photo.worker_registrations w WHERE worker_id=%s',(p['archive']['worker_id'],)).fetchone()[0];assert a['enabled']is False and a['qualified_operations']=={} and a['resource_classes']==['archive'];assert state(db,cpu)=={k:before[k]for k in('expected_node','expected_cpu','expected_slots')}
 p.update(state(db,cpu));p['expected_archive']=a;apply(db,'activate',p);after=state(db,cpu);assert after['expected_node']['ram_budget_bytes']==8*G and after['expected_node']['doctor_report_sha256']==before['expected_node']['doctor_report_sha256'];assert after['expected_cpu']['qualified_operations']==before['expected_cpu']['qualified_operations'];assert [s['resource_class']for s in after['expected_slots']].count('archive')==1
 assert db.execute('SELECT enabled,qualified_operations,last_seen=\'epoch\',reported_capacity_policy_sha IS NULL FROM photo.worker_registrations WHERE worker_id=%s',(p['archive']['worker_id'],)).fetchone()==(True,{'archive.build':['cpu']},True,True)
 assert db.execute('SELECT count(*)FROM photo.resource_nodes WHERE node_id=%s',(cpu['node'],)).fetchone()==(1,)

@pytest.mark.parametrize('case',['node_drift','cpu_drift','slot_drift','busy_slot','reserved','missing_review','missing_physical','bad_parent_budget','bad_doctor','same_certificate','bad_lock','role_reuse'])
def test_denied_attach_is_atomic(db,case):
 cpu,p=prepare(db)
 if case=='node_drift':p['expected_node']['scratch_budget_bytes']-=1
 elif case=='cpu_drift':p['expected_cpu']['capacity']={'drift':True}
 elif case=='slot_drift':p['expected_slots']=[]
 elif case=='busy_slot':
  tid=task(db,cpu,'cpu');db.execute("UPDATE photo.resource_slots SET tenant_id=%s,task_id=%s,fencing_token=0,lease_until=clock_timestamp()+interval '60 seconds'WHERE node_id=%s AND slot_name='cpu1'",(cpu['tenant'],tid,cpu['node']));p.update(state(db,cpu))
 elif case=='reserved':
  tid=task(db,cpu,'cpu');db.execute("INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,expires_at)VALUES(%s,%s,%s,1,1,0,clock_timestamp()+interval '60 seconds')",(cpu['tenant'],tid,cpu['node']))
 elif case=='missing_review':p['preflight']['shared_parent_kernel_reviewed']=False
 elif case=='missing_physical':p['preflight'].pop('physical_ram_bytes')
 elif case=='bad_parent_budget':p['preflight']['parent_memory_max_bytes']=9*G
 elif case=='bad_doctor':p['doctor_report_file_sha']='f'*64
 elif case=='same_certificate':p['archive']['certificate_sha256']='c'*64
 elif case=='bad_lock':p['archive']['lock_id']='d'*64
 else:p['archive']['db_principal']=cpu['principal']
 before=state(db,cpu)
 with pytest.raises((errors.RaiseException,errors.CheckViolation)):
  with db.transaction():apply(db,'attach',p)
 assert state(db,cpu)==before and db.execute('SELECT count(*)FROM photo.worker_registrations WHERE worker_id=%s',(p['archive']['worker_id'],)).fetchone()==(0,)

@pytest.mark.parametrize('case',['archive_row_drift','archive_enabled','expired_review','wrong_models','privileged_login'])
def test_denied_activation_is_atomic_and_keeps_cpu_authority(db,case):
 cpu,p=prepare(db);apply(db,'attach',p);p.update(state(db,cpu));p['expected_archive']=db.execute('SELECT to_jsonb(w)FROM photo.worker_registrations w WHERE worker_id=%s',(p['archive']['worker_id'],)).fetchone()[0]
 if case=='archive_row_drift':p['expected_archive']['capacity']={'drift':True}
 elif case=='archive_enabled':db.execute('UPDATE photo.worker_registrations SET enabled=true WHERE worker_id=%s',(p['archive']['worker_id'],))
 elif case=='expired_review':p['preflight']['reviewed_at']='2000-01-01T00:00:00+00:00'
 elif case=='wrong_models':
  report=json.loads(p['doctor_report_jcs']);report['models']=[{'model_id':'invented','status':'passed'}];p['doctor_report_jcs']=json.dumps(report,sort_keys=True,separators=(',',':'));p['doctor_report_file_sha']=hashlib.sha256(p['doctor_report_jcs'].encode()).hexdigest()
 else:p['archive']['db_principal']=db.info.user
 before=state(db,cpu);members=db.execute('SELECT to_jsonb(w)FROM photo.worker_registrations w WHERE node_id=%s ORDER BY id',(cpu['node'],)).fetchall()
 with pytest.raises(errors.RaiseException):
  with db.transaction():apply(db,'activate',p)
 assert state(db,cpu)==before and db.execute('SELECT to_jsonb(w)FROM photo.worker_registrations w WHERE node_id=%s ORDER BY id',(cpu['node'],)).fetchall()==members


def heartbeat(db,cpu,p,kind):
 identity=cpu['worker']if kind=='cpu'else p['archive']['worker_id'];principal=cpu['principal']if kind=='cpu'else p['archive']['db_principal'];ops=['asset.inspect']if kind=='cpu'else['archive.build']
 db.execute(sql.SQL('SET SESSION AUTHORIZATION {}').format(sql.Identifier(principal)));db.execute('SET ROLE photo_worker')
 try:assert db.execute('SELECT photo.heartbeat_worker_capacity_v2(%s,%s,\'cpu\',%s,%s,0,%s,%s)',(identity,kind,ops,8*G,12*G,p['preflight']['policy_sha256'])).fetchone()==(True,)
 finally:db.execute('RESET ROLE');db.execute('RESET SESSION AUTHORIZATION')

def task_ram(kind):return 8*G-(512<<20)if kind=='cpu'else G

def task(db,cpu,kind):
 params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type)VALUES(%s,'fixture',%s,%s,2,'application/json')RETURNING id",(cpu['tenant'],uuid4().hex,'a'*64)).fetchone()[0]
 snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count)VALUES(%s,'input','user',%s,%s,1)RETURNING id",(cpu['tenant'],cpu['user'],'b'*64)).fetchone()[0]
 run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)VALUES(%s,'apply','running',%s,'{}',%s,%s,true)RETURNING id",(cpu['tenant'],snapshot,'c'*64,cpu['user'])).fetchone()[0]
 operation='asset.inspect'if kind=='cpu'else'archive.build';tid=uuid4();db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)VALUES(%s,%s,%s,%s,%s,'1',%s,'ready','[]',%s,%s,%s)",(tid,cpu['tenant'],run,'fixture-'+uuid4().hex,operation,kind,params,'d'*64,Jsonb(dict(backend='cpu',resource_estimate=dict(ram_bytes=task_ram(kind),scratch_bytes=1024,output_bytes=1024,vram_bytes=0)))));return tid

def test_real_crossclass_reservation_node_lock_prevents_overcommit(database):
 with psycopg.connect(**database)as db:
  cpu,p=activated(db);heartbeat(db,cpu,p,'cpu');heartbeat(db,cpu,p,'archive');a=task(db,cpu,'cpu');b=task(db,cpu,'archive');db.execute('SET CONSTRAINTS ALL IMMEDIATE')
 from threading import Event
 locked=Event();release=Event()
 def reserve(t,kind,hold=False):
  with psycopg.connect(**database)as conn:
   conn.execute('SET LOCAL ROLE photo_scheduler')
   if not hold:conn.execute("SET LOCAL lock_timeout='500ms'")
   try:outcome=conn.execute('SELECT * FROM photo.reserve_dispatch(%s,%s,%s,0,1024,%s,%s,\'\')',(t,cpu['node'],task_ram(kind),uuid4(),uuid4())).fetchone()[0]
   except errors.LockNotAvailable:
    if hold:raise
    return 'lock_timeout'
   if hold:locked.set();assert release.wait(10)
   return outcome
 with ThreadPoolExecutor(max_workers=2)as pool:
  first=pool.submit(reserve,a,'cpu',True);assert locked.wait(10)
  try:second=pool.submit(reserve,b,'archive').result(timeout=10);assert second in ('waiting_resource','lock_timeout')
  finally:release.set()
  assert first.result(timeout=10)=='queued'
 # After the first node lock commits, actual aggregate reservation still denies
 # the normal 1GiB archive task against the same 8GiB physical parent budget.
 assert reserve(b,'archive')=='waiting_resource'
 with psycopg.connect(**database)as db:assert db.execute("SELECT sum(ram_bytes),count(*)FROM photo.resource_reservations WHERE node_id=%s AND state='reserved'",(cpu['node'],)).fetchone()==(task_ram('cpu'),1)
