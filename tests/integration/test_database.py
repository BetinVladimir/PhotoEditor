import json
import re
from pathlib import Path
from uuid import uuid4
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb


def tenant(db,email):
    user=db.execute('INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',(email,)).fetchone()[0]
    tid=db.execute("INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Test',%s,%s,'{}') RETURNING id",(user,Jsonb({'storage_bytes':10_000_000,'source_files':10_000,'gpu_minutes_month':10_000}))).fetchone()[0]
    db.execute("INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'owner')",(tid,user))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE');db.execute('SET CONSTRAINTS ALL DEFERRED')
    return tid,user


def test_all_tenant_tables_force_rls(db):
    rows=db.execute("SELECT c.relname,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='tenant_id' WHERE n.nspname='photo' AND c.relkind='r' AND c.relname<>'resource_slots'").fetchall()
    assert len(rows)>=50
    assert all(enabled and forced for _,enabled,forced in rows)


def test_style_analysis_schema_contains_fenced_geometry_epochs(db):
    definitions='\n'.join(row[0] for row in db.execute("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='photo.style_analysis_builds'::regclass OR conrelid='photo.style_analysis_nodes'::regclass").fetchall())
    for phase in ('features','body_fit','geometry','geometry_registration'):
        assert phase in definitions


def test_style_analysis_current_output_function_signature_is_installed(db):
    row=db.execute("SELECT to_regprocedure('photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text)')::text, to_regprocedure('photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text)')::text").fetchone()
    assert row[0]=='photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text)'
    assert row[1] is None
    source=db.execute("SELECT pg_get_functiondef('photo.advance_style_analysis_build(uuid,uuid,text,text)'::regprocedure)").fetchone()[0]
    assert "WHEN 'features' THEN 'body_fit'" in source
    assert "WHEN 'geometry' THEN 'geometry_registration'" in source


def test_scheduler_preflight_references_only_installed_procedures(db):
    source=(Path(__file__).resolve().parents[2]/'backend/internal/workflow/service.go').read_text()
    signatures=sorted(set(re.findall(r"to_regprocedure\('([^']+)'\)",source)))
    assert len(signatures)>=40
    # V22 is an optional, separately migrated capability. Its guarded
    # preflight may name absent functions, while every installed V21/base
    # procedure must resolve on a freshly migrated database.
    optional=[signature for signature in signatures if signature.startswith('photo.') and
              ('v22_' in signature or 'session_plan_document' in signature)]
    missing=[signature for signature in signatures if signature not in optional and
             db.execute('SELECT to_regprocedure(%s)::text',(signature,)).fetchone()[0] is None]
    assert missing==[]
    installed=[signature for signature in optional if
               db.execute('SELECT to_regprocedure(%s)::text',(signature,)).fetchone()[0] is not None]
    assert not installed or len(installed)==len(optional)


def test_tenant_isolation_and_missing_context(db):
    a,u=tenant(db,'a@example.test');b,v=tenant(db,'b@example.test')
    for t in [a,b]:db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Project')",(t,))
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute('SELECT count(*) FROM photo.projects').fetchone()[0]==0
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(a),))
    assert db.execute('SELECT DISTINCT tenant_id FROM photo.projects').fetchall()==[(a,)]
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():
        db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Forbidden')",(b,))


def test_cross_tenant_foreign_key(db):
    a,u=tenant(db,'a@example.test');b,v=tenant(db,'b@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'A') RETURNING id",(a,)).fetchone()[0]
    with pytest.raises(errors.ForeignKeyViolation),db.transaction():
        db.execute("INSERT INTO photo.collections(tenant_id,project_id,name) VALUES(%s,%s,'Cross')",(b,project))
        db.execute('SET CONSTRAINTS ALL IMMEDIATE')


def test_owner_invariant(db):
    a,u=tenant(db,'owner@example.test')
    with pytest.raises(errors.CheckViolation),db.transaction():
        db.execute("UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s",(a,))
        db.execute('SET CONSTRAINTS ALL IMMEDIATE')


def test_immutable_style_and_rename(db):
    a,u=tenant(db,'owner@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'P') RETURNING id",(a,)).fetchone()[0]
    sid=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Style',%s) RETURNING id",(a,project,u)).fetchone()[0]
    version=db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics) VALUES(%s,%s,1,'{}',%s,'2.0.0','photo_renderer_v2','accepted','{}') RETURNING id",(a,sid,'a'*64)).fetchone()[0]
    db.execute("UPDATE photo.styles SET name='Renamed',revision=revision+1 WHERE id=%s",(sid,))
    with pytest.raises(errors.CheckViolation),db.transaction():
        db.execute("UPDATE photo.style_versions SET config_json='{}'::jsonb || '{\"different\":true}'::jsonb WHERE id=%s",(version,))
    assert db.execute('SELECT version_no FROM photo.style_versions WHERE id=%s',(version,)).fetchone()[0]==1


def test_worker_cannot_directly_read_business_tables(db):
    tenant(db,'owner@example.test')
    db.execute('SET LOCAL ROLE photo_worker')
    with pytest.raises(errors.InsufficientPrivilege),db.transaction():db.execute('SELECT * FROM photo.assets')


def test_outbox_lease_fencing(db):
    from datetime import timedelta
    tid, _ = tenant(db, 'outbox@example.test')
    event = uuid4()
    row = db.execute("INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(%s,%s,'photo.commands.v1','io.verify','{}') RETURNING id", (tid,event)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_publisher')
    claimed = db.execute("SELECT id,event_id,lease FROM photo.claim_outbox_batch('task',10,60)").fetchall()
    assert len(claimed) == 1 and claimed[0][:2] == (row,event)
    lease = claimed[0][2]
    assert db.execute("SELECT * FROM photo.claim_outbox_batch('task',10,60)").fetchall() == []
    assert db.execute("SELECT photo.mark_outbox_sent('task',%s,%s)",(row,lease-timedelta(seconds=1))).fetchone()[0] is False
    assert db.execute("SELECT photo.mark_outbox_sent('task',%s,%s)",(row,lease)).fetchone()[0] is True
    assert db.execute("SELECT photo.mark_outbox_sent('task',%s,%s)",(row,lease)).fetchone()[0] is True
    with pytest.raises(errors.InsufficientPrivilege), db.transaction():
        db.execute('SELECT * FROM photo.outbox')
