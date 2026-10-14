"""Synthetic committed metadata for composition SQL admission predicates.

No object-store publication or parent-byte qualification is implied.
"""
from uuid import uuid4
from psycopg.types.json import Jsonb
from hashlib import sha256
from test_database import tenant
from test_v25_layer_params_shape import params
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.layers.resources import layer_resources


def seed_original_composition(db, *, source_object=None, canvas_hash=None, snapshot_canvas=False):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Composition fixture') RETURNING id",(tid,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
    def artifact(digest,media,role=None,size=100):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,%s,%s,'v1',clock_timestamp(),%s) RETURNING id",(tid,str(uuid4()),digest,size,media,role)).fetchone()[0]
    original=artifact('a'*64,'image/jpeg')
    source_sha='b'*64
    if source_object is None:
        source=artifact(source_sha,'application/json','working_image')
    else:
        source_sha=source_object['sha256']
        source=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,'application/json',%s,clock_timestamp(),'working_image') RETURNING id",(tid,source_object['bucket'],source_object['key'],source_sha,source_object['bytes'],source_object['version'])).fetchone()[0]
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,source_sha)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour') RETURNING id",(tid,actor,'d'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,%s)",(tid,snapshot,version,source,source_sha,Jsonb(dict(canvas_hash=canvas_hash or 'b'*64) if snapshot_canvas else {})))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    body=params();body['base']=dict(asset_version_id=str(version),source=dict(artifact_id=str(source),sha256=source_sha),canvas_hash=canvas_hash or 'b'*64)
    wire=canonical_bytes(body);digest=sha256(wire).hexdigest()
    params_id=artifact(digest,'application/json',size=len(wire))
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'test','ready','v1',clock_timestamp())",(tid,uuid4(),project,version,params_id,digest,len(wire)))
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=layer_resources(5,5,mask_refs=1,mask_nodes=1))
    return dict(Tenant=str(tid),Project=str(project),Snapshot=str(snapshot),Actor=str(actor),Session=str(session),Params=wire.decode(),Profile=canonical_bytes(profile).decode(),Artifact=dict(artifact_id=str(params_id),sha256=digest),Width=5,Height=5,Revoked=False)
