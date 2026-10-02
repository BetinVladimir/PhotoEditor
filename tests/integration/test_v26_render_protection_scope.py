"""Published user mask metadata scope; synthetic object versions, no pixel IO."""
import json
from copy import deepcopy
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from photo_worker.contracts.base import canonical_bytes, canonical_hash


def test_user_protection_scope_requires_published_binding_and_typed_inputs(db):
    canvas=dict(width=5,height=5,orientation=1,space_id='source',source_sha256='a'*64)
    chain=dict(steps=[],hash=canonical_hash(dict(steps=[])))
    source=canonical_bytes(dict(canvas=canvas,transform_chain=chain))
    source_sha=sha256(source).hexdigest()
    case=seed_original_composition(db,source_object=dict(sha256=source_sha,bucket='test',key=str(uuid4()),bytes=len(source),version='v1'),canvas_hash=canonical_hash(canvas))
    tenant,project,actor,session=(case[k] for k in ('Tenant','Project','Actor','Session'))
    binding=json.loads(case['Params'])['base'];version=binding['asset_version_id']
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tenant,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
    db.execute('SET LOCAL ROLE photo_api')
    binary_args=(tenant,version,uuid4(),actor,session,'c'*64,228,'test',5,5)
    binary=db.execute('SELECT * FROM photo.reserve_v25_user_mask_pixels('+','.join(['%s']*10)+')',binary_args).fetchone()
    assert db.execute('SELECT photo.complete_v25_user_mask_pixels('+','.join(['%s']*13)+')',(*binary_args[:5],binary[1],binary[2],binary_args[5],228,'test','v1',5,5)).fetchone()==(True,)
    mask=dict(artifact=dict(artifact_id=str(binary[1]),sha256='c'*64),canvas=canvas,transform_chain=chain,object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=True)
    raw=canonical_bytes(mask).decode();pin=sha256(raw.encode()).hexdigest()
    args=(tenant,version,uuid4(),actor,session,pin,len(raw),'test')
    root=db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
    assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*args[:5],root[1],root[2],pin,len(raw),'test','v1')).fetchone()==(True,)
    db.execute('RESET ROLE')
    body=dict(recipe=dict(protected_mask=dict(artifact_id=str(root[1]),sha256=pin)),scalars=dict(source=binding))
    node=dict(protection_jcs=raw,source_jcs=source.decode())
    query='SELECT photo.v26_render_protection_scope('+','.join(['%s']*7)+')'
    def ready(body_value=body,node_value=node,task=None):
        return db.execute(query,(tenant,project,actor,session,Jsonb(body_value),Jsonb(node_value),task)).fetchone()[0]
    assert ready() is False
    db.execute('SET LOCAL ROLE photo_api')
    assert db.execute('SELECT photo.bind_v25_user_mask('+','.join(['%s']*9)+')',(tenant,version,actor,session,root[1],raw,binding['source']['artifact_id'],source_sha,binding['canvas_hash'])).fetchone()==(True,)
    db.execute('RESET ROLE')
    assert ready() is True
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'v2','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(tenant,case['Snapshot'],Jsonb(dict(project_id=project)),'a'*64,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",(tenant,project,run,case['Snapshot'],'a'*64))
    task=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,'render.compose:0','render.compose','1','gpu','blocked','[]',%s,%s,'{}') RETURNING id",(tenant,run,root[1],'b'*64)).fetchone()[0]
    assert ready(task=task) is False
    for ordinal,(artifact,digest,contract) in enumerate(((root[1],pin,'MaskRef'),(binary[1],'c'*64,'NpyArray'))):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,task,artifact,digest,ordinal,contract))
    assert ready(task=task) is True
    for artifact in (root[1],binary[1]):
        with db.transaction(force_rollback=True):
            saved=db.execute('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND artifact_id=%s RETURNING tenant_id,project_id,task_id,artifact_id,sha256,ordinal',(task,artifact)).fetchone()
            db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,'CorrectionPlan')",saved)
            assert ready(task=task) is False
    altered=deepcopy(node);altered['protection_jcs']+=' '
    assert ready(node_value=altered) is False
    altered=deepcopy(node);altered['source_jcs']='{}'
    assert ready(node_value=altered) is False
    for field in ('canvas_hash','asset_version_id'):
        altered=deepcopy(body);altered['scalars']['source'][field]='f'*64 if field=='canvas_hash' else str(uuid4())
        assert ready(body_value=altered) is False
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(binary[1],))
        assert ready() is False
    from v26_protection_fixture import publish_user_protection
    unprotected_raw,unprotected_ref,_=publish_user_protection(db,case,binding,dict(canvas=canvas,transform_chain=chain),protected=False)
    altered=deepcopy(body);altered['recipe']['protected_mask']=unprotected_ref
    assert ready(body_value=altered,node_value=dict(protection_jcs=unprotected_raw,source_jcs=source.decode())) is False
    foreign=deepcopy(dict(canvas=canvas,transform_chain=chain));foreign['canvas']['width']=4
    foreign_raw,foreign_ref,_=publish_user_protection(db,case,binding,foreign)
    altered=deepcopy(body);altered['recipe']['protected_mask']=foreign_ref
    assert ready(body_value=altered,node_value=dict(protection_jcs=foreign_raw,source_jcs=source.decode())) is False
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    assert ready() is False
    for role in ('photo_api','photo_scheduler','photo_worker'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v26_render_protection_scope(uuid,uuid,uuid,uuid,jsonb,jsonb,uuid)','EXECUTE')",(role,)).fetchone()==(False,)
