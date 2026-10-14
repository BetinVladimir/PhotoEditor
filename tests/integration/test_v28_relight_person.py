"""Canonical producer SQL metadata; no real person inference or pixel IO."""
from uuid import uuid4
from hashlib import sha256
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.contracts.images import Canvas,TransformChain,MaskRef
from photo_worker.contracts.person import PersonParams
from test_v28_relight_lease import relight_lease_fixture


def seed_person_inputs(db,tenant,scope,retained,objects=None,publish=None):
    run,task,params,pixels,root=[uuid4() for _ in range(5)]
    producer=PersonParams(image=ArtifactRef(artifact_id=scope['source'],sha256=scope['source_sha']),model_lock_id='a'*64)
    original=canonical_bytes(producer)
    canvas=Canvas(width=5,height=5,orientation=1,space_id='oriented',source_sha256=scope['original_sha'])
    person=MaskRef(artifact=ArtifactRef(artifact_id=pixels,sha256='a'*64),canvas=canvas,
        transform_chain=TransformChain.create([]),selector='person',confidence=1,model_lock_id='a'*64,protected=False)
    body=canonical_bytes(person);digest=sha256(body).hexdigest()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','completed',%s,'{}',%s,%s,'fail_fast')",(run,tenant,scope['snapshot'],'c'*64,scope['actor']))
    bucket,key,version='fixture',str(params),'producer-v1'
    if objects is not None:
        from io import BytesIO
        client,bucket=objects
        version=client.put_object(bucket,key,BytesIO(original),len(original),content_type='application/json',metadata={'sha256':sha256(original).hexdigest()}).version_id
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(params,tenant,bucket,key,version,sha256(original).hexdigest(),len(original)))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'person:0','mask.person','1','gpu','blocked','[]',%s,%s,'{}',1,1)",(task,tenant,run,params,'d'*64))
    params_key,params_version=key,version
    outputs=[]
    for aid,sha,size,role,media in [(pixels,'a'*64,228,'person_mask','application/octet-stream'),(root,digest,len(body),'person_mask_ref','application/json')]:
        object_bucket,object_key,object_version=publish(aid,body,role) if publish and aid==root else ('fixture',str(aid),'v1')
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,1,1)",(aid,tenant,object_bucket,object_key,object_version,sha,size,media,role,task))
        outputs.append(dict(artifact_id=str(aid),sha256=sha))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,uuid4()))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb(outputs),'e'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    scope['subject_ref']=outputs[-1]
    retained.update(original=original,version=params_version,key=params_key,task=task,params=params,root=root,pixels=pixels,body=body,result=result)
    return [(params,sha256(original).hexdigest(),'Params'),(pixels,'a'*64,'NpyArray'),(root,digest,'MaskRef')]

def test_positive_canonical_person_origin_and_substitutions(db):
    retained={}
    def setup(db,tenant,scope):return seed_person_inputs(db,tenant,scope,retained)
    def params(pin,scope):
        return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),subject=scope['subject_ref']))
    args,scope,_=relight_lease_fixture(db,params_builder=params,extra_inputs_factory=setup)
    def origin(body=retained['body']):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            value=db.execute('SELECT photo.v28_worker_relight_person_origin('+','.join(['%s']*11)+')',(*args,body.decode())).fetchone()[0]
            db.execute('RESET ROLE');return value
    assert db.execute('SELECT photo.v28_worker_relight_source_ready('+','.join(['%s']*10)+')',args).fetchone()==(True,)
    actual=origin()
    assert actual is not None and actual['artifact_id']==str(retained['params']) and actual['object_version']=='producer-v1'
    assert origin(b' '+retained['body']) is None
    for query,values in (
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['root'],)),
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pixels'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert origin() is None
        assert origin()==actual
