from uuid import uuid4
from hashlib import sha256
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v28_relight_lease import relight_lease_fixture


def complete_relight_task(db,args,*,root_id=None,root_body=b'{}',root_location=None,review_required=False):
    tenant,run,task,_,_,worker,*_=args
    root=root_id or uuid4();bucket,key,version=root_location or ('fixture',str(root),'v1')
    digest=sha256(root_body).hexdigest();size=len(root_body)
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json','document',clock_timestamp(),%s,1,1)",(root,tenant,bucket,key,version,digest,size,task))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,worker))
    db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document',%s,%s,%s,%s,'application/json','ready',%s)",(tenant,task,worker,'a'*64,uuid4(),root,bucket,key,digest,size,version))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest,review_required) VALUES(%s,%s,1,1,%s,'{}',%s,%s) RETURNING id",(tenant,task,Jsonb([dict(artifact_id=str(root),sha256=digest)]),'e'*64,review_required)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s,lease_until=NULL,worker_id=NULL WHERE id=%s",(result,task))
    db.execute('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,))
    return result,root


def test_completed_relight_source_qualification_without_worker_lease(db):
    def build(pin,scope):return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),calibration=pin['calibration']))
    args,scope,pin=relight_lease_fixture(db,qualified=True,params_builder=build)
    tenant,run,task,_,_,worker,*_=args
    result,root=complete_relight_task(db,args)
    def evidence(body=args[8]):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            value=db.execute('SELECT photo.v28_relight_adoption_source(%s,%s,1,1,%s,%s)',(tenant,task,result,body)).fetchone()[0]
            db.execute('RESET ROLE');return value
    actual=evidence()
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        discovered=db.execute('SELECT * FROM photo.v28_relight_adoption_params(%s,%s,1,1,%s)',(tenant,task,result)).fetchone()
        db.execute('RESET ROLE')
    assert discovered==(args[6],args[7])
    assert actual is not None and actual['result_id']==str(result) and actual['root_ref']['artifact_id']==str(root)
    assert evidence(' '+args[8]) is None
    for query,values in (
        ("UPDATE photo.output_uploads SET state='cleaning' WHERE artifact_id=%s",(root,)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(root,)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(args[6],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert evidence() is None
        assert evidence()==actual
    sig='photo.v28_relight_adoption_source(uuid,uuid,integer,bigint,uuid,text)'
    for role in ('photo_scheduler','photo_worker','photo_api','public'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,sig)).fetchone()[0] is (role=='photo_scheduler')
