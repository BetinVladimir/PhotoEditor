"""Authored, unexecuted: structural upgrade, isolated resource kernel and real wire."""
from pathlib import Path
from uuid import uuid4
import psycopg
from psycopg.types.json import Jsonb
import pytest
from conftest import database, db
from test_v25_lens_objectstore import lens_objects
from test_database import tenant

ROOT=Path(__file__).resolve().parents[2]
# Landed target; an overlay runner must deliberately pin the work SQL instead.
PATCH=(ROOT/'backend/db/migrations/02596_native_scene_dispatch_identity.sql').read_text().split('-- +goose Down')[0]
SIG='photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'
OLD="IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse') AND t.operation_version='1'"
NEW="IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse','scene.parse','depth.predict') AND t.operation_version='1'"


def catalog(db):
    return db.execute('SELECT to_jsonb(p),pg_get_functiondef(p.oid) FROM pg_proc p WHERE oid=to_regprocedure(%s)',(SIG,)).fetchone()


def scoped_rows(connection, scope):
    tid,task,node,worker=scope
    # Closed fixture-owned tables only. No credentials or external tenant rows.
    result={}
    for table in ('tenants','memberships','tenant_usage','artifacts','snapshots','runs','tasks','worker_registrations','outbox','resource_reservations'):
        column='id' if table=='tenants' else 'tenant_id'
        result[table]=connection.execute('SELECT to_jsonb(t) FROM photo.'+table+' t WHERE '+column+'=%s ORDER BY to_jsonb(t)::text',(tid,)).fetchall()
    for table in ('resource_nodes','resource_slots'):
        result[table]=connection.execute('SELECT to_jsonb(t) FROM photo.'+table+' t WHERE node_id=%s ORDER BY to_jsonb(t)::text',(node,)).fetchall()
    assert len(result['tasks'])==1 and len(result['worker_registrations'])==1
    return result


@pytest.mark.migration_head(2595)
@pytest.mark.parametrize('drift',[None,'predicate','owner','public','scheduler','path','security'])
def test_upgrade_preserves_identity_rows_roles_and_refuses_drift(database,drift):
    with psycopg.connect(**database) as connection:
        scope=resource_case(connection,'scene.parse',dict(backend='cuda',renderer_version='v2.2',model_lock_sha='e'*64))
        if drift=='predicate':
            row,definition=catalog(connection)
            assert row['prosrc'].count(OLD)==1
            connection.execute(definition.replace(row['prosrc'],row['prosrc'].replace(OLD,OLD.replace('IF t.','IF  t.'))))
        elif drift=='owner':connection.execute('ALTER FUNCTION '+SIG+' OWNER TO photo_scheduler')
        elif drift in ('public','scheduler'):connection.execute('GRANT EXECUTE ON FUNCTION '+SIG+' TO '+('PUBLIC' if drift=='public' else 'photo_scheduler'))
        elif drift=='path':connection.execute('ALTER FUNCTION '+SIG+' SET search_path=pg_catalog')
        elif drift=='security':connection.execute('ALTER FUNCTION '+SIG+' SECURITY INVOKER')
        connection.commit()
        before,definition=catalog(connection)
        roles=connection.execute('SELECT oid,rolname,rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls FROM pg_roles ORDER BY oid').fetchall()
        memberships=connection.execute('SELECT to_jsonb(m) FROM pg_auth_members m ORDER BY to_jsonb(m)::text').fetchall()
        rows=scoped_rows(connection,scope)
        if drift:
            with pytest.raises(psycopg.errors.RaiseException,match='NATIVE_SCENE_DISPATCH_'):
                with connection.transaction():connection.execute(PATCH)
            assert catalog(connection)==(before,definition)
        else:
            connection.execute(PATCH)
            after,_=catalog(connection)
            assert after['prosrc']==before['prosrc'].replace(OLD,NEW)
            assert {k:v for k,v in after.items() if k!='prosrc'}=={k:v for k,v in before.items() if k!='prosrc'}
        assert connection.execute('SELECT oid,rolname,rolsuper,rolinherit,rolcreaterole,rolcreatedb,rolcanlogin,rolreplication,rolbypassrls FROM pg_roles ORDER BY oid').fetchall()==roles
        assert connection.execute('SELECT to_jsonb(m) FROM pg_auth_members m ORDER BY to_jsonb(m)::text').fetchall()==memberships
        assert scoped_rows(connection,scope)==rows


def test_fresh_head_closed_mapping_and_private_acl(db):
    row,_=catalog(db)
    assert row['prosrc'].count(NEW)==1 and OLD not in row['prosrc']
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway','photo_portal','photo_semantic_admission'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,SIG)).fetchone()==(False,)


def resource_case(db,operation,profile,version='1'):
    """Synthetic initial metadata for the private resource kernel only.

    No model inference, physical receipt, qualification or adopted result is
    manufactured. Full production authority is exercised by the wire case below.
    All task profile variants are authored at INSERT, never changed after seal.
    """
    tid,user=tenant(db,uuid4().hex+'@dispatch.test')
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tid,))
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES(%s,'test',%s,%s,2,'application/json') RETURNING id",(tid,uuid4().hex,'a'*64)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'input','user',%s,%s,0) RETURNING id",(tid,user,'b'*64)).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'apply','running',%s,'{}',%s,%s,true) RETURNING id",(tid,snapshot,'c'*64,user)).fetchone()[0]
    if version!='1':
        # The isolated owner registers the real alternate version before creating
        # any task. Copy actual deployed routing/model/backend tuple; do not
        # pretend absent registry authority is a version-mapping negative.
        assert version=='2'
        source=db.execute("SELECT resource_class,routing_key,model_id,timeout_seconds,backends FROM photo.operation_registry WHERE operation=%s AND version='1'",(operation,)).fetchone()
        assert source is not None and source[0]=='gpu' and source[2] is not None
        assert db.execute('SELECT count(*) FROM photo.operation_registry WHERE operation=%s AND version=%s',(operation,version)).fetchone()==(0,)
        db.execute('INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES(%s,%s,%s,%s,%s,%s,%s,false)',(operation,version,*source[:4],Jsonb(source[4])))
        assert db.execute('SELECT resource_class,routing_key,model_id,timeout_seconds,backends FROM photo.operation_registry WHERE operation=%s AND version=%s',(operation,version)).fetchone()==source
        assert db.execute('SELECT v2_enabled FROM photo.operation_registry WHERE operation=%s AND version=%s',(operation,version)).fetchone()==(False,)
    task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,'scene',%s,%s,'gpu','ready','[]',%s,%s,%s) RETURNING id",(tid,run,operation,version,params,'d'*64,Jsonb(profile))).fetchone()[0]
    node='scene-dispatch-'+uuid4().hex;worker=uuid4()
    db.execute("INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES(%s,true,10000000,10000000,10000000,10000000,10000000,10000000,clock_timestamp(),%s)",(node,'a'*64))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'gpu1','gpu','{}')",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,node_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,enabled,qualified_operations) VALUES(%s,%s,%s,'[\"gpu\"]','[\"cuda\"]',%s,clock_timestamp(),'{}',session_user,true,%s)",(tid,worker,node,'e'*64,Jsonb({operation:['cuda']})))
    return tid,task,node,worker


@pytest.mark.parametrize('operation',['scene.parse','depth.predict'])
@pytest.mark.parametrize('variant',['sha_only','both_equal','missing','malformed','wrong_type','conflicting','wrong_renderer','wrong_version','wrong_version_with_legacy_id','revoked_worker'])
def test_private_resource_kernel_model_pins_and_worker_refusal(db,operation,variant):
    profile=dict(backend='cuda',renderer_version='v2.2',model_lock_sha='e'*64)
    expected='queued'
    if variant=='both_equal':profile['model_lock_id']='e'*64
    elif variant=='missing':profile.pop('model_lock_sha');expected='unqualified_model'
    elif variant=='malformed':profile['model_lock_sha']='not-a-digest';expected='unqualified_model'
    elif variant=='wrong_type':profile['model_lock_sha']=42;expected='unqualified_model'
    elif variant=='conflicting':profile['model_lock_id']='f'*64;expected='unqualified_model'
    elif variant=='wrong_renderer':profile['renderer_version']='not-v2';expected='unqualified_model'
    elif variant=='wrong_version':expected='unqualified_model'
    elif variant=='wrong_version_with_legacy_id':profile['model_lock_id']='e'*64
    elif variant=='revoked_worker':expected='waiting_worker'
    tid,task,node,worker=resource_case(db,operation,profile,version='2' if variant.startswith('wrong_version') else '1')
    if variant=='revoked_worker':db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,))
    before=db.execute('SELECT (SELECT count(*) FROM photo.outbox WHERE tenant_id=%s),(SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s),(SELECT active_gpu_tasks FROM photo.tenant_usage WHERE tenant_id=%s)',(tid,task,tid)).fetchone()
    got=db.execute('SELECT * FROM photo.reserve_dispatch_resources(%s,%s,1024,1024,1024,%s,%s,\'\')',(task,node,uuid4(),uuid4())).fetchone()
    assert got[0]==expected
    if expected=='queued':
        assert got[1]['model']['lock_id']=='e'*64
        assert got[1]['model']['id']==('segformer_b0_ade' if operation=='scene.parse' else 'depth_anything_v2_small')
    else:
        assert got[1] is None
        assert db.execute('SELECT state FROM photo.tasks WHERE id=%s',(task,)).fetchone()==('ready',)
        assert db.execute('SELECT (SELECT count(*) FROM photo.outbox WHERE tenant_id=%s),(SELECT count(*) FROM photo.resource_reservations WHERE task_id=%s),(SELECT active_gpu_tasks FROM photo.tenant_usage WHERE tenant_id=%s)',(tid,task,tid)).fetchone()==before


def test_real_scene_depth_dispatch_worker_physical_results_and_recovery(database,lens_objects):
    from test_v27_semantic_intent_publication import test_semantic_intent_publication_old_version_and_replay
    test_semantic_intent_publication_old_version_and_replay(database,lens_objects,True,'native-scene-wire')
