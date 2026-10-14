"""Full JSON documents with controlled ingest/qualification metadata; no GPU proof."""
from hashlib import sha256
from pathlib import Path
from uuid import uuid4
import json
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from layer_original_fixture import seed_original_composition


def seed_full_render_freeze(db, *, bucket="test", publish_document=None):
    root=Path(__file__).resolve().parents[2]
    image=json.loads((root/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    pixels=uuid4();image['pixels']['artifact_id']=str(pixels)
    image_raw=canonical_bytes(image)
    versions={}
    def publish(key,raw):
        version=publish_document(key,raw) if publish_document is not None else 'v1'
        versions[key]=version
        return version
    image_version=publish('image',image_raw)
    case=seed_original_composition(db,source_object=dict(sha256=sha256(image_raw).hexdigest(),bucket=bucket,key='image',bytes=len(image_raw),version=image_version),canvas_hash=canonical_hash(image['canvas']))
    tid,actor,project,session=(case[k] for k in ('Tenant','Actor','Project','Session'))
    binding=json.loads(case['Params'])['base']
    asset=db.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tid,binding['asset_version_id'])).fetchone()[0]
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'preview','draft',%s,%s,%s,%s,'fail_fast') RETURNING id",(tid,case['Snapshot'],Jsonb(dict(project_id=project)),'a'*64,actor)).fetchone()[0]
    producer=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,'mask.rasterize:controlled-ingest','mask.rasterize','1','cpu','blocked',%s,%s,%s,'{}',1,1) RETURNING id",(tid,run,Jsonb([binding['source']]),case['Artifact']['artifact_id'],'b'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,%s,'pixels',%s,%s,'application/octet-stream','v1',now(),'working_rgb',%s,1,1)",(pixels,tid,bucket,image['pixels']['sha256'],128+4*image['canvas']['width']*image['canvas']['height']*3,producer))
    db.execute('UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE tenant_id=%s AND id=%s',(producer,tid,binding['source']['artifact_id']))
    bodies={'image':image_raw.decode()}
    def artifact(key,body,role='document'):
        raw=canonical_bytes(body);digest=sha256(raw).hexdigest();bodies[key]=raw.decode()
        version=publish(key,raw)
        ident=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,'application/json',%s,now(),%s) RETURNING id",(tid,bucket,key,digest,len(raw),version,role)).fetchone()[0]
        return dict(artifact_id=str(ident),sha256=digest),raw
    style=json.loads((root/'backend/internal/run/identity_style.json').read_text());style['quality']['status']='needs_review'
    style_ref,_=artifact('style',style,'style_candidate')
    style_id=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Freeze identity',%s) RETURNING id",(tid,project,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES(%s,%s,1,%s,%s,'2.0.0','photo_renderer_v2','accepted','{}',%s)",(tid,style_id,Jsonb(style),style_ref['sha256'],style_ref['artifact_id']))
    override_ref,override_raw=artifact('override',dict(schema_version=1,input_sha256=image['canvas']['source_sha256'],modules=dict(strength=.4,extensions=dict(background=dict(mode='keep',asset_id=None,blur_strength=0,protect_subject=True))),revision=1))
    layer_ref,layer_raw=artifact('layers',dict(schema_version=1,source=binding,layers=[]))
    for key,ref,raw in (('override',override_ref,override_raw),('layers',layer_ref,layer_raw)):
        db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,now())",(tid,uuid4(),project,binding['asset_version_id'],ref['artifact_id'],ref['sha256'],len(raw),bucket,versions[key]))
    recipe=dict(schema_version=3,source=binding,base_style=style_ref,session_corrections=None,learning_version_id=None,overrides=[override_ref],layer_stack=layer_ref,renderer_version='v2.2',parameter_delta=dict(ev=.1,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0))
    recipe_ref,_=artifact('draft',recipe)
    stream=db.execute("INSERT INTO photo.v2_document_streams(tenant_id,project_id,asset_version_id,stream_type) VALUES(%s,%s,%s,'draft') RETURNING id",(tid,project,binding['asset_version_id'])).fetchone()[0]
    document=db.execute("INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256) VALUES(%s,%s,%s,1,'EditingRecipeV3',%s,%s) RETURNING id",(tid,project,stream,recipe_ref['artifact_id'],recipe_ref['sha256'])).fetchone()[0]
    revision=db.execute('UPDATE photo.v2_document_streams SET current_document_id=%s WHERE tenant_id=%s AND id=%s RETURNING revision',(document,tid,stream)).fetchone()[0]
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tid,))
    return dict(Tenant=tid,Actor=actor,Session=session,Asset=str(asset),Stream=str(stream),Request=dict(document_id=str(document),expected_revision=str(revision),assume_profile=None),Objects=bodies)
