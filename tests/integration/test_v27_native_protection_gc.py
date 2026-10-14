"""Isolated metadata lifecycle test; no native worker/model admission is implied."""
import json
from uuid import uuid4
from psycopg.types.json import Jsonb
from layer_original_fixture import seed_original_composition
from test_v25_lens_objectstore import lens_objects


def seed_native_registry_metadata(db, objects=None):
    case = seed_original_composition(db)
    tenant, project = case['Tenant'], case['Project']
    base = json.loads(case['Params'])['base']
    run, task, document = uuid4(), uuid4(), uuid4()
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','completed',%s,'{}',%s,%s,'fail_fast')", (run,tenant,case['Snapshot'],'a'*64,case['Actor']))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'defect:gc','defects.propose','1','cpu','[]',%s,%s,%s,1,1)", (task,tenant,run,case['Artifact']['artifact_id'],'a'*64,Jsonb(dict(backend='cpu'))))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),clock_timestamp(),'cpu',%s)",(tenant,task,uuid4(),Jsonb(dict(backend='cpu'))))
    result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,'[]','{}',%s) RETURNING id", (tenant,task,'a'*64)).fetchone()[0]
    pins={}
    def artifact(media, role):
        from io import BytesIO
        from hashlib import sha256
        artifact_id=uuid4()
        bucket,key,version,digest,size='test',str(uuid4()),'v1','a'*64,100
        if objects is not None:
            client,bucket=objects
            key=f'tenants/{tenant}/native-cleanup/{artifact_id}'
            payload=(b'{"kind":"isolated-cleanup-metadata"}' if media=='application/json' else b'isolated-protection-pixels')
            digest,size=sha256(payload).hexdigest(),len(payload)
            if role=='document':
                client.put_object(bucket,key,BytesIO(payload),size,content_type=media,metadata={'sha256':digest})
            version=client.put_object(bucket,key,BytesIO(payload),size,content_type=media,metadata={'sha256':digest}).version_id
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,clock_timestamp(),%s)",(artifact_id,tenant,bucket,key,digest,size,media,version,role))
        pins[artifact_id]=(digest,size,bucket,version)
        return artifact_id
    root = artifact('application/json','document')
    pixels = artifact('application/octet-stream','defect_protection')
    report = artifact('application/json','defect_propose_report')
    db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,'ready',%s,clock_timestamp())",(tenant,document,project,base['asset_version_id'],root,*pins[root]))
    # Owner-seeded registry row isolates retention independently of content gates.
    db.execute("INSERT INTO photo.semantic_native_protections(tenant_id,project_id,document_id,asset_version_id,source_id,source_sha,parent_run_id,task_id,result_id,root_id,root_sha,pixels_id,pixels_sha,report_id,report_sha,anchor) VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,'{}')",(tenant,project,document,base['asset_version_id'],base['source']['artifact_id'],base['source']['sha256'],run,task,result,root,pins[root][0],pixels,pins[pixels][0],report,pins[report][0]))
    return case, root, pixels, report, document


def test_native_protection_children_follow_root_retention(db):
    case,root,pixels,report,document=seed_native_registry_metadata(db)
    tenant=case["Tenant"]
    for child in (pixels,report):
        assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(tenant,child)).fetchone()[0]
        assert not db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(uuid4(),child)).fetchone()[0]
    db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=%s AND id=%s",(tenant,pixels))
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_maintenance')
        assert db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchall()==[]
    db.execute("UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=%s AND document_id=%s",(tenant,document))
    for child in (pixels,report):
        assert not db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(tenant,child)).fetchone()[0]
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_maintenance')
        assert db.execute('SELECT artifact_id FROM photo.claim_artifact_gc()').fetchone()[0]==pixels
    db.execute("UPDATE photo.v2_draft_uploads SET state='ready' WHERE tenant_id=%s AND document_id=%s",(tenant,document))
    assert db.execute('SELECT photo.v2_artifact_has_references(%s,%s)',(tenant,pixels)).fetchone()[0]
    # SQL metadata purge only: these synthetic object keys have no stored bytes.
    # Physical object-store cleanup still requires a separate maintenance test.
    db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tenant,))
    job=db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s) RETURNING id",(tenant,tenant,case['Actor'])).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_maintenance')
        claimed=db.execute('SELECT job_id,cleanup_token FROM photo.claim_tenant_deletion()').fetchone()
        assert claimed and claimed[0]==job
        assert not db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,uuid4())).fetchone()[0]
        assert db.execute('SELECT photo.finish_tenant_deletion(%s,%s)',(job,claimed[1])).fetchone()[0]
    db.execute('SET LOCAL ROLE NONE')
    assert db.execute('SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
    assert db.execute('SELECT count(*) FROM photo.artifacts WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
    assert db.execute('SELECT status FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]=='deleted'


def test_native_registry_purge_uses_real_tenant_cleaner(database,lens_objects):
    # Real object versions + SQL registry lifecycle; native model admission is
    # independently tested by the CPU/mTLS fixture, not this owner-seeded row.
    import os
    import subprocess
    from pathlib import Path
    from io import BytesIO
    from hashlib import sha256
    import psycopg
    from psycopg.conninfo import make_conninfo
    client,bucket=lens_objects
    root_dir=Path(__file__).resolve().parents[2]
    with psycopg.connect(**database) as db:
        case,root,pixels,report,document=seed_native_registry_metadata(db,objects=(client,bucket))
        tenant=case['Tenant']
        db.execute("UPDATE photo.tenants SET status='deleting' WHERE id=%s",(tenant,))
        db.execute("INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by) VALUES(%s,'tenant',%s,%s)",(tenant,tenant,case['Actor']))
    neighbor_key=f'tenants/{uuid4()}/native-cleanup/neighbor'
    neighbor_body=b'neighbor tenant must survive'
    neighbor_sha=sha256(neighbor_body).hexdigest()
    neighbor_version=client.put_object(bucket,neighbor_key,BytesIO(neighbor_body),len(neighbor_body),content_type='application/octet-stream',metadata={'sha256':neighbor_sha}).version_id
    config=client._photo_test_config
    fixture=dict(Tenant=tenant,Neighbor=dict(Key=neighbor_key,VersionID=neighbor_version,SHA256=neighbor_sha,ContentType='application/octet-stream',Bytes=len(neighbor_body)))
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_NATIVE_TENANT_PURGE=json.dumps(fixture),PHOTO_TEST_MINIO_URL=config['Endpoint'],PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=config['AccessKey'],PHOTO_TEST_MINIO_SECRET=config['SecretKey'],GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','-tags','integration','./internal/maintenance','-run','^TestIntegrationNativeRegistryTenantPurge$','-count=1','-timeout','90s','-v'],cwd=root_dir/'backend',env=env,capture_output=True,text=True,timeout=120)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'V27_NATIVE_REGISTRY_PHYSICAL_TENANT_PURGE_OK' in result.stdout
    assert list(client.list_objects(bucket,prefix=f'tenants/{tenant}/',recursive=True,include_version=True))==[]
    surviving=list(client.list_objects(bucket,prefix=neighbor_key,recursive=True,include_version=True))
    assert len(surviving)==1 and surviving[0].version_id==neighbor_version
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
        assert db.execute('SELECT count(*) FROM photo.artifacts WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
