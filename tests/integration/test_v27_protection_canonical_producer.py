from uuid import uuid4
from psycopg.types.json import Jsonb
from test_v27_semantic_final_producer_gate import fixture


def test_canonical_protection_producer_is_source_and_fence_bound(db):
    tid,assembly,_,_,_,_,scope=fixture(db,snapshot_authority=True)
    run,params=db.execute('SELECT run_id,params_artifact_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()
    run=uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(run,tid,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),'b'*64,scope['actor']))
    task,root,child=uuid4(),uuid4(),uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'protection:0','mask.rasterize','1','cpu','[]',%s,%s,'{}',1,1)",(task,tid,run,params,'a'*64))
    for artifact,role,media,size,digest in ((root,'mask_protected_ref','application/json',100,'1'*64),(child,'mask_protected','application/octet-stream',256,'2'*64)):
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,%s,%s,'v1',clock_timestamp(),%s,%s,1,1)",(artifact,tid,str(artifact),digest,size,media,role,task))
    outputs=[dict(artifact_id=str(root),sha256='1'*64),dict(artifact_id=str(child),sha256='2'*64)]
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb(outputs),'3'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,'[]','{}',%s,'cpu')",(tid,run,task,scope['source'],'a'*64))
    args=(tid,scope['project'],scope['source'],'5'*64,root,'1'*64,child,'2'*64)
    def ready(values=args):return db.execute('SELECT photo.semantic_protection_producer_ready(%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
    assert ready() is True
    proposal_args=(tid,scope['project'],scope['actor'],scope['session'],scope['version'],scope['source'],'5'*64,root,'1'*64,child,'2'*64)
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tid),))
    def proposal(values=proposal_args):return db.execute('SELECT photo.semantic_proposal_protection_origin(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
    assert proposal() is True
    for index in range(11):
        changed=list(proposal_args);changed[index]='0'*64 if index in (6,8,10) else uuid4()
        assert proposal(tuple(changed)) is False
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.semantic_proposal_protection_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text)','EXECUTE')",(role,)).fetchone()[0] == (role=='photo_api')
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],));assert proposal() is False

    for index in range(8):
        changed=list(args);changed[index]='0'*64 if index in (3,5,7) else uuid4()
        assert ready(tuple(changed)) is False
    for query,values in (
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(root,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(child,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert ready() is False
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.semantic_protection_producer_ready(uuid,uuid,uuid,text,uuid,text,uuid,text)','EXECUTE')",(role,)).fetchone()[0]
