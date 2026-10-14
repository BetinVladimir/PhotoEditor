from uuid import uuid4


def test_result_params_are_scheduler_scoped_and_unknown_result_denied(db):
    signature='photo.v27_exposure_adoption_params(uuid,uuid,integer,bigint,uuid)'
    for role in ('photo_scheduler','photo_worker','photo_api','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT * FROM photo.v27_exposure_adoption_params(%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),1,1,uuid4())).fetchall()==[]


def test_exact_canonical_result_params_and_revocation(db):
    from psycopg.types.json import Jsonb
    from test_v27_semantic_final_producer_gate import fixture
    tid,_,_,_,_,_,scope=fixture(db,snapshot_authority=True)
    run,task,params,output=uuid4(),uuid4(),uuid4(),uuid4()
    config=dict(project_id=str(scope['project']),approved_semantic=dict(nodes=[dict(task_id=str(task),params_artifact_id=str(params),params_sha='a'*64)]))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(run,tid,scope['snapshot'],Jsonb(config),'b'*64,scope['actor']))
    for aid in (params,output):
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','v1',clock_timestamp())",(aid,tid,str(aid),'a'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,state,worker_id,lease_until,deadline) VALUES(%s,%s,%s,'semantic.local_exposure:0','semantic.local_exposure','1','cpu','[]',%s,%s,%s,1,1,'running',gen_random_uuid(),clock_timestamp()+interval '60 seconds',clock_timestamp()+interval '120 seconds')",(task,tid,run,params,'b'*64,Jsonb(dict(backend='cpu',renderer_version='v2.2'))))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb([dict(artifact_id=str(output),sha256='a'*64)]),'c'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,worker_id=NULL,lease_until=NULL WHERE id=%s",(result,task))
    args=(tid,task,1,1,result)
    def read(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            rows=db.execute('SELECT * FROM photo.v27_exposure_adoption_params(%s,%s,%s,%s,%s)',values).fetchall()
            db.execute('RESET ROLE');return rows
    assert read()==[(params,'a'*64)]
    for index,value in ((0,uuid4()),(1,uuid4()),(2,2),(3,2),(4,uuid4())):
        changed=list(args);changed[index]=value;assert read(changed)==[]
    for statement,values in [
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(params,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(params,)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,values);assert read()==[]


def test_adoption_ready_requires_scheduler_and_committed_scope(db):
    from hashlib import sha256
    signature='photo.v27_exposure_adoption_ready(uuid,uuid,integer,bigint,uuid,uuid,text,text)'
    for role in ('photo_scheduler','photo_api','photo_worker','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    raw='{"schema_version":1}'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.v27_exposure_adoption_ready(%s,%s,%s,%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(raw.encode()).hexdigest(),raw)).fetchone()[0] is False


def test_adoption_protection_origin_is_result_scoped_scheduler_only(db):
    from hashlib import sha256
    signature='photo.v27_exposure_adoption_protection_origin(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,uuid,text)'
    for role in ('photo_scheduler','photo_api','photo_worker','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    raw='{"schema_version":1}'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.v27_exposure_adoption_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(raw.encode()).hexdigest(),raw,uuid4(),'a'*64,uuid4(),'b'*64)).fetchone()[0] is False


def test_protection_pin_discovery_requires_result_scope(db):
    from hashlib import sha256
    signature='photo.v27_exposure_adoption_protection_pin(uuid,uuid,integer,bigint,uuid,uuid,text,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    raw='{"schema_version":1}'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT * FROM photo.v27_exposure_adoption_protection_pin(%s,%s,%s,%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(raw.encode()).hexdigest(),raw)).fetchall()==[]


def test_report_coordinates_are_result_scoped_and_scheduler_only(db):
    from hashlib import sha256
    signature='photo.v27_exposure_adoption_report_evidence(uuid,uuid,integer,bigint,uuid,uuid,text,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
    raw='{"schema_version":1}'
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.v27_exposure_adoption_report_evidence(%s,%s,%s,%s,%s,%s,%s,%s)',
            (uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(raw.encode()).hexdigest(),raw)).fetchone()[0] is None


def test_exposure_commit_cannot_bypass_result_proofs(db):
    signature='photo.adopt_semantic_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,uuid,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,signature)).fetchone()[0] is (role=='photo_scheduler')
        assert not db.execute("SELECT has_function_privilege(%s,'photo.adopt_v2_plan_pre_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')",(role,)).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute("SELECT photo.adopt_semantic_exposure(%s,%s,1,1,%s,'[]',%s,%s,%s,%s,%s,%s,%s,%s)",
            (uuid4(),uuid4(),uuid4(),'a'*64,uuid4(),'b'*64,'{}',uuid4(),'c'*64,uuid4(),'d'*64)).fetchone()[0] is False
