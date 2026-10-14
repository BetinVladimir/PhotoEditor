from uuid import uuid4
import psycopg
import pytest


def test_semantic_candidate_scope_hardened_scheduler_boundary(db):
    signature='photo.v27_semantic_candidate_scope(uuid,uuid,uuid,text)'
    row=db.execute('SELECT p.prosecdef,p.prokind,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='f' and row[2]=='photo_fn_owner'
    assert 'search_path=pg_catalog, photo' in row[3]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert not db.execute('SELECT photo.v27_semantic_candidate_scope(%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),'a'*64)).fetchone()[0]
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_api')
            db.execute('SELECT photo.v27_semantic_candidate_scope(%s,%s,%s,%s)',(uuid4(),uuid4(),uuid4(),'a'*64))


def test_semantic_candidate_positive_and_stale_metadata(db):
    from psycopg.types.json import Jsonb
    from test_database import tenant
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Semantic scope fixture') RETURNING id",(tid,)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id",(tid,actor,'a'*64)).fetchone()[0]
    for variant in ('valid','stale_fence','expired','cancelled','unadopted','wrong_output'):
        run,workflow,task,params,candidate=[uuid4() for _ in range(5)]
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','fixture-version',clock_timestamp())",(params,tid,str(params),'b'*64))
        db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,cancel_requested,failure_policy) VALUES(%s,%s,'v2','running',%s,%s,%s,%s,false,%s,'fail_fast')",(run,tid,snapshot,Jsonb({'project_id':str(project)}),'c'*64,actor,variant=='cancelled'))
        db.execute("INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,%s,'semantic','v2.2',%s)",(workflow,tid,project,run,snapshot,'d'*64))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,%s,'semantic.transfer:0','semantic.transfer','1','gpu','[]',%s,%s,1,1,'{\"renderer_version\":\"v2.2\",\"backend\":\"cuda\"}')",(task,tid,run,params,'e'*64))
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence,expires_at) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','fixture-version',clock_timestamp(),'document',%s,1,%s,CASE WHEN %s THEN clock_timestamp()-interval '1 second' ELSE NULL END)",(candidate,tid,str(candidate),'f'*64,task,1,False))
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document','fixture',%s,%s,2,'application/json','ready','fixture-version')",(tid,task,uuid4(),'a'*64,uuid4(),candidate,str(candidate),'f'*64))
        output={'artifact_id':str(candidate),'sha256':'f'*64}
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb([output]),'a'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
        db.execute('UPDATE photo.runs SET graph_sealed=true WHERE id=%s',(run,))
        if variant!='unadopted':
            db.execute("INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at) VALUES(%s,'v2-adopt',photo.v2_adoption_event_id(%s,%s),clock_timestamp())",(tid,task,result))
        if variant=='expired':
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(candidate,))
        if variant=='stale_fence':
            db.execute('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,))
        if variant=='wrong_output':
            candidate=uuid4()
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','fixture-version',clock_timestamp(),'document',%s,1,1)",(candidate,tid,str(candidate),'f'*64,task))
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:
            assert db.execute('SELECT photo.v27_semantic_candidate_scope(%s,%s,%s,%s)',(tid,workflow,candidate,'f'*64)).fetchone()[0] is (variant=='valid')
            assert not db.execute('SELECT photo.v27_semantic_candidate_scope(%s,%s,%s,%s)',(uuid4(),workflow,candidate,'f'*64)).fetchone()[0]
        finally:db.execute('RESET ROLE')


def test_semantic_params_loader_hardened_context_boundary(db):
    signature='photo.v27_semantic_adoption_params(uuid,uuid,integer,bigint,uuid)'
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')",(signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')",(signature,)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        assert db.execute('SELECT * FROM photo.v27_semantic_adoption_params(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchall()==[]
    finally:db.execute('RESET ROLE')


def test_semantic_protection_loader_scheduler_boundary(db):
    signature='photo.v27_semantic_adoption_protection(uuid,uuid,integer,bigint,uuid)'
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        assert db.execute('SELECT * FROM photo.v27_semantic_adoption_protection(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchall()==[]
    finally:db.execute('RESET ROLE')


def test_semantic_assembly_params_scheduler_boundary(db):
    signature='photo.v27_semantic_assembly_adoption_params(uuid,uuid,integer,bigint,uuid)'
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.v27_semantic_assembly_adoption_params(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchall()==[]


def test_semantic_producer_loader_scheduler_boundary(db):
    signature='photo.v27_semantic_assembly_producer(uuid,uuid,integer,bigint,uuid,uuid,text)'
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.v27_semantic_assembly_producer(%s,%s,1,1,%s,%s,%s)',
        (uuid4(),uuid4(),uuid4(),uuid4(),'a'*64)).fetchall()==[]


def test_semantic_snapshot_loader_scheduler_boundary(db):
    signature='photo.v27_semantic_assembly_snapshot(uuid,uuid,integer,bigint,uuid)'
    row=db.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
    assert row[0] and row[1]=='photo_fn_owner' and 'search_path=pg_catalog, photo' in row[2]
    assert db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')",(signature,)).fetchone()[0]
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT * FROM photo.v27_semantic_assembly_snapshot(%s,%s,1,1,%s)',(uuid4(),uuid4(),uuid4())).fetchall()==[]
