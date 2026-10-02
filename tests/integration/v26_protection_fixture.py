"""Publish and bind protection metadata via actual SQL API; no object byte IO."""
from hashlib import sha256
from uuid import uuid4
from photo_worker.contracts.base import canonical_bytes


def publish_user_protection(db, case, binding, source, *, protected=True):
    tenant,actor,session=(case[k] for k in ('Tenant','Actor','Session'))
    version=binding['asset_version_id'];width=source['canvas']['width'];height=source['canvas']['height']
    size=128+4*width*height
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
    db.execute('SET LOCAL ROLE photo_api')
    args=(tenant,version,uuid4(),actor,session,'c'*64,size,'test',width,height)
    binary=db.execute('SELECT * FROM photo.reserve_v25_user_mask_pixels('+','.join(['%s']*10)+')',args).fetchone()
    assert db.execute('SELECT photo.complete_v25_user_mask_pixels('+','.join(['%s']*13)+')',(*args[:5],binary[1],binary[2],'c'*64,size,'test','v1',width,height)).fetchone()==(True,)
    mask=dict(artifact=dict(artifact_id=str(binary[1]),sha256='c'*64),canvas=source['canvas'],transform_chain=source['transform_chain'],object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=protected)
    raw=canonical_bytes(mask).decode();pin=sha256(raw.encode()).hexdigest()
    args=(tenant,version,uuid4(),actor,session,pin,len(raw),'test')
    root=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*args[:5],root[1],root[2],pin,len(raw),'test','v1')).fetchone()==(True,)
    assert db.execute('SELECT photo.bind_v25_user_mask('+','.join(['%s']*9)+')',(tenant,version,actor,session,root[1],raw,binding['source']['artifact_id'],binding['source']['sha256'],binding['canvas_hash'])).fetchone()==(True,)
    db.execute('RESET ROLE')
    return raw,dict(artifact_id=str(root[1]),sha256=pin),mask['artifact']


def publish_legacy_raster_protection(db,case,binding,source):
    from psycopg.types.json import Jsonb
    tenant,project,actor=(case[k] for k in ('Tenant','Project','Actor'))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'preview','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(tenant,case['Snapshot'],Jsonb(dict(project_id=project)),'a'*64,actor)).fetchone()[0]
    producer=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'mask.rasterize:0','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1) RETURNING id",(tenant,run,Jsonb([binding['source']]),case['Artifact']['artifact_id'],'b'*64)).fetchone()[0]
    def artifact(pin,size,role,media):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,%s,%s,%s,clock_timestamp(),%s,1,1) RETURNING id",(tenant,str(uuid4()),pin,size,media,role,producer)).fetchone()[0]
    binary=artifact('c'*64,128+4*source['canvas']['width']*source['canvas']['height'],'mask_protected','application/octet-stream')
    mask=dict(artifact=dict(artifact_id=str(binary),sha256='c'*64),canvas=source['canvas'],transform_chain=source['transform_chain'],object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=True)
    raw=canonical_bytes(mask).decode();pin=sha256(raw.encode()).hexdigest();root=artifact(pin,len(raw),'mask_protected_ref','application/json')
    refs=[dict(artifact_id=str(root),sha256=pin),mask['artifact']]
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,producer,Jsonb(refs),'d'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
    return raw,refs[0],refs[1]
