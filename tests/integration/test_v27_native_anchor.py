"""Native anchor SQL metadata/recipe-head binding; no GPU/byte qualification."""
from copy import deepcopy
from hashlib import sha256
import json
from uuid import uuid4, uuid5

import pytest
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from test_v27_native_source_authority import seed_native_source_authority


def seed_native_anchor(db,operation,stage, *, extra_target=False):
    packet,case,_ = seed_native_source_authority(db,operation)
    tenant,project,actor,session = [case[key] for key in ['Tenant','Project','Actor','Session']]
    binding = packet['binding']
    parent = uuid4()
    run = uuid5(parent,'semantic.'+operation+':reference')
    mask = json.loads(case['Params'])['mask']['mask']
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,100,'application/json','v1',clock_timestamp(),'document')",(mask['artifact_id'],tenant,str(uuid4()),mask['sha256']))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,user_mask_binding) VALUES(%s,%s,%s,%s,%s,%s,100,'test','ready','v1',clock_timestamp(),%s)",(tenant,uuid4(),project,binding['asset_version_id'],mask['artifact_id'],mask['sha256'],Jsonb(dict(source=binding['source'],canvas_hash=binding['canvas_hash'],mask={}))))
    recipe = case['Artifact']
    stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(tenant,project,binding['asset_version_id'])).fetchone()[0]
    document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",(tenant,project,stream,recipe['artifact_id'],recipe['sha256'])).fetchone()[0]
    db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s',(document,stream))
    intent = dict(schema_version=1,kind='local_exposure',reference=binding,reference_mask=mask,target_snapshot_id=case['Snapshot'],strength=.5,ev=0,preserve_persons=False,prompt=None)
    snapshot = dict(schema_version=1,snapshot_id=case['Snapshot'],items=[dict(ordinal=0,binding=binding,working_image=binding['source'],recipe=recipe)])
    if extra_target:
        original_snapshot = case['Snapshot']
        case['Snapshot'] = str(db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,2,clock_timestamp()+interval '1 hour') RETURNING id",(tenant,actor,'d'*64)).fetchone()[0])
        snapshot['snapshot_id'] = case['Snapshot']
        intent['target_snapshot_id'] = case['Snapshot']
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) SELECT tenant_id,%s,ordinal,asset_version_id,artifact_id,sha256,payload FROM photo.snapshot_items WHERE tenant_id=%s AND snapshot_id=%s",(case['Snapshot'],tenant,original_snapshot))
        asset = db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) SELECT tenant_id,project_id,original_artifact_id,'{}','verified','{}' FROM photo.assets WHERE tenant_id=%s AND id=(SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s) RETURNING id", (tenant,tenant,binding['asset_version_id'])).fetchone()[0]
        version = db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id", (tenant,asset,binding['source']['artifact_id'],binding['source']['sha256'])).fetchone()[0]
        target = dict(binding,asset_version_id=str(version))
        db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,1,%s,%s,%s,%s)", (tenant,case['Snapshot'],version,binding['source']['artifact_id'],binding['source']['sha256'],Jsonb(dict(canvas_hash=binding['canvas_hash']))))
        db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(case['Snapshot'],))
        target_stream = db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(tenant,project,version)).fetchone()[0]
        target_document = db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",(tenant,project,target_stream,recipe['artifact_id'],recipe['sha256'])).fetchone()[0]
        db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE id=%s',(target_document,target_stream))
        snapshot['items'].append(dict(ordinal=1,binding=target,working_image=target['source'],recipe=recipe))
    def publish_metadata(label,body):
        text = canonical_bytes(body).decode()
        ref = dict(artifact_id=str(uuid4()),sha256=sha256(text.encode()).hexdigest())
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,'test',%s,%s,%s,'application/json','v1',clock_timestamp(),'document')",(ref['artifact_id'],tenant,'tenants/'+tenant+'/v2-drafts/'+ref['artifact_id'],ref['sha256'],len(text.encode())))
        db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'test','ready','v1',clock_timestamp())",(tenant,uuid5(parent,label),project,binding['asset_version_id'],ref['artifact_id'],ref['sha256'],len(text.encode())))
        return ref,text
    intent_ref,intent_text = publish_metadata('semantic.edit_intent',intent)
    snapshot_ref,snapshot_text = publish_metadata('semantic.input_snapshot',snapshot)
    request_sha = sha256(canonical_bytes(dict(actor_id=actor,intent=intent,tenant_id=tenant))).hexdigest()
    native = dict(schema_version=1,parent_run_id=str(parent),request_sha=request_sha,auth_session_id=session,source=binding,intent=intent_ref,snapshot=snapshot_ref)
    packet.update(run_id=str(run),parent_run_id=str(parent),request_sha=request_sha,anchor=dict(intent_text=intent_text,snapshot_text=snapshot_text),config_text=canonical_bytes(dict(project_id=project,**{'v27_'+stage+'_prepare':native})).decode())
    return packet,case,stream


@pytest.mark.parametrize('operation,stage',[('face.detect','detector'),('mask.person','person')])
def test_native_anchor_exact_then_changed_recipe(db,operation,stage):
    packet,case,stream = seed_native_anchor(db,operation,stage)
    tenant,run = case['Tenant'],packet['run_id']
    snapshot = json.loads(packet['anchor']['snapshot_text'])
    def lock(candidate=packet,child=run):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            result = db.execute('SELECT photo.lock_semantic_native_admission_anchor(%s,%s,%s)',(tenant,child,Jsonb(candidate))).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert lock()
    assert not lock(child=uuid4())
    for field in ['parent_run_id','request_sha','session_id','project_id']:
        changed = deepcopy(packet)
        changed[field] = 'f'*64 if field=='request_sha' else str(uuid4())
        assert not lock(changed),field
    changed = deepcopy(packet)
    altered = deepcopy(snapshot)
    altered['items'][0]['ordinal']=1
    changed['anchor']['snapshot_text']=canonical_bytes(altered).decode()
    assert not lock(changed)
    db.execute('UPDATE photo.v2_document_streams SET current_document_id=NULL WHERE id=%s',(stream,))
    assert not lock()
