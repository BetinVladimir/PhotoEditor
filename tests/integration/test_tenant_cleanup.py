from uuid import uuid4
from psycopg import sql

def test_tenant_cleanup_claim_checkpoint_and_finish_are_fenced(db):
 user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone()
 session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(user,uuid4().bytes)).fetchone()[0]
 tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
 project=db.execute("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'purge-me','restricted') RETURNING id",(tenant,)).fetchone()[0]
 db.execute("INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES(%s,'user',%s,'project.create','project',%s,gen_random_uuid(),'{}')",(tenant,user,project))
 db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tenant,))
 job=db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s) RETURNING id",(tenant,tenant,user)).fetchone()[0]
 first=db.execute('SELECT job_id,cleanup_token,tenant_id FROM photo.claim_tenant_deletion()').fetchone();assert first[0]==job and first[2]==tenant
 assert not db.execute('SELECT photo.checkpoint_tenant_deletion(%s,%s)',(job,uuid4())).fetchone()[0]
 assert db.execute('SELECT photo.checkpoint_tenant_deletion(%s,%s)',(job,first[1])).fetchone()[0]
 second=db.execute('SELECT job_id,cleanup_token,tenant_id FROM photo.claim_tenant_deletion()').fetchone();assert second[0]==job and second[1]!=first[1]
 assert not db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,first[1])).fetchone()[0]
 assert db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,second[1])).fetchone()[0]
 assert db.execute('SELECT status FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]=='deleted'
 assert db.execute('SELECT status,cleanup_passes,cleanup_token FROM photo.deletion_jobs WHERE id=%s',(job,)).fetchone()==('completed',2,None)
 assert db.execute('SELECT status FROM photo.memberships WHERE tenant_id=%s',(tenant,)).fetchone()[0]=='suspended'
 assert db.execute('SELECT count(*) FROM photo.authorize_tenant(%s,%s,%s)',(user,session,tenant)).fetchone()[0]==0
 assert db.execute('SELECT count(*) FROM photo.list_user_tenants(%s,%s,NULL,101) WHERE id=%s',(user,session,tenant)).fetchone()[0]==0
 assert db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND action NOT LIKE 'tenant.%%'",(tenant,)).fetchone()[0]==0
 assert db.execute("SELECT count(*) FROM photo.audit_log WHERE tenant_id=%s AND action='tenant.deleted'",(tenant,)).fetchone()[0]==1
 tables=[r[0] for r in db.execute("SELECT table_name FROM information_schema.columns WHERE table_schema='photo' AND column_name='tenant_id' AND table_name NOT IN ('audit_log','deletion_jobs','memberships') ORDER BY table_name")]
 leftovers={table:db.execute(sql.SQL('SELECT count(*) FROM photo.{} WHERE tenant_id=%s').format(sql.Identifier(table)),(tenant,)).fetchone()[0] for table in tables}
 assert not {table:count for table,count in leftovers.items() if count}

def test_active_run_blocks_tenant_cleanup_claim(db):
 digest='a'*64;user,_=db.execute("SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user(%s,'en',true)",(f'{uuid4()}@test.example',)).fetchone();tenant=db.execute('SELECT id FROM photo.tenants WHERE owner_user_id=%s',(user,)).fetchone()[0]
 snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,0) RETURNING id",(tenant,user,digest)).fetchone()[0]
 db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,'ingest','running',%s,'{}',%s,%s)",(tenant,snapshot,digest,user))
 db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tenant,));db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s)",(tenant,tenant,user))
 assert db.execute('SELECT job_id FROM photo.claim_tenant_deletion()').fetchone() is None

def test_photo_fn_owner_delete_privileges_match_finish_allowlist(db):
 import re
 source=db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
 match=re.search(r'tables_to_purge\s+constant\s+text\[\]\s*:=\s*ARRAY\[(.*?)\]',source,re.S|re.I)
 assert match is not None
 allowed={'audit_log',*re.findall(r"'([a-z0-9_]+)'",match.group(1))}
 actual={row[0] for row in db.execute("""
  SELECT c.relname FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='photo' AND c.relkind IN ('r','p')
    AND has_table_privilege('photo_fn_owner',c.oid,'DELETE')
 """)}
 assert actual == allowed
 assert {'audit_log','apply_v4_stages','projects','worker_registrations'} <= actual
 assert not {'deletion_jobs','memberships','tenants','users','sessions'} & actual
