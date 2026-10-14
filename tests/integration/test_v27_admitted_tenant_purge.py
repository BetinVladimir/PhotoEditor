"""Physical tenant cleanup after actual native preparation/execution/adoption.

Worker model/qualification doubles remain declared. Deletion uses the real
API SQL request, scheduler cancellation and maintenance cleaner/storage.
"""
import json
import os
import subprocess
from pathlib import Path
from io import BytesIO
from hashlib import sha256
from uuid import uuid4
import psycopg
from psycopg.conninfo import make_conninfo
from test_v27_target_worker_failure_matrix import lens_objects
from test_v27_target_worker_failure_matrix import test_target_worker_success_regression as execute_native_chain


def test_admitted_native_chain_physical_tenant_purge(database,lens_objects,monkeypatch):
    for name in ('PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE','PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST','PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE'):
        monkeypatch.delenv(name,raising=False)
    execute_native_chain(database,lens_objects,monkeypatch)
    client,bucket=lens_objects
    with psycopg.connect(**database) as db:
        rows=db.execute("SELECT DISTINCT tenant_id,created_by FROM photo.runs WHERE config_snapshot ? 'v27_detector_prepare'").fetchall()
        assert len(rows)==1
        tenant,actor=rows[0]
        registry=db.execute('SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=%s',(tenant,)).fetchone()[0]
        assert registry>0
        # Require actual finished producer/adoption receipts, not only registry metadata.
        assert db.execute("SELECT count(DISTINCT t.operation) FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id WHERE t.tenant_id=%s AND t.state='succeeded' AND r.config_snapshot ? 'v27_target_person_prepare'",(tenant,)).fetchone()[0]==1
        session=db.execute("SELECT (config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid FROM photo.runs WHERE tenant_id=%s AND config_snapshot ? 'v27_detector_prepare'",(tenant,)).fetchone()[0]
        revision=db.execute('SELECT revision FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]
        job=uuid4()
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT outcome,job_id FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,job)).fetchone()==('queued',job)
        assert db.execute('SELECT outcome,job_id FROM photo.request_tenant_deletion(%s,%s,%s,%s,%s)',(actor,session,tenant,revision,uuid4())).fetchone()==('deleting',job)
    versions=list(client.list_objects(bucket,prefix=f'tenants/{tenant}/',recursive=True,include_version=True))
    assert len(versions)>16
    neighbor_key=f'tenants/{uuid4()}/admitted-purge/neighbor'
    body=b'neighbor tenant must survive admitted native purge'
    version=client.put_object(bucket,neighbor_key,BytesIO(body),len(body),content_type='application/octet-stream',metadata={'sha256':sha256(body).hexdigest()}).version_id
    cfg=client._photo_test_config
    fixture={'Tenant':str(tenant),'RegistryRows':registry,'Neighbor':{'Key':neighbor_key,'VersionID':version,'SHA256':sha256(body).hexdigest(),'ContentType':'application/octet-stream','Bytes':len(body)}}
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_ADMITTED_NATIVE_PURGE=json.dumps(fixture),PHOTO_TEST_MINIO_URL=cfg['Endpoint'],PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=cfg['AccessKey'],PHOTO_TEST_MINIO_SECRET=cfg['SecretKey'],GOCACHE='/private/tmp/photoeditor-go-cache')
    root=Path(__file__).resolve().parents[2]
    result=subprocess.run(['go','test','-tags','integration','./internal/maintenance','-run','^TestIntegrationAdmittedNativeTenantPurge$','-count=1','-timeout','120s','-v'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=150)
    assert result.returncode==0,result.stdout+result.stderr
    assert 'V27_ADMITTED_NATIVE_PHYSICAL_TENANT_PURGE_OK' in result.stdout
    assert list(client.list_objects(bucket,prefix=f'tenants/{tenant}/',recursive=True,include_version=True))==[]
    survivors=list(client.list_objects(bucket,prefix=neighbor_key,recursive=True,include_version=True))
    assert len(survivors)==1 and survivors[0].version_id==version
    with psycopg.connect(**database) as db:
        for table in ('semantic_native_protections','artifacts','tasks','task_results','runs','resource_reservations','resource_slots'):
            assert db.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
        assert db.execute('SELECT status FROM photo.tenants WHERE id=%s',(tenant,)).fetchone()[0]=='deleted'
