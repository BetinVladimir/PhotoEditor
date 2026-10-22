from pathlib import Path as _PhotoRepositoryPath
from pathlib import Path
import json,os,subprocess,tempfile,psycopg,pytest
from uuid import uuid4
from hashlib import sha256
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from layer_original_fixture import seed_original_composition
from conftest import ROOT
from test_v26_render_batch_typed_go import _capture_named_go
pytestmark=pytest.mark.migration_head(2607)
def test_real_go_render_staging_quota_replay_cancel(database):
    objects=None;crash=False
    source=json.loads((ROOT/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    source_versions=set()
    bucket=objects[1] if objects is not None else 'test'
    def publish_fixture(body,media="application/json"):
        key=str(uuid4());version='v1'
        if objects is not None:
            from io import BytesIO
            version=objects[0].put_object(bucket,key,BytesIO(body),len(body),content_type=media,metadata={'sha256':sha256(body).hexdigest()}).version_id
            assert version not in ('','null',None)
            source_versions.add((key,version))
        return dict(bucket=bucket,key=key,version=version,bytes=len(body),sha256=sha256(body).hexdigest())
    pixels_object=dict(bucket=bucket,key=str(uuid4()),version='v1',bytes=64,sha256=source['pixels']['sha256'])
    if objects is not None:
        import numpy as np
        from io import BytesIO
        rgb=np.empty((300,400,3),dtype='<f4');rgb[...,0]=-0.125;rgb[...,1]=0.25;rgb[...,2]=2.0
        encoded=BytesIO();np.save(encoded,rgb,allow_pickle=False)
        pixels_object=publish_fixture(encoded.getvalue(),'application/x-npy')
        source['pixels']['sha256']=pixels_object['sha256']
        replacement=objects[0].put_object(bucket,pixels_object['key'],BytesIO(b'bad-npy'),7,content_type='application/x-npy',metadata={'sha256':sha256(b'bad-npy').hexdigest()})
        assert replacement.version_id!=pixels_object['version']
        source_versions.add((pixels_object['key'],replacement.version_id))
    source_raw=canonical_bytes(source)
    style_raw=canonical_bytes(json.loads((ROOT/'docs/implementation/contracts/examples/style-fixed.example.json').read_text()))
    with tempfile.TemporaryDirectory(prefix='render-typed-',dir=ROOT/'work') as directory:
        path=Path(directory)/'fixture.json'
        with psycopg.connect(**database) as db:
            case=seed_original_composition(db,source_object=publish_fixture(source_raw),canvas_hash=canonical_hash(source['canvas']))
            tenant,project,actor,session=(case[k] for k in ('Tenant','Project','Actor','Session'))
            binding=json.loads(case['Params'])['base']
            # Live variant uses verified uploaded NPY coordinates; controlled modes retain metadata only.
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/x-npy','pixels',now())",(source['pixels']['artifact_id'],tenant,bucket,pixels_object['key'],pixels_object['version'],source['pixels']['sha256'],pixels_object['bytes']))
            producer_run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,'ingest','running',%s,'{}',%s,%s) RETURNING id",(tenant,case['Snapshot'],'a'*64,actor)).fetchone()[0]
            producer=db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,'asset.decode:fixture','asset.decode','1','cpu','blocked','[]',%s,%s,1,1,'{}') RETURNING id",(tenant,producer_run,binding['source']['artifact_id'],'a'*64)).fetchone()[0]
            db.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=ANY(%s::uuid[])",(producer,[binding['source']['artifact_id'],source['pixels']['artifact_id']]))
            db.execute("UPDATE photo.artifacts SET output_role='working_rgb' WHERE id=%s",(source['pixels']['artifact_id'],))
            db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)',(tenant,))
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','4294967296') WHERE id=%s",(tenant,))
            style_object=publish_fixture(style_raw)
            style_ref=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,'application/json','document',now()) RETURNING id",(tenant,bucket,style_object['key'],style_object['version'],sha256(style_raw).hexdigest(),len(style_raw))).fetchone()[0]
            style=db.execute("INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES(%s,%s,'Typed render',%s) RETURNING id",(tenant,project,actor)).fetchone()[0]
            db.execute("INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES(%s,%s,1,'{}',%s,'2.0.0','photo_renderer_v2','accepted','{}',%s)",(tenant,style,sha256(style_raw).hexdigest(),style_ref))
            root,document,batch=uuid4(),uuid4(),uuid4()
            path.write_text(json.dumps(dict(Tenant=tenant,Project=project,Snapshot=case['Snapshot'],Batch=str(batch),Actor=actor,Session=session,Root=str(root),Binding=binding,Source=source,Style=dict(artifact_id=str(style_ref),sha256=sha256(style_raw).hexdigest()))))
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_RENDER_TYPED_FIXTURE=str(path),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
        if objects is not None:
            env['PHOTO_RENDER_OBJECT_CONFIG']=json.dumps(objects[0]._photo_test_config)
        command=['go','test','-json','./internal/v2render','-run','^TestIntegrationRenderBillingAdmission$','-count=1']
        result=subprocess.run(command,cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
        _capture_named_go(result, 'TestIntegrationRenderBillingAdmission')
        assert result.returncode==0,result.stdout+result.stderr
        recipe=Path(str(path)+'.recipe').read_text();digest=sha256(recipe.encode()).hexdigest()
        with psycopg.connect(**database) as db:
            root_object=publish_fixture(recipe.encode())
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json','document',now())",(root,tenant,bucket,root_object['key'],root_object['version'],digest,len(recipe.encode())))
            db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,now())",(tenant,document,project,binding['asset_version_id'],root,digest,len(recipe.encode()),bucket,root_object['version']))
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,));db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')',(tenant,binding['asset_version_id'],document,actor,session,root,digest,binding['source']['artifact_id'],binding['source']['sha256'],recipe)).fetchone()==(True,)
        env['PHOTO_RENDER_TYPED_STAGE']='1'
        from test_billing_apply import enroll
        with psycopg.connect(**database) as db:
            enroll(db,tenant)
            db.execute('INSERT INTO photo.billing_render_controls(tenant_id,enabled) VALUES(%s,true)',(tenant,))
        result=subprocess.run(command,cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=90)
        _capture_named_go(result,'TestIntegrationRenderBillingAdmission')
        assert result.returncode==0,result.stdout+result.stderr
