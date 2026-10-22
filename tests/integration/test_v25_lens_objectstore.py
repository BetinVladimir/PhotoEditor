"""Live, isolated MinIO version reads for lens adoption binary validation.

Catalog evidence comes from PostgreSQL; this does not prove complete result adoption.
All container data and credentials belong to a fresh repository-owned test dir.
"""
from pathlib import Path
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import json
import os
import shutil
import subprocess
import time
import socket
from contextlib import contextmanager

import numpy as np
import pytest
from minio import Minio
from minio.error import S3Error
from minio.versioningconfig import VersioningConfig, ENABLED
import urllib3
import psycopg
from psycopg.types.json import Jsonb
from test_database import tenant

from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader
from photo_worker.v2.lens_adoption import verify_lens_array

ROOT=Path(__file__).resolve().parents[2]
IMAGE='photoeditor-minio-integration:9e49d5e7'


@pytest.fixture
def lens_objects():
    if os.environ.get('PHOTO_TEST_NATIVE_INFRA')=='1':
        yield from native_lens_objects()
        return
    # Docker Desktop can respond slowly after a full schema/bootstrap run.
    # Keep this read-only preflight bounded; a timeout remains a test error.
    inspected=subprocess.run(['docker','image','inspect',IMAGE],capture_output=True,text=True,timeout=60)
    if inspected.returncode:pytest.skip('Build deploy/images/minio-integration/Dockerfile first')
    metadata=json.loads(inspected.stdout)[0]
    assert metadata['Config']['Labels']['photoeditor.purpose']=='isolated-objectstore-integration'
    assert metadata['Config']['Labels']['photoeditor.scope']=='development'
    tmpfs=os.getenv('PHOTO_TEST_MINIO_TMPFS','0')
    assert tmpfs in ('0','1'),'PHOTO_TEST_MINIO_TMPFS must be 0 or 1'
    identity=uuid4().hex
    folder=ROOT/'.runtime/v25-objectstore'/identity
    folder.mkdir(parents=True,mode=0o700)
    data=folder/'data';data.mkdir()
    env=folder/'minio.env'
    access='lens'+uuid4().hex;secret=uuid4().hex+uuid4().hex
    env.write_text(f'MINIO_ROOT_USER={access}\nMINIO_ROOT_PASSWORD={secret}\n');env.chmod(0o600)
    name='photoeditor-v25-'+identity
    # Explicit small-fixture mode for disk-constrained hosts. This does not test
    # MinIO container restart durability; creator/recovery processes remain real.
    storage_args=(['--tmpfs','/data:rw,size=128m'] if tmpfs=='1'
                  else ['--volume',str(data)+':/data'])
    try:
        started=subprocess.run(['docker','run','--rm','-d','--name',name,
            '--label','photoeditor.scope=development','--memory','512m','--cpus','2',
            '--publish','127.0.0.1::9000','--env-file',str(env),
            *storage_args,metadata['Id']],capture_output=True,text=True,timeout=20)
        assert started.returncode==0,started.stderr
        port=subprocess.check_output(['docker','port',name,'9000/tcp'],text=True,timeout=10).strip().rsplit(':',1)[1]
        client=Minio('127.0.0.1:'+port,access_key=access,secret_key=secret,secure=False,
            http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=1,read=2),retries=False))
        bucket='lens-'+identity
        deadline=time.monotonic()+60
        while True:
            try:
                client.make_bucket(bucket);break
            except Exception:
                if time.monotonic()>=deadline:raise
                time.sleep(.1)
        client.set_bucket_versioning(bucket,VersioningConfig(ENABLED))
        client._photo_test_config=dict(Endpoint='http://127.0.0.1:'+port,Bucket=bucket,AccessKey=access,SecretKey=secret,Development=True)
        yield client,bucket
    finally:
        stopped=subprocess.run(['docker','stop','--time','3',name],capture_output=True,text=True,timeout=10)
        # --rm removal can lag a successful stop. Confirm absence through a
        # successful inventory; an inspect/daemon error is not proof of absence.
        deadline=time.monotonic()+5
        while True:
            remaining=subprocess.run(['docker','ps','--all','--filter','name='+name,
                '--format','{{.Names}}'],capture_output=True,text=True,timeout=3)
            if remaining.returncode==0 and name not in remaining.stdout.splitlines():
                shutil.rmtree(folder)
                break
            if time.monotonic()>=deadline:
                raise RuntimeError('Integration container absence unconfirmed; preserve its data')
            time.sleep(.1)


def test_lens_array_reads_exact_old_version_and_rejects_new_wrong_bytes(lens_objects,database):
    client,bucket=lens_objects
    array=np.array([[[-2.,4.,1.],[0.,0.,0.]]],dtype='<f4')
    encoded=BytesIO();np.save(encoded,array,allow_pickle=False);payload=encoded.getvalue()
    first=client.put_object(bucket,'pixels.npy',BytesIO(payload),len(payload),content_type='application/x-npy')
    replacement=bytes(len(payload))
    second=client.put_object(bucket,'pixels.npy',BytesIO(replacement),len(replacement),content_type='application/x-npy')
    assert first.version_id and second.version_id and first.version_id!=second.version_id
    ref=ArtifactRef(uuid4(),sha256(payload).hexdigest())
    root_payload=b'{}';root_ref=ArtifactRef(uuid4(),sha256(root_payload).hexdigest())
    root_version=client.put_object(bucket,'root.json',BytesIO(root_payload),len(root_payload),content_type='application/json').version_id
    with psycopg.connect(**database,autocommit=True) as catalog:
        with catalog.transaction():
            tid,actor=tenant(catalog,str(uuid4())+'@example.test')
            project=catalog.execute("INSERT INTO photo.projects(tenant_id,name) VALUES(%s,'Lens object evidence') RETURNING id",(tid,)).fetchone()[0]
            snapshot=catalog.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,0) RETURNING id",(tid,actor,'a'*64)).fetchone()[0]
            run=catalog.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,'v2','running',%s,%s,%s,%s,'fail_fast') RETURNING id",(tid,snapshot,Jsonb({'project_id':str(project)}),'b'*64,actor)).fetchone()[0]
            catalog.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",(tid,project,run,snapshot,'c'*64))
            params=catalog.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,%s,%s,2,'application/json','synthetic',clock_timestamp()) RETURNING id",(tid,bucket,str(uuid4()),sha256(b'{}').hexdigest())).fetchone()[0]
            task=catalog.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,'optical.lens:0','optical.lens','1','cpu','[]',%s,%s,1,1,%s) RETURNING id",(tid,run,params,'d'*64,Jsonb({'backend':'cpu','renderer_version':'v2.2'}))).fetchone()[0]
            worker=uuid4()
            catalog.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,1,1,%s,clock_timestamp(),'cpu','{}')",(tid,task,worker))
            catalog.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,%s,%s,'pixels.npy',%s,%s,'application/x-npy',%s,clock_timestamp(),%s,1,1,'pixels')",(ref.artifact_id,tid,bucket,ref.sha256,len(payload),first.version_id,task))
            catalog.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'pixels',%s,'pixels.npy',%s,%s,'application/x-npy','ready',%s)",(tid,task,worker,'e'*64,uuid4(),ref.artifact_id,bucket,ref.sha256,len(payload),first.version_id))
            catalog.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,%s,%s,'root.json',%s,2,'application/json',%s,clock_timestamp(),%s,1,1,'document')",(root_ref.artifact_id,tid,bucket,root_ref.sha256,root_version,task))
            catalog.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document',%s,'root.json',%s,2,'application/json','ready',%s)",(tid,task,worker,'e'*64,uuid4(),root_ref.artifact_id,bucket,root_ref.sha256,root_version))
            result=catalog.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,task,Jsonb([{'artifact_id':str(root_ref.artifact_id),'sha256':root_ref.sha256}]),'f'*64)).fetchone()[0]
            catalog.execute("UPDATE photo.tasks SET canonical_result_id=%s,state='succeeded' WHERE id=%s",(result,task))
        catalog.execute('SET ROLE photo_scheduler')
        def reader(tenant_id=tid,fence=1):return PostgresObjectEvidenceReader(catalog,client,tenant_id=tenant_id,task_id=task,attempt=1,fence=fence,allowed_bucket=bucket)
        verify_lens_array(reader(),ref,array.shape,'pixels')
        with pytest.raises(AdoptionInvalid,match='EVIDENCE_MISMATCH'):
            verify_lens_array(reader(uuid4()),ref,array.shape,'pixels')
        with pytest.raises(AdoptionInvalid,match='EVIDENCE_MISMATCH'):
            verify_lens_array(reader(fence=2),ref,array.shape,'pixels')
        catalog.execute('RESET ROLE')
        from psycopg import errors
        with pytest.raises(errors.CheckViolation,match='immutable field artifacts.object_version'):
            catalog.execute('UPDATE photo.artifacts SET object_version=%s WHERE id=%s',(second.version_id,ref.artifact_id))
        bad=ArtifactRef(uuid4(),ref.sha256)
        bad_version=client.put_object(bucket,'bad.npy',BytesIO(replacement),len(replacement),content_type='application/x-npy').version_id
        catalog.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,%s,%s,'bad.npy',%s,%s,'application/x-npy',%s,clock_timestamp(),%s,1,1,'pixels')",(bad.artifact_id,tid,bucket,bad.sha256,len(replacement),bad_version,task))
        catalog.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'pixels',%s,'bad.npy',%s,%s,'application/x-npy','ready',%s)",(tid,task,worker,'e'*64,uuid4(),bad.artifact_id,bucket,bad.sha256,len(replacement),bad_version))
        catalog.execute('SET ROLE photo_scheduler')
        with pytest.raises(AdoptionInvalid,match='HEADER_INVALID'):
            verify_lens_array(reader(),bad,array.shape,'pixels')


@contextmanager
def native_object_server(binary, values):
    """Start only an absent local service, owning its process/data exclusively."""
    try:
        with socket.create_connection(('127.0.0.1', 59010), timeout=1):
            pass
    except ConnectionRefusedError:
        folder=ROOT/'.runtime/v25-objectstore'/('native-'+uuid4().hex)
        folder.mkdir(parents=True,mode=0o700)
        volume=os.environ.get('PHOTO_TEST_NATIVE_MINIO_DATA_ROOT')
        if volume:
            base=Path(volume)
            # A separately mounted repository-owned volume keeps the real
            # server's free-space guard intact on nearly full developer disks.
            if (not base.is_absolute() or base.is_symlink() or not base.is_relative_to(ROOT/'work')
                    or base.resolve()!=base or not os.path.ismount(base)
                    or base.stat().st_dev==base.parent.stat().st_dev):
                raise RuntimeError('Native MinIO data root must be a mounted repository-owned volume')
            data=base/('native-'+uuid4().hex)
        else:
            data=folder/'data'
        data.mkdir(mode=0o700)
        log=(folder/'server.log').open('wb')
        process=None
        try:
            process=subprocess.Popen([str(binary),'server',str(data),
                '--address','127.0.0.1:59010','--console-address','127.0.0.1:59011'],
                env=dict(os.environ,MINIO_ROOT_USER=values['MINIO_ROOT_USER'],
                         MINIO_ROOT_PASSWORD=values['MINIO_ROOT_PASSWORD']),
                stdout=log,stderr=subprocess.STDOUT,cwd=ROOT)
            deadline=time.monotonic()+20
            while True:
                if process.poll() is not None:
                    raise RuntimeError('Owned native MinIO exited; see '+str(folder/'server.log'))
                def remaining_timeout():
                    remaining=deadline-time.monotonic()
                    if remaining<=0:raise TimeoutError('Owned native MinIO readiness deadline')
                    return urllib3.Timeout(total=remaining,connect=min(1,remaining),read=min(1,remaining))
                try:
                    response=urllib3.PoolManager().request('GET',
                        'http://127.0.0.1:59010/minio/health/ready',
                        timeout=remaining_timeout(),retries=False)
                    if response.status==200:
                        # The health endpoint can precede authenticated S3
                        # initialization on a freshly formatted owned volume.
                        try:
                            Minio('127.0.0.1:59010',access_key=values['MINIO_ROOT_USER'],
                                secret_key=values['MINIO_ROOT_PASSWORD'],secure=False,
                                http_client=urllib3.PoolManager(timeout=remaining_timeout(),retries=False)).list_buckets()
                            remaining_timeout()
                            break
                        except S3Error as error:
                            if error.code!='XMinioServerNotInitialized':raise
                except urllib3.exceptions.HTTPError:
                    pass
                if time.monotonic()>=deadline:
                    raise TimeoutError('Owned native MinIO readiness deadline')
                time.sleep(.1)
            yield
        finally:
            if process is not None and process.poll() is None:
                process.terminate()
                try:process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill();process.wait(timeout=5)
            log.close()
            shutil.rmtree(data)
        return
    # Existing process is not ours: never stop it or remove its data.
    yield


def native_lens_objects():
    """Pinned local server; each case owns only its fresh versioned bucket."""
    from hashlib import sha256
    credential_file=ROOT/'work/minio-native-audit.env'
    assert credential_file.stat().st_mode&0o077==0,'Unsafe native credentials permissions'
    manifest=json.loads((ROOT/'.runtime/minio-native/source-manifest.json').read_text())
    lock=json.loads((ROOT/'resources/minio-source.lock.json').read_text())
    assert manifest['commit']==lock['commit'] and manifest['sum']==lock['sum']
    binary=ROOT/'.runtime/minio-native/minio-server'
    assert sha256(binary.read_bytes()).hexdigest()==manifest['binary_sha256']
    values=dict(line.split('=',1) for line in credential_file.read_text().splitlines())
    with native_object_server(binary,values):
        client=Minio('127.0.0.1:59010',access_key=values['MINIO_ROOT_USER'],secret_key=values['MINIO_ROOT_PASSWORD'],secure=False,
            http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=2,read=10),retries=False))
        bucket='native-lens-'+uuid4().hex
        client.make_bucket(bucket)
        try:
            client.set_bucket_versioning(bucket,VersioningConfig(ENABLED))
            client._photo_test_config=dict(Endpoint='http://127.0.0.1:59010',Bucket=bucket,AccessKey=values['MINIO_ROOT_USER'],SecretKey=values['MINIO_ROOT_PASSWORD'],Development=True)
            yield client,bucket
        finally:
            for item in client.list_objects(bucket,recursive=True,include_version=True):
                client.remove_object(bucket,item.object_name,version_id=item.version_id)
            client.remove_bucket(bucket)
