from uuid import uuid4
import psycopg
from hashlib import sha256

def test_relight_lease_gate_acl_and_unregistered_refusal(database):
    signature='photo.v28_relight_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    with psycopg.connect(**database) as conn:
        privileges=conn.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE'),has_function_privilege('photo_api',%s,'EXECUTE'),has_function_privilege('photo_scheduler',%s,'EXECUTE'),has_function_privilege('public',%s,'EXECUTE')",(signature,)*4).fetchone()
        assert privileges==(True,False,False,False)
        security=conn.execute('SELECT p.prosecdef,r.rolname,p.proconfig FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=%s::regprocedure',(signature,)).fetchone()
        assert security==(True,'photo_fn_owner',['search_path=pg_catalog, photo'])
        args=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(b'{}').hexdigest(),'{}','b'*64)
        assert conn.execute('SELECT photo.v28_relight_lease_ready(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()==(False,)


def relight_lease_fixture(db,qualified=False,objects=None,calibration_raw=None,params_builder=None,extra_inputs_factory=None,source_builder=None,execution_resource_estimate=None,runtime_sha='b'*64,workflow_type=None,params_declared_bytes_delta=0,params_declared_version=None):
    """Owner-seeded SQL attempt fixture; no producer/qualification/IO acceptance."""
    from psycopg.types.json import Jsonb
    from test_v27_semantic_final_producer_gate import fixture
    tenant,template,_,_,_,_,scope=fixture(db,snapshot_authority=True,source_builder=source_builder,qualification_objects=objects)
    oldrun,oldparams,profile,inputs=db.execute('SELECT run_id,params_artifact_id,execution_profile,inputs FROM photo.tasks WHERE id=%s',(template,)).fetchone()
    extra=extra_inputs_factory(db,tenant,scope) if extra_inputs_factory else []
    profile['v2_entry_count']+=len(extra)
    qualification=None
    if qualified:
        from semantic_qualification_fixture import frozen_qualification
        calibration_ref=None
        if calibration_raw is not None:
            from io import BytesIO
            client,bucket=objects
            aid=uuid4();key='relight-calibration/'+str(aid);digest=sha256(calibration_raw).hexdigest()
            version=client.put_object(bucket,key,BytesIO(calibration_raw),len(calibration_raw),content_type='application/json').version_id
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(aid,tenant,bucket,key,version,digest,len(calibration_raw)))
            calibration_ref=dict(artifact_id=str(aid),sha256=digest)
        qualification=frozen_qualification(db,tenant,scope['project'],scope['actor'],operation='image.relight',backend='cpu',objects=objects,calibration_ref=calibration_ref,runtime_lock_sha=runtime_sha)
        qualification['policy_sha']=db.execute('SELECT photo.v28_relight_policy_sha(%s)',(qualification['policy_id'],)).fetchone()[0]
    from photo_worker.contracts.base import canonical_bytes
    raw=canonical_bytes({'calibration':qualification['calibration']}).decode() if qualified else '{}'
    if params_builder is not None:raw=params_builder(qualification,scope).decode()
    digest=sha256(raw.encode()).hexdigest();worker=uuid4();node='relight-'+str(uuid4())
    run,task,params=uuid4(),uuid4(),uuid4()
    if execution_resource_estimate is not None:profile['resource_estimate']=execution_resource_estimate
    profile.update(backend='cpu',model_lock_id='a'*64,relight_runtime_sha=runtime_sha)
    if qualified:
        profile.update(relight_qualification=qualification,v2_entry_count=profile['v2_entry_count']+2)
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) SELECT %s,tenant_id,'v2','running',input_snapshot_id,config_snapshot || %s,config_hash,created_by,'fail_fast',false FROM photo.runs WHERE id=%s",(run,Jsonb({'v28_relight':{'auth_session_id':str(scope['session'])}}),oldrun))
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) SELECT tenant_id,project_id,%s,input_snapshot_id,coalesce(%s,workflow_type),renderer_version,policy_sha FROM photo.v2_workflows WHERE run_id=%s",(run,workflow_type,oldrun))
    params_bucket,params_key,params_version='fixture',str(params),'v1'
    if objects is not None:
        from io import BytesIO
        client,params_bucket=objects
        params_version=client.put_object(params_bucket,params_key,BytesIO(raw.encode()),len(raw.encode()),content_type='application/json').version_id
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(params,tenant,params_bucket,params_key,params_version if params_declared_version is None else params_declared_version,digest,len(raw.encode())+params_declared_bytes_delta))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,worker_id,lease_until,deadline) VALUES(%s,%s,%s,'relight:0','image.relight','1','cpu','running',%s,%s,%s,%s,1,1,%s,clock_timestamp()+interval '60 seconds',clock_timestamp()+interval '120 seconds')",(task,tenant,run,Jsonb(inputs),params,'b'*64,Jsonb(profile),worker))
    db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) SELECT tenant_id,project_id,%s,CASE WHEN artifact_id=%s THEN %s ELSE artifact_id END,CASE WHEN artifact_id=%s THEN %s ELSE sha256 END,contract,ordinal FROM photo.v2_task_inputs WHERE task_id=%s",(task,oldparams,params,oldparams,digest,template))
    if qualified:
        ordinal=db.execute('SELECT max(ordinal)+1 FROM photo.v2_task_inputs WHERE task_id=%s',(task,)).fetchone()[0]
        for index,(contract,key) in enumerate([('Report','report'),('Calibration','calibration')]):
            ref=qualification[key]
            db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,scope['project'],task,ref['artifact_id'],ref['sha256'],contract,ordinal+index))
    ordinal=db.execute('SELECT max(ordinal)+1 FROM photo.v2_task_inputs WHERE task_id=%s',(task,)).fetchone()[0]
    for index,(ref,extra_sha,contract) in enumerate(extra):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,scope['project'],task,ref,extra_sha,contract,ordinal+index))
    db.execute('UPDATE photo.runs SET graph_sealed=true WHERE id=%s',(run,))
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,0,2000000000)',(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"image.relight\":[\"cpu\"]}','[\"v2.2\"]')",(tenant,worker,'a'*64,node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'cpu1','cpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tenant,task))
    args=(tenant,run,task,1,1,worker,params,digest,raw,'b'*64)
    return args,scope,qualification


def test_relight_live_metadata_lease_and_revocation_matrix(db):
    args,_,_=relight_lease_fixture(db)
    tenant,run,task,_,_,worker,params,_,_,_=args
    def ready(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            value=db.execute('SELECT photo.v28_relight_lease_ready('+','.join(['%s']*10)+')',values).fetchone()[0]
            db.execute('RESET ROLE')
            return value
    assert ready() is True
    for query,values in (
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,)),
        ("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(task,)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,)),
        ('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s',(task,)),
        ("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s",(task,)),
        ('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,)),
        ("UPDATE photo.worker_registrations SET db_principal='another_principal' WHERE worker_id=%s",(worker,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ("UPDATE photo.tasks SET execution_profile=execution_profile-'relight_runtime_sha' WHERE id=%s",(task,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(params,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values)
            assert ready() is False
        assert ready() is True
    for index,value in ((0,uuid4()),(3,2),(4,2),(5,uuid4()),(7,'0'*64),(9,'0'*64)):
        altered=list(args);altered[index]=value
        assert ready(altered) is False
    assert ready() is True
