"""Real frozen-scene correspondence, gateway publication and independent adopter.

Imported qualification and installed-runtime attestation are fixture doubles,
explicitly matching the other native wire fixtures. Scene computation, typed
source reads, physical replay and SQL admission/adoption are production code.
"""
import json
from io import BytesIO
from hashlib import sha256
import numpy as np
import os
from pathlib import Path
from uuid import UUID,uuid4
import psycopg
from psycopg import sql
from psycopg.conninfo import conninfo_to_dict,make_conninfo
from minio import Minio
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import TaskDatabase,Lease
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.gateway import client_tls
from photo_worker.operations.semantic.scene_worker import semantic_scene_worker_factory
from photo_worker.operations.semantic.transfer import execute_transfer
from photo_worker.operations.semantic.color_authority import semantic_working_color_authority
from photo_worker.color.icc import ICCRegistry
from photo_worker.v2.semantic_scene_adoption import SemanticSceneAdoption
from photo_worker.v2.service import Adopter
from photo_worker.runtime.application_role import require_application_role

def run(worker_dsn,scheduler_dsn):
    admin_dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    task=UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK'])
    with psycopg.connect(admin_dsn) as db:
        row=db.execute('SELECT worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,dispatch_command FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    command=TaskCommand.model_validate_json(json.dumps(row[7]))
    assert command.operation=='semantic.transfer' and isinstance(row[5].get('semantic_scene'),dict)
    lease=Lease(command,row[0],row[2],row[3],row[4],row[5],row[6]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    scratch=directory/'scene-transfer';scratch.mkdir()
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'];database=TaskDatabase(worker_dsn)
    pin=database.semantic_qualification(lease)
    measurements={key:pin[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}
    # Deliberate installed-runtime double; no physical model qualification is
    # inferred from synthetic report pins. The actual scene algorithm is used.
    def runtime(active):
        active()
        assert measurements['model_lock_sha']=='c'*64 and measurements['runtime_lock_sha']=='b'*64
        active()
    icc=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('work_rec2020_gamma24_v1').sha256
    factory=semantic_scene_worker_factory(database=database,objects=objects,bucket=bucket,measurements=measurements,verify_color=semantic_working_color_authority(icc),verify_installed_runtime=runtime)
    with TaskArtifacts.create(ctx,config,frozenset({'document','mask','mask_pixels','transform'})) as artifacts:
        installed=factory(ctx,artifacts)
        result=execute_transfer(ctx,artifacts,load_arrays=installed.load_arrays,verify_runtime=installed.verify_runtime,verify_scene=installed.verify_scene)
    manifest=canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=result.review_required))
    committed=database.commit_success(lease,manifest)
    assert committed[0]=='committed' and database.commit_success(lease,manifest)==('replayed',*committed[1:])
    # A canonical unsupported refusal is not evidence of a working positive tail.
    from photo_worker.contracts.semantic import TransferCandidate
    from photo_worker.contracts.images import MaskRef,ResolvedTransform
    def published(ref):
        with psycopg.connect(admin_dsn) as verification:
            location=verification.execute('SELECT bucket,object_key,object_version,sha256,verified_at FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,ref.artifact_id)).fetchone()
        assert location is not None and location[4] is not None and location[3]==ref.sha256
        response=objects.get_object(location[0],location[1],version_id=location[2])
        try:raw=response.read()
        finally:response.close();response.release_conn()
        assert sha256(raw).hexdigest()==ref.sha256
        return raw
    candidate=TransferCandidate.model_validate_json(published(result.outputs[0]))
    assert candidate.state=='proposed' and candidate.reason is None and result.review_required and candidate.confidence>0
    assert candidate.mask is not None and candidate.transform is not None
    mask=MaskRef.model_validate_json(published(candidate.mask))
    transform=ResolvedTransform.model_validate_json(published(candidate.transform))
    assert transform.from_canvas_hash==transform.to_canvas_hash==candidate.target.canvas_hash
    assert np.allclose(np.asarray(transform.matrix).reshape(3,3),np.eye(3),atol=1e-4)
    transferred=np.load(BytesIO(published(mask.artifact)),allow_pickle=False)
    assert transferred.dtype==np.float32 and transferred.shape==(300,400)
    assert np.isfinite(transferred).all() and transferred.min()>=0 and transferred.max()<=1
    assert np.count_nonzero(transferred)>0 and not mask.protected
    assert canonical_hash(mask.canvas)==candidate.target.canvas_hash
    expected=np.zeros((300,400),dtype=np.float32);expected[40:90,40:80]=.5
    assert np.allclose(transferred,expected,atol=1e-4)
    # Synthetic person occupies the lower half; no proposed support may reach it.
    assert np.count_nonzero(transferred[150:])==0
    print('V27_SCENE_TRANSFER_PROPOSED_PHYSICAL_CANDIDATE_OK')
    # Independently reread exact versioned documents/arrays under result-bound
    # SQL authority; a successful worker manifest alone does not adopt a result.
    adoption_scratch=directory/'scene-adoption';adoption_scratch.mkdir()
    verifier=SemanticSceneAdoption(measurements,runtime,adoption_scratch,1024**3,8*1024**3)
    with psycopg.connect(scheduler_dsn,autocommit=True,options='-c role=photo_scheduler') as read:
        require_application_role(read,'photo_scheduler')
        with psycopg.connect(scheduler_dsn,autocommit=True,options='-c role=photo_scheduler') as write:
            require_application_role(write,'photo_scheduler')
            adopter=Adopter(read,write,objects,bucket,layer_scratch_root=adoption_scratch,layer_scratch_bytes=1024**3,semantic_scene_verifier=verifier,semantic_ram_bytes=8*1024**3,semantic_working_icc_sha=icc)
            assert adopter.coordinator.adopt(command.tenant_id,task)
    print('V27_SCENE_TRANSFER_INDEPENDENT_ADOPTION_OK')
    print('V27_WORKER_WIRE_OK')

def main():
    dsn=os.environ['PHOTO_TEST_DATABASE_URL'];names=[];connections=[]
    with psycopg.connect(dsn,autocommit=True) as admin:
        try:
            for role in ('photo_worker','photo_scheduler'):
                name='scene_tail_'+uuid4().hex;password=uuid4().hex+uuid4().hex
                admin.execute(sql.SQL('CREATE ROLE {} LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD {}').format(sql.Identifier(name),sql.Literal(password)))
                names.append(name)
                admin.execute(sql.SQL('GRANT {} TO {}').format(sql.Identifier(role),sql.Identifier(name)))
                options=conninfo_to_dict(dsn);options.update(user=name,password=password)
                connections.append(make_conninfo(**options))
            run(*connections)
        finally:
            for name in reversed(names):admin.execute(sql.SQL('DROP ROLE {}').format(sql.Identifier(name)))

if __name__=='__main__':main()
