"""Canonical mask.rasterize metadata lineage; binary object IO is separate."""
import json
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from photo_worker.contracts.base import canonical_bytes,canonical_hash


def test_raster_protection_requires_canonical_same_source_producer(db):
    canvas=dict(width=5,height=5,orientation=1,space_id='source',source_sha256='a'*64)
    chain=dict(steps=[],hash=canonical_hash(dict(steps=[])))
    source=canonical_bytes(dict(canvas=canvas,transform_chain=chain));source_sha=sha256(source).hexdigest()
    case=seed_original_composition(db,source_object=dict(bucket='test',key=str(uuid4()),version='v1',bytes=len(source),sha256=source_sha),canvas_hash=canonical_hash(canvas))
    tenant,project,actor,session=(case[k] for k in ('Tenant','Project','Actor','Session'))
    binding=json.loads(case['Params'])['base']
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'preview','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(tenant,case['Snapshot'],Jsonb(dict(project_id=project)),'a'*64,actor)).fetchone()[0]
    producer=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'mask.rasterize:0','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1) RETURNING id",(tenant,run,Jsonb([binding['source']]),case['Artifact']['artifact_id'],'b'*64)).fetchone()[0]
    def artifact(pin,size,role,media):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,%s,%s,%s,clock_timestamp(),%s,1,1) RETURNING id",(tenant,str(uuid4()),pin,size,media,role,producer)).fetchone()[0]
    binary=artifact('c'*64,228,'mask_protected','application/octet-stream')
    mask=dict(artifact=dict(artifact_id=str(binary),sha256='c'*64),canvas=canvas,transform_chain=chain,object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=True)
    raw=canonical_bytes(mask).decode();pin=sha256(raw.encode()).hexdigest()
    root=artifact(pin,len(raw),'mask_protected_ref','application/json')
    outputs=[dict(artifact_id=str(root),sha256=pin),dict(artifact_id=str(binary),sha256='c'*64)]
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,producer,Jsonb(outputs),'d'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
    body=dict(recipe=dict(protected_mask=outputs[0]),scalars=dict(source=binding))
    node=dict(protection_jcs=raw,source_jcs=source.decode())
    query='SELECT photo.v26_render_protection_scope('+','.join(['%s']*7)+')'
    def ready():return db.execute(query,(tenant,project,actor,session,Jsonb(body),Jsonb(node),None)).fetchone()[0]
    assert ready() is True
    for statement,args in (
        ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(binary,)),
        ('UPDATE photo.artifacts SET produced_attempt=2 WHERE id=%s',(root,)),
        ("UPDATE photo.artifacts SET output_role='mask_target' WHERE id=%s",(binary,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(binary,)),
        ("UPDATE photo.tasks SET operation='fixture.other' WHERE id=%s",(producer,)),
        ("UPDATE photo.tasks SET state='blocked' WHERE id=%s",(producer,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(statement,args)
            assert ready() is False
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.tasks SET inputs='[]'::jsonb WHERE id=%s",(producer,))
        assert ready() is False
    original_version=binding['asset_version_id']
    binding['asset_version_id']=str(uuid4())
    assert ready() is False
    binding['asset_version_id']=original_version
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=%s AND project_id=%s',(tenant,project))
        assert ready() is False
    # Result rows are immutable: recreate the controlled fixture rather than UPDATE.
    with db.transaction(force_rollback=True):
        other=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,2,2,%s,'{}',%s) RETURNING id",(tenant,producer,Jsonb(outputs[:1]),'e'*64)).fetchone()[0]
        db.execute('UPDATE photo.tasks SET canonical_result_id=%s,attempt=2,fencing_token=2 WHERE id=%s',(other,producer))
        db.execute('UPDATE photo.artifacts SET produced_attempt=2,produced_fence=2 WHERE id IN(%s,%s)',(root,binary))
        assert ready() is False
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    assert ready() is False
    for name in ('v26_render_protection_scope','v26_render_user_protection_scope','v26_render_raster_protection_scope'):
        for role in ('photo_api','photo_scheduler','photo_worker'):
            assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",(role,'photo.'+name+'(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid)')).fetchone()==(False,)
