"""Actual SQL factory/index/object inputs; explicit MinIO transport test port."""
from types import SimpleNamespace
from uuid import uuid4
from psycopg.conninfo import make_conninfo
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import ArtifactRef
from photo_worker.contracts.semantic import V2SemanticAssembleParams,EditIntent,TransferCandidate
from photo_worker.operations.session.contracts import Calibration
from photo_worker.operations.semantic.assembly_installation import sql_semantic_assembly_factory
from photo_worker.operations.semantic.assemble import assemble_semantic_plan
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.input_closure import V2InputIndex


def verify_cpu_inputs(db,database,client,bucket,tenant,scope,root,*,expected_state='unsupported',reserve_quota=True):
    task=scope['assembly'];worker=uuid4();node='cpu-physical-'+str(uuid4())
    run,params,digest,profile=db.execute('SELECT run_id,params_artifact_id,input_digest,execution_profile FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    params_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(params,)).fetchone()[0]
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,0,2000000000)",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"v2.semantic.assemble\":[\"cpu\"]}','[\"v2.2\"]')",(tenant,worker,'a'*64,node))
    db.execute("UPDATE photo.tasks SET state='running',attempt=2,fencing_token=2,canonical_result_id=NULL,worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(worker,task))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'cpu1','cpu','{}',%s,%s,2,clock_timestamp()+interval '60 seconds')",(node,tenant,task))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES(%s,%s,2,2,%s,clock_timestamp(),'cpu',%s)",(tenant,task,worker,Jsonb(profile)))
    entries=db.execute('SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.task_id=%s',(task,)).fetchall()
    if reserve_quota:
        db.execute('INSERT INTO photo.tenant_usage(tenant_id,reserved_bytes) VALUES(%s,1048576)',(tenant,))
        db.execute("INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at) VALUES(%s,'user',%s,'run',%s,1048576,1,clock_timestamp()+interval '1 hour')",(tenant,scope['actor'],run))
    db.commit()
    command=SimpleNamespace(tenant_id=tenant,run_id=run,task_id=task,attempt=2,params=ArtifactRef(artifact_id=params,sha256=params_sha),inputs=[ArtifactRef.model_validate_json(__import__('json').dumps(profile['v2_input_manifest']))])
    lease=SimpleNamespace(command=command,worker_id=worker,fence=2,input_digest=digest,execution_profile=profile)
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    locations={(row[0],row[1]):row[2:] for row in entries}
    class Stream:
        def __init__(self,response):self.response=response;self.size=int(response.headers['Content-Length']);self.closed=False
        def read(self,n):return self.response.read(n)
        def close(self):
            if not self.closed:
                self.closed=True
                try:self.response.close()
                finally:self.response.release_conn()
    def open_stream(ref):
        location=locations[(ref.artifact_id,ref.sha256)]
        return Stream(client.get_object(location[0],location[1],version_id=location[2]))
    transport=TaskDatabase(make_conninfo(**database))
    factory=sql_semantic_assembly_factory(database=transport,objects=client,bucket=bucket)
    with ScratchSpace(root,tenant,task,2,1024**3,min_free_bytes=0) as scratch:
        index=V2InputIndex.build(task_id=task,inputs=command.inputs,params=command.params,execution_profile=profile,scratch=scratch,open_stream=open_stream,check_active=ctx.check_active)
        with TaskArtifacts(ctx,SimpleNamespace(open_stream=open_stream),scratch,index) as artifacts:
            installed=factory(ctx,artifacts)
            params=artifacts.params(V2SemanticAssembleParams)
            intent=artifacts.contract(params.intent,EditIntent)
            calibration=artifacts.contract(params.calibration,Calibration)
            snapshot=installed.load_snapshot(artifacts,intent)
            candidates=[artifacts.contract(ref,TransferCandidate) for ref in params.candidates]
            installed.verify_provenance(params,intent,snapshot,candidates)
            plan=assemble_semantic_plan(params,intent,snapshot,calibration,candidates)
            assert len(plan.candidates)==1 and plan.candidates[0].state==expected_state
    assert not scratch.directory.exists()

    import os,subprocess
    from pathlib import Path
    repo=Path(__file__).resolve().parents[2];credentials=client._provider.retrieve()
    environment=dict(os.environ,PHOTO_TEST_V27_CPU_STATE=expected_state,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_V27_GATEWAY_TASK=str(task),PHOTO_TEST_V27_REPO_ROOT=str(repo),PHOTO_TEST_MINIO_URL='http://'+client._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,GOCACHE='/private/tmp/photoeditor-go-cache')
    wire=subprocess.run(['go','test','./internal/gateway','-run','^TestIntegrationV27IndexedDownloads$','-count=1','-timeout=90s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=100)
    assert wire.returncode==0,wire.stdout+wire.stderr
    assert '--- PASS: TestIntegrationV27IndexedDownloads' in wire.stdout
