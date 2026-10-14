"""Canonical interactive manual producer metadata; no pixel IO or full workflow."""
from uuid import uuid4
from hashlib import sha256
from psycopg.types.json import Jsonb
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import Canvas,TransformChain,MaskRef
from photo_worker.contracts.manual_mask import MaskRasterParams,ManualMask,RasterizedMask
from test_v28_relight_lease import relight_lease_fixture


def seed_manual_inputs(db,tenant,scope,retained,*,physical=False,publish=None,external=False,ambiguous=False):
    run,task,params=uuid4(),uuid4(),uuid4()
    source=ArtifactRef(artifact_id=scope['source'],sha256=scope['source_sha'])
    canvas=Canvas(width=5,height=5,orientation=1,space_id='oriented',source_sha256=scope['original_sha'])
    shapes=[] if not physical else [dict(kind='stroke',sequence=0,mode='add',points=[dict(x=.5,y=.5)],radius=.3,hardness=.5),dict(kind='stroke',sequence=1,mode='protect',points=[dict(x=.2,y=.2)],radius=.2,hardness=1.)]
    vectors=ManualMask.model_validate_json(canonical_bytes(dict(canvas=canvas.model_dump(mode='json'),source_sha256=scope['original_sha'],shapes=shapes)))
    vector_ref=None;vector_raw=None
    if external:
        vector_raw=canonical_bytes(vectors);aid=uuid4();digest=sha256(vector_raw).hexdigest()
        bucket,key,version=publish(aid,vector_raw,'ManualMask') if publish else ('fixture',str(aid),'v1')
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(aid,tenant,bucket,key,version,digest,len(vector_raw)))
        vector_ref=ArtifactRef(artifact_id=aid,sha256=digest)
    declared=[source.model_dump(mode='json')]+([vector_ref.model_dump(mode='json')] if vector_ref else [])
    original=MaskRasterParams(image=source,vectors=vector_ref,inline_vectors=None if external else vectors)
    raw=canonical_bytes(original);params_sha=sha256(raw).hexdigest()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,%s,'mask','running',%s,%s,%s,%s,true)",(run,tenant,scope['snapshot'],Jsonb(dict(asset_version_id=str(scope['version']),source_sha256=scope['source_sha'])),'a'*64,scope['actor']))
    bucket,key,version=publish(params,raw,'Params') if publish else ('fixture',str(params),'v1')
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json',clock_timestamp())",(params,tenant,bucket,key,version,params_sha,len(raw)))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'manual:0','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1)",(task,tenant,run,Jsonb(declared),params,'b'*64))
    pins={};raw_objects={params:raw};extra=[(params,params_sha,'Params')]
    if vector_ref:
        raw_objects[vector_ref.artifact_id]=vector_raw;extra.append((vector_ref.artifact_id,vector_ref.sha256,'ManualMask'))
    def artifact(role,raw,contract,media='application/json'):
        aid=uuid4();digest=sha256(raw).hexdigest()
        bucket,key,version=publish(aid,raw,role) if publish else ('fixture',str(aid),'v1')
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s,1,1)",(aid,tenant,bucket,key,version,digest,len(raw),media,role,task))
        pin=ArtifactRef(artifact_id=aid,sha256=digest);pins[role]=pin;raw_objects[aid]=raw
        if contract:extra.append((aid,digest,contract))
        return pin
    target_raw,guard_raw=b't'*228,b'g'*228
    if physical:
        from io import BytesIO
        import numpy as np
        from photo_worker.algorithms.manual_mask import rasterize
        tile=next(rasterize(vectors,scope['original_sha']))
        def npy(array):
            stream=BytesIO();np.save(stream,array,allow_pickle=False);return stream.getvalue()
        target_raw,guard_raw=npy(tile.target),npy(tile.protected)
    target_pixel=artifact('mask_target',target_raw,'NpyArray','application/octet-stream')
    guard_pixel=artifact('mask_protected',guard_raw,'NpyArray','application/octet-stream')
    def mask(pixel,protected):return MaskRef(artifact=pixel,canvas=canvas,transform_chain=TransformChain.create([]),selector='manual',confidence=1,model_lock_id=None,protected=protected)
    target,guard=mask(target_pixel,False),mask(guard_pixel,True)
    target_ref=artifact('mask_target_ref',canonical_bytes(target),'MaskRef')
    guard_ref=artifact('mask_guard_ref',canonical_bytes(guard),'MaskRef')
    root=RasterizedMask(image=source,vectors=vector_ref or ArtifactRef(artifact_id=params,sha256=params_sha),target=target,protected=guard,
        target_png=artifact('mask_target_png',b'tpng',None,'image/png'),protected_png=artifact('mask_protected_png',b'gpng',None,'image/png'))
    root_raw=canonical_bytes(root);root_ref=artifact('rasterized_mask',root_raw,'RasterizedMask')
    if ambiguous:
        artifact('rasterized_mask',root_raw,'RasterizedMask')
    db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,operation,source_artifact_id,declared_inputs,params,params_sha256,backend) VALUES(%s,%s,%s,'mask_manual','mask.rasterize',%s,%s,%s,%s,'cpu')",(tenant,run,task,scope['source'],Jsonb(declared),Jsonb(original.model_dump(mode='json')),params_sha))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,uuid4()))
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([p.model_dump(mode='json') for p in pins.values()]+([root_ref.model_dump(mode='json')] if ambiguous else [])),'d'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    scope.update(subject_ref=target_ref.model_dump(mode='json'),protected_ref=guard_ref.model_dump(mode='json'))
    retained.update(raw_objects=raw_objects,canvas=canvas,run=run,task=task,params=params,params_sha=params_sha,root=root_ref,root_raw=root_raw,target_raw=canonical_bytes(target),guard_raw=canonical_bytes(guard),pins=pins)
    return extra


@pytest.mark.parametrize('protected',[False,True])
def test_positive_manual_origin_and_child_revocation(db,protected):
    retained={}
    def setup(db,tenant,scope):return seed_manual_inputs(db,tenant,scope,retained)
    def params(pin,scope):return canonical_bytes(dict(base=dict(asset_version_id=str(scope['version']),source=dict(artifact_id=str(scope['source']),sha256=scope['source_sha']),canvas_hash='c'*64),subject=scope['subject_ref'],protected=scope['protected_ref']))
    args,scope,_=relight_lease_fixture(db,params_builder=params,extra_inputs_factory=setup)
    mask_raw=retained['guard_raw'] if protected else retained['target_raw']
    def origin(root=retained['root_raw'],mask=mask_raw):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            value=db.execute('SELECT photo.v28_worker_relight_manual_origin('+','.join(['%s']*13)+')',(*args,mask.decode(),None if root is None else root.decode(),protected)).fetchone()[0]
            db.execute('RESET ROLE');return value
    actual=origin()
    assert actual==dict(root=retained['root'].model_dump(mode='json'),params=dict(artifact_id=str(retained['params']),sha256=retained['params_sha']))
    assert origin(None)==actual
    assert origin(b' '+retained['root_raw']) is None
    assert origin(mask=b' '+mask_raw) is None
    mutations=[('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(retained['task'],)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(retained['run'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(retained['params'],)),
        ("UPDATE photo.asset_versions SET review_state='review' WHERE id=%s",(scope['version'],))]
    for role in ('mask_target_ref' if not protected else 'mask_guard_ref','mask_target','mask_protected','rasterized_mask'):
        mutations.append(('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(retained['pins'][role].artifact_id,)))
    for query,values in mutations:
        with db.transaction(force_rollback=True):
            db.execute(query,values);assert origin() is None
        assert origin()==actual
