"""Real installed begin_output contracts on an explicit OWNED isolated PG cluster.

Relational task/worker fixtures only; no worker, object upload, physical ZIP or
photo quality qualification. Migration installation is normal Goose externally.
"""
import json,os,re
from pathlib import Path
from uuid import uuid4
import psycopg
from psycopg import errors
from psycopg.types.json import Jsonb
import pytest
from test_task_leases import queued_task,claim
MAX_ZIP=10*1024**3+1024**2+4096*5000
MAX_MANIFEST=1048576

@pytest.fixture
def db():
 path=os.environ.get('PHOTO_ARCHIVE_OUTPUT_POLICY_TEST_CONFIG')
 if not path:pytest.skip('Explicit owned isolated PostgreSQL config required')
 p=Path(path);assert p.stat().st_mode&0o777==0o600 and p.stat().st_size<=16384
 c=json.loads(p.read_text());kw=c['connection'];assert kw['host']=='127.0.0.1'and int(kw['port'])!=57432
 assert kw['dbname']==c['owned_database_name']and kw['dbname']not in('photoeditor','postgres','template0','template1')
 assert c['expected_system_identifier']!=c['forbidden_main_system_identifier']
 with psycopg.connect(**kw)as connection:
  assert connection.execute('SELECT current_database(),(SELECT system_identifier::text FROM pg_control_system())').fetchone()==(c['owned_database_name'],c['expected_system_identifier'])
  assert connection.execute('SELECT max(version_id)FROM public.goose_db_version WHERE is_applied').fetchone()==(2658,)
  connection.rollback()
  with connection.transaction(force_rollback=True):yield connection

def prepare(db,output_budget=MAX_ZIP+MAX_MANIFEST):
 x=queued_task(db);command={**x['command'],'operation':'archive.build','resource_class':'archive','timeout_seconds':7200};x['command']=command
 db.execute("UPDATE photo.tasks SET operation='archive.build',resource_class='archive',timeout_seconds=7200,dispatch_command=%s,execution_profile=%s WHERE id=%s",(Jsonb(command),Jsonb({'backend':'cpu','precision':'byte','resource_estimate':{'ram_bytes':1<<30,'vram_bytes':0,'scratch_bytes':MAX_ZIP+MAX_MANIFEST,'output_bytes':output_budget,'pixel_count':0}}),x['task']))
 db.execute("UPDATE photo.worker_registrations SET resource_classes='[\"archive\"]',qualified_operations='{\"archive.build\":[\"cpu\"]}',certificate_sha256=%s WHERE worker_id=%s",('f'*64,x['worker']))
 db.execute("UPDATE photo.resource_slots SET resource_class='archive'WHERE task_id=%s",(x['task'],));db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb(%s::bigint))WHERE id=%s",(100*(1<<30),x['tenant']))
 db.execute('SET LOCAL ROLE photo_worker');assert claim(db,x)[0]=='claimed';db.execute('RESET ROLE');return x

def begin(db,x,role,media,size,key=None,fence=1):
 return db.execute('SELECT * FROM photo.begin_output(%s,1,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(x['task'],fence,x['worker'],'f'*64,key or uuid4(),role,size,'a'*64,media,'test')).fetchone()

def test_exact_two_installed_rows(db):
 rows=db.execute("SELECT version,role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='archive.build'ORDER BY role").fetchall();assert rows==[('1','archive',['application/zip'],MAX_ZIP,1),('1','archive_manifest',['application/json'],MAX_MANIFEST,1)]

def test_real_gateway_reserves_each_exact_role_once_and_replays(db):
 x=prepare(db);key=uuid4();db.execute('SET LOCAL ROLE photo_gateway');first=begin(db,x,'archive','application/zip',MAX_ZIP,key);assert first[0]=='created';assert begin(db,x,'archive','application/zip',MAX_ZIP,key)[0]=='busy';assert begin(db,x,'archive','application/zip',1)[0]=='quota';assert begin(db,x,'archive_manifest','application/json',MAX_MANIFEST)[0]=='created'
 with pytest.raises(errors.InsufficientPrivilege),db.transaction():db.execute('SELECT * FROM photo.output_uploads')
 db.execute('RESET ROLE');assert db.execute('SELECT count(*),sum(bytes)FROM photo.output_uploads WHERE task_id=%s',(x['task'],)).fetchone()==(2,MAX_ZIP+MAX_MANIFEST)

@pytest.mark.parametrize('role,media,size',[('archive','application/json',1),('archive_manifest','application/zip',1),('other','application/json',1),('archive','application/zip',MAX_ZIP+1),('archive_manifest','application/json',MAX_MANIFEST+1)])
def test_wrong_role_media_or_excess_bytes_denied_without_ticket(db,role,media,size):
 x=prepare(db);db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,role,media,size)[0]=='forbidden';db.execute('RESET ROLE');assert db.execute('SELECT count(*)FROM photo.output_uploads WHERE task_id=%s',(x['task'],)).fetchone()==(0,);assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(x['tenant'],)).fetchone()==(0,)

def test_execution_profile_budget_is_not_bypassed(db):
 x=prepare(db,1);db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,'archive','application/zip',2)[0]=='quota'

def test_tenant_quota_is_not_bypassed(db):
 x=prepare(db);db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1')WHERE id=%s",(x['tenant'],));db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,'archive','application/zip',2)[0]=='quota'

def test_stale_fence_is_not_bypassed(db):
 x=prepare(db);db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,'archive','application/zip',2,fence=2)[0]=='stale'

def test_cancelled_run_is_not_bypassed(db):
 x=prepare(db);db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(x['run'],));db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,'archive','application/zip',2)[0]=='stale'

def test_manifest_role_count_one_is_independent(db):
 x=prepare(db);db.execute('SET LOCAL ROLE photo_gateway');assert begin(db,x,'archive_manifest','application/json',1)[0]=='created';assert begin(db,x,'archive_manifest','application/json',1)[0]=='quota';assert begin(db,x,'archive','application/zip',1)[0]=='created'
