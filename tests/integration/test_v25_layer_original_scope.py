"""Exact snapshot original provenance for null composition lineage."""
from copy import deepcopy
from uuid import uuid4
from psycopg.types.json import Jsonb
from test_database import tenant
from test_v25_layer_params_shape import params
from photo_worker.operations.layers.resources import layer_resources
from photo_worker.contracts.base import canonical_bytes
from hashlib import sha256


def test_v25_layer_original_scope_exact_binding_and_live_authority(db):
    tid,actor=tenant(db,str(uuid4())+'@example.test')
    project=db.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Composition') RETURNING id",(tid,)).fetchone()[0]
    session=db.execute("INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES(%s,%s,gen_random_uuid(),'web',clock_timestamp()+interval '1 hour') RETURNING id",(actor,uuid4().bytes)).fetchone()[0]
    def artifact(sha,media,role=None):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,'test',%s,%s,100,%s,'v1',clock_timestamp(),%s) RETURNING id",(tid,str(uuid4()),sha,media,role)).fetchone()[0]
    original=artifact('a'*64,'image/jpeg');source=artifact('b'*64,'application/json','working_image')
    asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tid,project,original)).fetchone()[0]
    version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tid,asset,source,'b'*64)).fetchone()[0]
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,clock_timestamp()+interval '1 hour') RETURNING id",(tid,actor,'d'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tid,snapshot,version,source,'b'*64))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    body=params();body['base']=dict(asset_version_id=str(version),source=dict(artifact_id=str(source),sha256='b'*64),canvas_hash='b'*64)
    args=(tid,project,snapshot,actor,session)
    query='SELECT photo.v25_layer_original_scope(%s,%s,%s,%s,%s,%s)'
    profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=layer_resources(5,5,mask_refs=1,mask_nodes=1))
    def scoped(candidate=body,identity=args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        result=db.execute(query,(*identity,Jsonb(candidate))).fetchone()[0]
        combined=db.execute('SELECT photo.v25_layer_original_preflight(%s,%s,%s,%s,%s,%s,5,5,%s)',(*identity,Jsonb(candidate),Jsonb(profile))).fetchone()[0]
        assert combined is result
        db.execute('RESET ROLE');return result
    assert scoped() is True
    wire=canonical_bytes(body);digest=sha256(wire).hexdigest()
    params_artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,'test',%s,%s,%s,'application/json','v1',clock_timestamp()) RETURNING id",(tid,str(uuid4()),digest,len(wire))).fetchone()[0]
    document=uuid4()
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'test','ready','v1',clock_timestamp())",(tid,document,project,version,params_artifact,digest,len(wire)))
    params_query='SELECT photo.v25_layer_original_params_scope(%s,%s,%s,%s,%s,%s,%s,%s,5,5,%s)'
    params_args=(*args,params_artifact,digest,wire.decode(),Jsonb(profile))
    def params_scoped(candidate=params_args):
        db.execute('SET LOCAL ROLE photo_scheduler')
        result=db.execute(params_query,candidate).fetchone()[0]
        db.execute('RESET ROLE');return result
    assert params_scoped() is True
    for field,value,restore in [('object_version','v2','v1'),('bucket','other','test'),('bytes',len(wire)+1,len(wire))]:
        # Column names are fixed fixture literals, never user input.
        db.execute(f'UPDATE photo.v2_draft_uploads SET {field}=%s WHERE document_id=%s',(value,document))
        assert params_scoped() is False
        db.execute(f'UPDATE photo.v2_draft_uploads SET {field}=%s WHERE document_id=%s',(restore,document))
    assert params_scoped() is True
    for index,value in [(5,uuid4()),(6,'c'*64),(7,wire.decode()+' ')]:
        wrong=list(params_args);wrong[index]=value
        assert params_scoped(wrong) is False
    db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE document_id=%s",(document,))
    assert params_scoped() is False
    db.execute("UPDATE photo.v2_draft_uploads SET state='ready' WHERE document_id=%s",(document,))
    assert params_scoped() is True
    db.execute('SET LOCAL ROLE photo_scheduler')
    for variant in ['ram','renderer','dimensions']:
        bad=deepcopy(profile);width=5
        if variant=='ram':bad['resource_estimate']['ram_bytes']-=1
        elif variant=='renderer':bad['renderer_version']='v2.1'
        else:width=6
        assert db.execute('SELECT photo.v25_layer_original_preflight(%s,%s,%s,%s,%s,%s,%s,5,%s)',(*args,Jsonb(body),width,Jsonb(bad))).fetchone()[0] is False
    db.execute('RESET ROLE')
    unselected_source=artifact('c'*64,'application/json','working_image')
    unselected_version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,version_no,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,2,%s,%s,'{}','accepted') RETURNING id",(tid,asset,unselected_source,'c'*64)).fetchone()[0]
    unselected=deepcopy(body)
    unselected['base']['asset_version_id']=str(unselected_version)
    unselected['base']['source']=dict(artifact_id=str(unselected_source),sha256='c'*64)
    assert scoped(unselected) is False
    db.execute('UPDATE photo.v2_draft_uploads SET asset_version_id=%s WHERE document_id=%s',(unselected_version,document))
    assert params_scoped() is False
    db.execute('UPDATE photo.v2_draft_uploads SET asset_version_id=%s WHERE document_id=%s',(version,document))
    for index in range(5):
        wrong=list(args);wrong[index]=uuid4()
        assert scoped(identity=wrong) is False
    for variant in ['parent','version','source','sha','missing']:
        wrong=deepcopy(body)
        if variant=='parent':wrong['source_result']=deepcopy(wrong['proposal_result'])
        elif variant=='version':wrong['base']['asset_version_id']=str(uuid4())
        elif variant=='source':wrong['base']['source']['artifact_id']=str(uuid4())
        elif variant=='sha':wrong['base']['source']['sha256']='c'*64
        else:del wrong['source_result']
        assert scoped(wrong) is False
    db.execute("UPDATE photo.asset_versions SET review_state='rejected' WHERE id=%s",(version,))
    assert scoped() is False
    assert params_scoped() is False
    db.execute("UPDATE photo.asset_versions SET review_state='accepted' WHERE id=%s",(version,))
    assert scoped() is True
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(source,))
    assert scoped() is False
    db.execute('UPDATE photo.artifacts SET expires_at=NULL WHERE id=%s',(source,))
    assert scoped() is True
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(session,))
    assert scoped() is False
    assert db.execute("SELECT has_function_privilege('photo_api','photo.v25_layer_original_scope(uuid,uuid,uuid,uuid,uuid,jsonb)','EXECUTE')").fetchone()[0] is False
