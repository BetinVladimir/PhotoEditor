"""Typed batch recovery: real SQL, optional live MinIO source JSON and transport; synthetic binary provenance."""
from pathlib import Path as _PhotoRepositoryPath
import json
import os
from pathlib import Path
import subprocess
import tempfile
from uuid import uuid4
from hashlib import sha256
import psycopg
import pytest
from psycopg.conninfo import make_conninfo
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from layer_original_fixture import seed_original_composition
from conftest import ROOT
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('crash',[False,True])
def test_full_typed_go_batch_begin_and_part_recovery(database,crash):
    run_typed_recovery(database,crash)


def test_full_typed_go_batch_live_transport(database,lens_objects):
    run_typed_recovery(database,True,lens_objects)


def run_typed_recovery(database,crash,objects=None):
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
        command=['go','test','-json','./internal/v2render','-run','^TestIntegrationRenderBatchTypedPlan$','-count=1']
        result=subprocess.run(command,cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
        _capture_named_go(result, 'TestIntegrationRenderBatchTypedPlan')
        assert result.returncode==0,result.stdout+result.stderr
        recipe=Path(str(path)+'.recipe').read_text();digest=sha256(recipe.encode()).hexdigest()
        with psycopg.connect(**database) as db:
            root_object=publish_fixture(recipe.encode())
            db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,%s,%s,%s,%s,%s,%s,'application/json','document',now())",(root,tenant,bucket,root_object['key'],root_object['version'],digest,len(recipe.encode())))
            db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,now())",(tenant,document,project,binding['asset_version_id'],root,digest,len(recipe.encode()),bucket,root_object['version']))
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,));db.execute('SET LOCAL ROLE photo_api')
            assert db.execute('SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')',(tenant,binding['asset_version_id'],document,actor,session,root,digest,binding['source']['artifact_id'],binding['source']['sha256'],recipe)).fetchone()==(True,)
        env['PHOTO_RENDER_TYPED_STAGE']='1'
        env['PHOTO_RENDER_TYPED_CRASH']='1' if crash else '0'
        result=subprocess.run(command,cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
        _capture_named_go(result, 'TestIntegrationRenderBatchTypedPlan', require_pass=not crash)
        if crash:
            assert result.returncode!=0
            assert 'signal: killed' in result.stdout+result.stderr,result.stdout+result.stderr
            assert Path(str(path)+'.recovery').is_file()
        else:
            assert result.returncode==0,result.stdout+result.stderr
        if objects is not None:
            from io import BytesIO
            client,bucket=objects
            with psycopg.connect(**database) as db:
                source_versions.update(db.execute("SELECT object_key,object_version FROM photo.artifacts WHERE tenant_id=%s AND bucket=%s AND media_type='application/json'",(tenant,bucket)).fetchall())
                key,old_version=db.execute('SELECT object_key,object_version FROM photo.artifacts WHERE id=%s',(binding['source']['artifact_id'],)).fetchone()
            replacement=client.put_object(bucket,key,BytesIO(b'{}'),2,content_type='application/json',metadata={'sha256':sha256(b'{}').hexdigest()})
            assert replacement.version_id!=old_version
            source_versions.add((key,replacement.version_id))
        restart=['go','test','-json','./internal/v2render','-run','^TestIntegrationRenderBatchTypedRestart$','-count=1']
        result=subprocess.run(restart,cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
        _capture_named_go(result, 'TestIntegrationRenderBatchTypedRestart')
        assert result.returncode==0,result.stdout+result.stderr
        with psycopg.connect(**database) as db:
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_api')
                    db.execute('SELECT * FROM photo.next_v26_render_recovery(NULL)')
        with psycopg.connect(**database) as db:
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute("SELECT set_config('app.tenant_id',%s,true)",(tenant,))
                    db.execute('SET LOCAL ROLE photo_api')
                    db.execute('UPDATE photo.runs SET v26_input_recovery_failures=1 WHERE tenant_id=%s AND id=%s',(tenant,json.loads(Path(str(path)+'.recovery').read_text())['Batch']))
            with pytest.raises(psycopg.errors.InsufficientPrivilege):
                with db.transaction():
                    db.execute('SET LOCAL ROLE photo_api')
                    db.execute('SELECT photo.record_v26_render_recovery(%s,%s,true)',(tenant,json.loads(Path(str(path)+'.recovery').read_text())['Batch']))
        if objects is not None:
            client,bucket=objects
            staged=subprocess.run(['go','test','-json','./internal/v2render','-run','^TestIntegrationStageRenderRuntimeBatch$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
            _capture_named_go(staged, 'TestIntegrationStageRenderRuntimeBatch')
            assert staged.returncode==0,staged.stdout+staged.stderr
            runtime=subprocess.run(['go','test','-json','./internal/workflow','-run','^TestIntegrationRenderRecoveryRuntimeLoop$','-count=1'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=60)
            _capture_named_go(runtime, 'TestIntegrationRenderRecoveryRuntimeLoop')
            assert runtime.returncode==0,runtime.stdout+runtime.stderr
            with psycopg.connect(**database) as db:
                rows=db.execute("SELECT d.purpose,a.object_key,a.object_version,a.sha256,a.bytes,d.object_version FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=%s AND d.bucket=%s AND d.state='ready' ORDER BY d.owner_id,d.purpose",(tenant,bucket)).fetchall()
            assert len(rows)==8
            assert [row[0] for row in rows]==['v2_input_chunk_0','v2_input_manifest']*4
            for purpose,key,version,digest,length,document_version in rows:
                assert version==document_version and version not in ('','null',None)
                response=client.get_object(bucket,key,version_id=version)
                try:payload=response.read()
                finally:response.close();response.release_conn()
                assert len(payload)==length and sha256(payload).hexdigest()==digest
                assert json.loads(payload)['schema_version']==1
            with psycopg.connect(**database) as db:
                declared=db.execute("SELECT t.task_id FROM photo.v2_task_inputs t WHERE t.tenant_id=%s AND t.artifact_id=%s AND t.sha256=%s AND t.contract='NpyArray'",(tenant,source['pixels']['artifact_id'],source['pixels']['sha256'])).fetchall()
            assert len(declared)==4
            verify_live_render_pixels(client,bucket,pixels_object,source,rgb,Path(directory)/'pixels-check')
            versions=list(client.list_objects(bucket,recursive=True,include_version=True))
            assert len(versions)==8+len(source_versions)
            assert {(entry.object_name,entry.version_id) for entry in versions}=={(row[1],row[2]) for row in rows}|source_versions


def verify_live_render_pixels(client,bucket,location,source,expected,folder):
    from io import BytesIO
    from types import SimpleNamespace
    import numpy as np
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.v2.render_source import load_render_source_pixels
    image=WorkingImage.model_validate_json(canonical_bytes(source))
    streams=[]
    class Stream:
        def __init__(self,ref):
            assert ref==image.pixels
            self.size=location['bytes'];self.closed=False
            self.response=client.get_object(bucket,location['key'],version_id=location['version'])
            streams.append(self)
        def read(self,n):return self.response.read(n)
        def close(self):
            if not self.closed:self.response.close();self.response.release_conn();self.closed=True
    class Gateway:
        def open_stream(self,ref):return Stream(ref)
    # Controlled lease/gateway admission; actual TaskArtifacts byte verification,
    # scratch accounting and renderer source loader operate on real MinIO data.
    command=SimpleNamespace(inputs=[image.pixels],params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command))
    folder.mkdir(mode=0o700)
    with ScratchSpace(folder,uuid4(),uuid4(),1,8*1024*1024,min_free_bytes=0) as scratch:
        with TaskArtifacts(ctx,Gateway(),scratch) as artifacts:
            with load_render_source_pixels(artifacts,image,max_input_bytes=8*1024*1024) as loaded:
                assert np.array_equal(loaded.pixels.rgb,expected)
                assert loaded.pixels.alpha is None
                assert not loaded.alpha_requires_new_reference
                mapped=loaded.pixels.rgb
            assert mapped._mmap.closed
            with scratch.reserve(8*1024*1024):pass
    assert len(streams)==1 and all(stream.closed for stream in streams)


def _capture_named_go(result, name, *, require_pass=True):
    """Persist only closed Go event metadata: never child output, DSN or object config."""
    from uuid import uuid4
    from hashlib import sha256
    events = []
    for line in result.stdout.splitlines():
        if not line.startswith('{'):
            continue
        event = json.loads(line)
        events.append({key: event[key] for key in ('Action', 'Package', 'Test', 'Elapsed') if key in event})
    evidence = ROOT/'work/specialized-v26-go-evidence'
    evidence.mkdir(parents=True, exist_ok=True)
    record = dict(root=name, exit_code=result.returncode, events=events,
                  stdout_sha256=sha256(result.stdout.encode()).hexdigest(),
                  stderr_sha256=sha256(result.stderr.encode()).hexdigest())
    path = evidence/(name+'-'+uuid4().hex+'.json')
    path.write_text(json.dumps(record, indent=2)+'\n')
    package = ('photoeditor/backend/internal/workflow' if name in ('TestIntegrationRenderBatchPassFinalizesAndSynchronizes', 'TestIntegrationRenderRecoveryRuntimeLoop') else 'photoeditor/backend/internal/v2draft' if name == 'TestIntegrationRenderCatalogFrameAuthority' else 'photoeditor/backend/internal/v2render')
    exact = [event for event in events if event.get('Test') == name and event.get('Package') == package]
    assert sum(event.get('Action') == 'run' for event in exact) == 1, str(path)
    assert not any(event.get('Test') == name and event.get('Package') != package for event in events), str(path)
    assert not any(event.get('Action') == 'skip' for event in events), str(path)
    if require_pass:
        assert result.returncode == 0, str(path)
        assert [event['Action'] for event in exact if event.get('Action') in ('pass', 'fail', 'skip')] == ['pass'], str(path)
        assert not any(event.get('Action') == 'fail' for event in events), str(path)
    else:
        assert result.returncode != 0, str(path)
        assert not any(event.get('Action') == 'pass' for event in exact), str(path)

