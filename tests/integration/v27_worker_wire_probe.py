"""Owned integration subprocess: real Python worker readers against Go mTLS gateway."""
import os
import json
from pathlib import Path
from uuid import UUID,uuid4
import psycopg
import numpy as np
from minio import Minio
from photo_worker.contracts.wire import TaskCommand
from photo_worker.contracts.semantic import SemanticTransferParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.database import Lease,TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.operations.semantic.adapter import sql_semantic_artifact_factory


def main(runtime_dsn):
    expected_state=os.environ['PHOTO_TEST_V27_EXPECTED_STATE']
    proposed=expected_state=='proposed'
    edge=256 if proposed else 16
    expected_rgb=np.random.default_rng(2026).uniform(.05,.95,(edge,edge,3)).astype(np.float32) if proposed else np.full((edge,edge,3),.5,dtype=np.float32)
    expected_mask=np.zeros((edge,edge),dtype=np.float32) if proposed else np.ones((edge,edge),dtype=np.float32)
    if proposed:expected_mask[96:160,96:160]=1
    dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    with psycopg.connect(dsn) as db:
        row=db.execute('SELECT tenant_id,run_id,id,worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,inputs,params_artifact_id FROM photo.tasks WHERE id=%s',(UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK']),)).fetchone()
        sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(row[0],row[11])).fetchone()[0]
    command=TaskCommand.model_validate_json(json.dumps(dict(schema_version=1,event_id=str(uuid4()),tenant_id=str(row[0]),run_id=str(row[1]),task_id=str(row[2]),attempt=row[4],operation='semantic.transfer',operation_version='1',resource_class='gpu',correlation_id=str(uuid4()),inputs=row[10],params=dict(artifact_id=str(row[11]),sha256=sha),model=None,timeout_seconds=1800,traceparent='')))
    lease=Lease(command,row[3],row[5],row[6],row[7],row[8],row[9]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    scratch=directory/'scratch';scratch.mkdir()
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    database=TaskDatabase(runtime_dsn);pin=database.semantic_qualification(lease)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    from photo_worker.color.icc import ICCRegistry
    working_profile=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('work_rec2020_gamma24_v1')
    def color(image):
        if image.color.icc_sha256!=working_profile.sha256:raise AssertionError('Unexpected fixture ICC')
    def scene(params,intent,calibration):
        assert params.reference==intent.reference and calibration.model_lock_sha==params.model_lock_sha
        assert calibration.dataset_sha==pin['dataset_sha']
        # Controlled metadata owner; constant fixture images must stay unsupported.
    factory=sql_semantic_artifact_factory(database=database,objects=objects,bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],model_lock_sha=pin['model_lock_sha'],runtime_lock_sha=pin['runtime_lock_sha'],device_profile_sha=pin['device_profile_sha'],dataset_sha=pin['dataset_sha'],verify_color=color,verify_scene=scene)
    with TaskArtifacts.create(ctx,config,frozenset({'document','mask','mask_pixels','transform'})) as artifacts:
        installed=factory(ctx,artifacts)
        params=artifacts.params(SemanticTransferParams)
        reference=artifacts.contract(params.reference.source,WorkingImage)
        target=artifacts.contract(params.target.source,WorkingImage)
        with installed.load_arrays(artifacts,params,reference,target,lease.execution_profile,ctx.check_active) as arrays:
            assert arrays.reference.shape==(edge,edge,3) and np.allclose(arrays.reference,expected_rgb**2.4)
            assert arrays.target.shape==(edge,edge,3) and np.array_equal(arrays.mask,expected_mask)
            assert arrays.reference_protected[0,0]==1 and arrays.target_protected[0,0]==1
        assert installed.verify_runtime() is not None
        from photo_worker.operations.semantic.transfer import execute_transfer
        result=execute_transfer(ctx,artifacts,load_arrays=installed.load_arrays,
            verify_runtime=installed.verify_runtime,verify_scene=installed.verify_scene)
        assert len(result.outputs)==1 and result.bytes_written>0 and result.review_required is proposed
        with psycopg.connect(dsn) as verification:
            stored=verification.execute('SELECT bucket,object_key,object_version,sha256,verified_at,output_role FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,result.outputs[0].artifact_id)).fetchone()
        assert stored[4] is not None and stored[5]=='document'
        response=objects.get_object(stored[0],stored[1],version_id=stored[2])
        try:body=response.read()
        finally:response.close();response.release_conn()
        from hashlib import sha256
        from photo_worker.contracts.semantic import TransferCandidate
        assert sha256(body).hexdigest()==stored[3]==result.outputs[0].sha256
        candidate=TransferCandidate.model_validate_json(body)
        assert candidate.state==expected_state
        if not proposed:assert candidate.mask is None and candidate.transform is None
        else:
            assert candidate.mask is not None and candidate.transform is not None
            from photo_worker.contracts.images import MaskRef,ResolvedTransform
            from io import BytesIO
            def published(ref):
                with psycopg.connect(dsn) as verification:
                    location=verification.execute('SELECT bucket,object_key,object_version,sha256,verified_at FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,ref.artifact_id)).fetchone()
                assert location[4] is not None and location[3]==ref.sha256
                response=objects.get_object(location[0],location[1],version_id=location[2])
                try:raw=response.read()
                finally:response.close();response.release_conn()
                assert sha256(raw).hexdigest()==ref.sha256
                return raw
            mask=MaskRef.model_validate_json(published(candidate.mask))
            transform=ResolvedTransform.model_validate_json(published(candidate.transform))
            assert mask.canvas==target.canvas and not mask.protected
            transferred=np.load(BytesIO(published(mask.artifact)),allow_pickle=False)
            assert transferred.dtype==np.float32 and np.allclose(transferred,expected_mask,atol=1e-4)
            assert np.allclose(np.asarray(transform.matrix).reshape(3,3),np.eye(3),atol=1e-4)
        assert candidate.target==params.target
    assert not artifacts.scratch.directory.exists()
    print('V27_WORKER_WIRE_OK')


if __name__ == '__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main, os.environ['PHOTO_TEST_DATABASE_URL'])
