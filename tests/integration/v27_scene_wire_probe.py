"""Actual scene/depth adapter, claim, versioned gateway outputs and SQL result.

Model inference and installed-runtime measurement are explicit deterministic
fixture doubles. This is lifecycle coverage, never photograph/GPU qualification.
"""
from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
import json
import os
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID, uuid4
import numpy as np
import psycopg
from psycopg import sql
from psycopg.conninfo import conninfo_to_dict, make_conninfo
from psycopg.types.json import Jsonb
from minio import Minio
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import SceneLabels, SceneParseParams
from photo_worker.contracts.images import RelativeDepth
from photo_worker.contracts.wire import TaskCommand,TaskError,TaskMetrics
from photo_worker.operations.scene import parse
from photo_worker.operations.depth import predict
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.database import Lease,TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls

class FixtureEngine:
    def __init__(self,model): self.model=model
    def infer(self,value):
        assert value.dtype==np.float32 and np.isfinite(value).all()
        if self.model=='segformer_b0_ade':
            logits=np.full((150,32,32),-20,dtype=np.float32)
            logits[1]=20
            return logits
        assert self.model=='depth_anything_v2_small'
        return np.tile(np.linspace(0,1,32,dtype=np.float32),(32,1))

class FixturePool:
    lock=SimpleNamespace(lock_id='c'*64)
    @contextmanager
    def acquire(self,model_id,backend):
        assert model_id in ('segformer_b0_ade','depth_anything_v2_small') and backend=='cuda'
        yield FixtureEngine(model_id)

def run(runtime_dsn):
    dsn=os.environ['PHOTO_TEST_DATABASE_URL'];task=UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(dsn) as db:
        row=db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    command=TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.task_id==task and command.operation in ('scene.parse','depth.predict')
    lease=Lease(command,row[0],row[2],row[3],row[4],row[5],row[6]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    scratch=directory/'scratch';scratch.mkdir()
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    roles=('scene_labels','scene_labels_ref') if command.operation=='scene.parse' else ('relative_depth','depth_invalid','relative_depth_ref')
    adapter=parse if command.operation=='scene.parse' else predict
    # This deliberate test double does not claim installed native measurement.
    def runtime(active):
        active.check_active()
        return (active.lease.execution_profile['model_lock_sha']=='c'*64 and
                active.lease.execution_profile['runtime_lock_sha']=='b'*64 and
                active.lease.execution_profile['device_profile_sha']=='c'*64)
    with TaskArtifacts.create(ctx,config,frozenset(roles)) as artifacts:
        result=adapter.execute(ctx,artifacts,FixturePool(),'cuda',runtime)
    assert not artifacts.scratch.directory.exists()
    refs=[ref.model_dump(mode='json') for ref in result.outputs]
    assert len(refs)==2 if command.operation=='scene.parse' else len(refs) in (2,3)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    bodies={}
    with psycopg.connect(dsn) as db:
        db.execute('SELECT set_config(\'app.tenant_id\',%s,true)',(str(command.tenant_id),))
        db.execute('SET LOCAL ROLE photo_fn_owner')
        def ready(outputs,attempt=command.attempt,fence=lease.fence):
            return db.execute('SELECT photo.semantic_scene_result_ready(%s,%s,%s,%s,%s)',(command.tenant_id,task,attempt,fence,Jsonb(outputs))).fetchone()[0]
        assert ready(refs)
        assert not ready(refs,command.attempt+1) and not ready(refs,fence=lease.fence+1)
        for bad in ([],refs+refs,[refs[0]],[dict(refs[0],sha256='0'*64),*refs[1:]]): assert not ready(bad)
        db.execute('RESET ROLE')
        for ref in result.outputs:
            stored=db.execute('SELECT bucket,object_key,object_version,sha256,output_role,verified_at,bytes FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,ref.artifact_id)).fetchone()
            response=objects.get_object(stored[0],stored[1],version_id=stored[2])
            try: body=response.read()
            finally: response.close();response.release_conn()
            assert len(body)==stored[6] and sha256(body).hexdigest()==ref.sha256==stored[3]
            assert stored[5] is not None and stored[4] not in bodies
            bodies[stored[4]]=body
        # Remaining run reservation plus nonreturned allocations equals frozen
        # reserve before settlement. Ready output bytes are already storage.
        remaining,allocated,frozen=db.execute("SELECT q.bytes,(SELECT coalesce(sum(u.bytes),0) FROM photo.output_uploads u JOIN photo.tasks x ON x.tenant_id=u.tenant_id AND x.id=u.task_id WHERE u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation AND x.run_id=t.run_id),(r.config_snapshot->'v27_scene_prepare'->>'reserved_bytes')::bigint FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.quota_reservations q ON q.tenant_id=t.tenant_id AND q.holder_id=t.run_id AND q.holder_type='run' WHERE t.id=%s",(task,)).fetchone()
        assert remaining>0 and allocated>0 and remaining+allocated==frozen
    if command.operation=='scene.parse':
        root=SceneLabels.model_validate_json(bodies['scene_labels_ref'])
        labels=np.load(BytesIO(bodies['scene_labels']),allow_pickle=False)
        assert labels.dtype==np.uint16 and np.all(labels==1) and root.labels==result.outputs[0]
    else:
        root=RelativeDepth.model_validate_json(bodies['relative_depth_ref'])
        depth=np.load(BytesIO(bodies['relative_depth']),allow_pickle=False)
        assert depth.dtype==np.float32 and np.isfinite(depth).all() and depth.max()>depth.min()
        assert root.artifact==result.outputs[0]
    if command.attempt==1 and os.environ.get('PHOTO_TEST_V27_SCENE_TAIL')!='1':
        database=TaskDatabase(runtime_dsn)
        assert database.commit_failure(lease,TaskError(code='INFERENCE_FAILED',retryable=True,message_key='error.inference_failed',args={}),TaskMetrics(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written))=='committed'
        # These are fully completed uploads, so there is no unknown in-flight
        # PUT and no reason to shorten the 3h pending-upload quiet period.
        expected={ref.artifact_id for ref in result.outputs}
        for _ in range(len(expected)):
            with psycopg.connect(dsn) as db:
                db.execute('SET LOCAL ROLE photo_maintenance')
                claim=db.execute('SELECT * FROM photo.claim_output_cleanup()').fetchone()
                assert claim is not None and claim[4] in expected and claim[3]==task
            expected.remove(claim[4])
            # Actual object-store version removal and confirmed absence precede
            # the trusted maintenance settlement call. No marker is fabricated.
            versions=[v for v in objects.list_objects(claim[7],prefix=claim[8],recursive=True,include_version=True) if v.object_name==claim[8]]
            assert versions
            for version in versions: objects.remove_object(claim[7],claim[8],version_id=version.version_id)
            assert not [v for v in objects.list_objects(claim[7],prefix=claim[8],recursive=True,include_version=True) if v.object_name==claim[8]]
            with psycopg.connect(dsn) as db:
                db.execute('SET LOCAL ROLE photo_maintenance')
                assert db.execute('SELECT photo.finish_output_cleanup(%s,%s)',claim[:2]).fetchone()==(True,)
                assert db.execute('SELECT photo.finish_output_cleanup(%s,%s)',claim[:2]).fetchone()==(False,)
        assert not expected
        with psycopg.connect(dsn) as db:
            state,qbytes,counted,refunded=db.execute("SELECT t.state,q.bytes,(SELECT coalesce(sum(u.bytes),0) FROM photo.output_uploads u WHERE u.run_reservation_id=q.id AND NOT u.returned_to_run_reservation),(SELECT count(*) FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=1 AND u.state='cleaned' AND u.returned_to_run_reservation) FROM photo.tasks t JOIN photo.quota_reservations q ON q.tenant_id=t.tenant_id AND q.holder_type='run' AND q.holder_id=t.run_id WHERE t.id=%s",(task,)).fetchone()
            assert state=='retry_wait' and qbytes==frozen and counted==0 and refunded==len(result.outputs)
            assert db.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(task,)).fetchone()==(0,)
        print('V27_SCENE_CONFIRMED_REFUND_OK')
        print('V27_WORKER_WIRE_OK')
        return
    payload=canonical_bytes(dict(outputs=refs,metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=result.review_required))
    database=TaskDatabase(runtime_dsn);committed=database.commit_success(lease,payload)
    assert committed[0]=='committed'
    assert database.commit_success(lease,payload)==('replayed',*committed[1:])
    print('V27_WORKER_WIRE_OK')

def main():
    # A separate membership-only login must pass the real runtime attestation.
    # SET ROLE on an administrator connection is deliberately insufficient.
    dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    name='scene_worker_'+uuid4().hex
    password=uuid4().hex+uuid4().hex
    with psycopg.connect(dsn,autocommit=True) as admin:
        admin.execute(sql.SQL('CREATE ROLE {} LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD {}').format(sql.Identifier(name),sql.Literal(password)))
        try:
            admin.execute(sql.SQL('GRANT photo_worker TO {}').format(sql.Identifier(name)))
            options=conninfo_to_dict(dsn)
            options.update(user=name,password=password)
            run(make_conninfo(**options))
        finally:
            # TaskDatabase closes every physical connection before returning.
            admin.execute(sql.SQL('DROP ROLE {}').format(sql.Identifier(name)))

if __name__=='__main__': main()
