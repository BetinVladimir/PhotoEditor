"""Real region Params publication; parent source/recipe metadata remain synthetic."""
from pathlib import Path
import json
import os
import subprocess
from hashlib import sha256
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from test_v23_record_correction import _fixture
from test_v25_lens_objectstore import lens_objects

ROOT=Path(__file__).resolve().parents[2]


def test_region_params_publication_replay_and_content_conflict(database,lens_objects):
    client,bucket=lens_objects
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            tenant,actor,session,project,request,_=_fixture(db)
            version=request['asset_version_id']
            asset=db.execute('SELECT asset_id FROM photo.asset_versions WHERE tenant_id=%s AND id=%s',(tenant,version)).fetchone()[0]
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tenant,))
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s",(tenant,))
        before=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()
        case={k:str(v) for k,v in dict(Tenant=tenant,Actor=actor,Session=session,Project=project,Asset=asset,Version=version).items()}
        case['Objects']=client._photo_test_config
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_REGION_PUBLISH_CASE=json.dumps(case),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2region','-run','^TestIntegrationRegionParamsPublication$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode==0,result.stdout+result.stderr
        assert db.execute("SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND operation='region.preview'",(tenant,)).fetchone()==(0,)
        assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v26_region'",(tenant,)).fetchone()==(0,)
        assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND key='region-disabled-admission'",(tenant,)).fetchone()==(0,)
        rows=db.execute("SELECT a.bytes,a.object_version,u.object_version,a.object_key,a.sha256 FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.bucket=%s AND u.state='ready'",(tenant,bucket)).fetchall()
        assert len(rows)==1
        size,version,upload_version,key,digest=rows[0]
        assert version==upload_version and version not in ('','null',None)
        response=client.get_object(bucket,key,version_id=version)
        try:payload=response.read()
        finally:response.close();response.release_conn()
        assert len(payload)==size and sha256(payload).hexdigest()==digest
        plan=json.loads(payload)
        assert plan['tenant_id']==str(tenant) and plan['request']['max_edge']==1024
        assert plan['render_order']=='full_canvas_then_crop_then_downscale'
        assert len(plan['inputs'])==4
        after=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()
        assert after==(before[0]+size,before[1])


@pytest.mark.parametrize("alpha",[False,True])
def test_live_region_preparation_pins_source_and_recipe_versions(database,lens_objects,alpha):
    from io import BytesIO
    from uuid import uuid4
    from psycopg.types.json import Jsonb
    client,bucket=lens_objects
    def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
    def store(key,value):
        body=encoded(value)
        stored=client.put_object(bucket,key,BytesIO(body),len(body),content_type='application/json',metadata={'sha256':sha256(body).hexdigest()})
        return body,sha256(body).hexdigest(),stored.version_id
    with psycopg.connect(**database,autocommit=True) as db:
        with db.transaction():
            tenant,actor,session,project,_,_=_fixture(db)
            canvas=dict(width=2000,height=1000,orientation=1,space_id='source',source_sha256='a'*64)
            chain=dict(steps=[],hash=sha256(encoded(dict(steps=[]))).hexdigest())
            image=dict(pixels=dict(artifact_id=str(uuid4()),sha256='b'*64),alpha=None,dtype='float32',canvas=canvas,color=dict(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='c'*64),transform_chain=chain)
            if alpha:image['alpha']=dict(artifact_id=str(uuid4()),sha256='9'*64)
            source_key='tenants/'+str(tenant)+'/region-source.json'
            source_body,source_sha,source_version=store(source_key,image)
            source=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json','source',now()) RETURNING id",(tenant,bucket,source_key,source_version,source_sha,len(source_body))).fetchone()[0]
            asset=db.execute("INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES(%s,%s,%s,'{}','verified','{}') RETURNING id",(tenant,project,source)).fetchone()[0]
            version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,asset,source,source_sha)).fetchone()[0]
            snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id",(tenant,actor,'a'*64)).fetchone()[0]
            run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,'v2','running',%s,%s,%s,%s,'fail_fast',true) RETURNING id",(tenant,snapshot,Jsonb(dict(project_id=str(project))),'b'*64,actor)).fetchone()[0]
            db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'learning','v2.2',%s)",(tenant,project,run,snapshot,'c'*64))
            recipe=dict(schema_version=1,source=dict(asset_version_id=str(version),source=dict(artifact_id=str(source),sha256=source_sha),canvas_hash=sha256(encoded(canvas)).hexdigest()),base_style=dict(artifact_id=str(uuid4()),sha256='d'*64),effective_delta=dict(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0),renderer_version='v2.2',icc_lock_sha='e'*64,model_lock_sha='f'*64)
            recipe_key='tenants/'+str(tenant)+'/region-recipe.json'
            recipe_body,recipe_sha,recipe_version=store(recipe_key,recipe)
            recipe_id=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json','v23_frozen_recipe',now()) RETURNING id",(tenant,bucket,recipe_key,recipe_version,recipe_sha,len(recipe_body))).fetchone()[0]
            db.execute("INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,'v23_frozen_base',%s,%s,%s,%s,%s,'ready',%s,now())",(tenant,run,uuid4(),recipe_id,uuid4(),recipe_sha,len(recipe_body),bucket,recipe_version))
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s) ON CONFLICT DO NOTHING',(tenant,))
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','100000000') WHERE id=%s",(tenant,))
        # Latest versions are deliberately corrupt. Preparation must read the SQL pins.
        for key in (source_key,recipe_key):client.put_object(bucket,key,BytesIO(b'{}'),2,content_type='application/json')
        case={k:str(v) for k,v in dict(Tenant=tenant,Actor=actor,Session=session,Project=project,Asset=asset,Version=version).items()}
        case['Objects']=client._photo_test_config
        case['Request']=dict(version_id=str(version),recipe=dict(artifact_id=str(recipe_id),sha256=recipe_sha),rect=dict(x0=0,y0=0,x1=1,y1=1),max_edge=1024)
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_REGION_PUBLISH_CASE=json.dumps(case),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2region','-run','^TestIntegrationRegionParamsPublication$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=45)
        assert result.returncode==0,result.stdout+result.stderr
        assert db.execute("SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND operation='region.preview'",(tenant,)).fetchone()==(0,)
        assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v26_region'",(tenant,)).fetchone()==(0,)
        assert db.execute("SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=%s AND key='region-disabled-admission'",(tenant,)).fetchone()==(0,)
        row=db.execute("SELECT a.object_key,a.object_version FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=%s AND u.state='ready'",(tenant,)).fetchone()
        assert row is not None
        response=client.get_object(bucket,row[0],version_id=row[1])
        try:plan=json.loads(response.read())
        finally:response.close();response.release_conn()
        from photo_worker.operations.region.contracts import RegionPreviewParams
        from photo_worker.contracts.images import WorkingImage
        from photo_worker.contracts.base import canonical_bytes
        params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
        params.validate_source(source_body)
        assert plan['source']['object_version']==source_version
        assert plan['recipe']['object_version']==recipe_version
        assert plan['geometry']['output_width']==1024 and plan['geometry']['output_height']==512
