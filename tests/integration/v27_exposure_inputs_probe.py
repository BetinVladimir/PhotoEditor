"""Persisted HTTP Apply inputs through worker SQL and exact-version MinIO.

The owned fixture provisions worker/node metadata; Go Dispatcher and
TaskDatabase.claim establish the actual reservation and worker lease.
This helper alone does not exercise authenticated gateway staging or adoption.
CPU compute and output bytes use an explicitly fixture-owned MinIO transport.
"""
from specialized_go_child_evidence import run_named_go
from pathlib import Path as _PhotoRepositoryPath
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
from psycopg.conninfo import make_conninfo
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.images import MaskRef
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.semantic.exposure_installation import sql_local_exposure_factory
from photo_worker.runtime.database import TaskDatabase
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.input_closure import V2InputIndex


def verify_http_exposure_worker_inputs(db,database,client,bucket,tenant,root):
    task,run,params,digest,profile,inputs=db.execute("SELECT id,run_id,params_artifact_id,input_digest,execution_profile,inputs FROM photo.tasks WHERE tenant_id=%s AND operation='semantic.local_exposure' AND state='ready'",(tenant,)).fetchone()
    params_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(tenant,params)).fetchone()[0]
    worker=uuid4();node='exposure-inputs-'+str(uuid4())
    db.execute("INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES(%s,true,4000000000,0,2000000000,4000000000,0,2000000000,clock_timestamp(),repeat('a',64))",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"semantic.local_exposure\":[\"cpu\"]}','[\"v2.2\"]')",(tenant,worker,profile['model_lock_sha'],node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES(%s,'cpu1','cpu','{}')",(node,))
    db.commit()
    import os,subprocess
    repo=Path(__file__).resolve().parents[2]
    environment=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_V27_DISPATCH_TASK=str(task),GOCACHE=str(_PhotoRepositoryPath(__file__).resolve().parents[2]/'work/go-cache'))
    # Qualification is a trusted synthetic fixture pin; explicit operator
    # activation is separate and never bypasses dispatcher/claim admission.
    state=db.execute("""SELECT t.operation,t.operation_version,t.resource_class,
        t.execution_profile->>'backend',op.v2_enabled,op.resource_class,op.backends
        FROM photo.tasks t JOIN photo.operation_registry op
          ON op.operation=t.operation AND op.version=t.operation_version
        WHERE t.tenant_id=%s AND t.id=%s""",(tenant,task)).fetchone()
    assert state is not None and state[:4]==('semantic.local_exposure','1','cpu','cpu')
    assert state[5]=='cpu' and 'cpu' in state[6]
    qualification_ready=db.execute("""SELECT photo.v27_downstream_profile_ready(t.tenant_id,i.project_id,
        t.operation,t.operation_version,t.resource_class,t.execution_profile)
        FROM photo.tasks t JOIN photo.semantic_targets target ON target.tenant_id=t.tenant_id AND target.child_run_id=t.run_id
        JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
        WHERE t.tenant_id=%s AND t.id=%s""",(tenant,task)).fetchall()
    assert qualification_ready==[(True,)]
    original_enabled=state[4]
    db.commit()
    # Persist only nonsecret exact activation predicates for this owned graph.
    import json
    evidence=root.parent/'exposure-dispatch-activation.json'
    diagnostic=dict(operation=state[0],version=state[1],
        task_resource_class=state[2],backend=state[3],original_enabled=original_enabled,
        registry_resource_class=state[5],registry_backends=state[6],qualification_ready=True,
        negative={'attempted':False},restoration={'attempted':False})
    def save_diagnostic():
        evidence.write_text(json.dumps(diagnostic,sort_keys=True))
    save_diagnostic()
    try:
        if not original_enabled:
            before_refusal=db.execute("""SELECT
                (SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s),
                (SELECT count(*) FROM photo.outbox WHERE tenant_id=%s)""",(tenant,task,tenant)).fetchone()
            db.commit()
            denied=run_named_go(['go','test','./internal/workflow','-run','^TestIntegrationSemanticExposureDispatcher$','-count=1','-timeout=30s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=40,expect_failure=True)
            diagnostic['negative']={'attempted':True,'returncode':denied.returncode,
                'activation_stale':'V2_OPERATION_DISPATCH_ACTIVATION_STALE' in denied.stdout+denied.stderr}
            save_diagnostic()
            assert denied.returncode!=0 and diagnostic['negative']['activation_stale']
            after_refusal=db.execute("""SELECT
                (SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=%s AND task_id=%s),
                (SELECT count(*) FROM photo.outbox WHERE tenant_id=%s)""",(tenant,task,tenant)).fetchone()
            ready=db.execute('SELECT state,dispatch_command IS NULL FROM photo.tasks WHERE tenant_id=%s AND id=%s',(tenant,task)).fetchone()
            diagnostic['negative'].update(before_counts=before_refusal,after_counts=after_refusal,
                counts_unchanged=after_refusal==before_refusal,task_state=ready[0],command_absent=ready[1])
            save_diagnostic()
            assert after_refusal==before_refusal
            assert ready==('ready',True)
            assert db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.local_exposure' AND version='1' AND resource_class='cpu' AND backends ? 'cpu' AND NOT v2_enabled").rowcount==1
            db.commit()
        dispatched=run_named_go(['go','test','./internal/workflow','-run','^TestIntegrationSemanticExposureDispatcher$','-count=1','-timeout=30s','-v'],cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=40)
        assert dispatched.returncode==0,dispatched.stdout+dispatched.stderr
        assert 'V27_DISPATCHER_OK' in dispatched.stdout
        envelope=db.execute('SELECT dispatch_command FROM photo.tasks WHERE tenant_id=%s AND id=%s',(tenant,task)).fetchone()[0]
        db.commit()
        from photo_worker.contracts.wire import TaskCommand
        command=TaskCommand.model_validate_json(canonical_bytes(envelope))
        from wire_runtime_login import worker_login
        with worker_login(make_conninfo(**database), task, pending_dispatch=True) as runtime_dsn:
            return _execute_http_exposure_worker_inputs(db,database,client,bucket,tenant,root,
                task,run,params,digest,profile,inputs,worker,runtime_dsn,command)
    finally:
        # Isolated operator state only; never restore before actual claim/compute.
        db.rollback()
        diagnostic['restoration']={'attempted':True,'expected_enabled':original_enabled}
        save_diagnostic()
        restored=db.execute("UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation='semantic.local_exposure' AND version='1' AND resource_class='cpu' AND backends ? 'cpu'",(original_enabled,)).rowcount
        db.commit()
        actual=db.execute("SELECT v2_enabled FROM photo.operation_registry WHERE operation='semantic.local_exposure' AND version='1' AND resource_class='cpu' AND backends ? 'cpu'").fetchall()
        db.commit()
        diagnostic['restoration'].update(rowcount=restored,actual_enabled=None if len(actual)!=1 else actual[0][0],
            matched=restored==1 and actual==[(original_enabled,)])
        save_diagnostic()
        assert diagnostic['restoration']['matched']


def _execute_http_exposure_worker_inputs(db,database,client,bucket,tenant,root,
        task,run,params,digest,profile,inputs,worker,runtime_dsn,command):
    claimed=TaskDatabase(runtime_dsn).claim(command,worker)
    assert claimed.outcome=='claimed'
    lease=claimed.lease
    assert lease is not None and lease.fence==1
    rows=db.execute('SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.tenant_id=%s AND i.task_id=%s',(tenant,task)).fetchall()
    db.commit()
    locations={(r[0],r[1]):r[2:] for r in rows}
    class Stream:
        def __init__(self,response):self.response=response;self.size=int(response.headers['Content-Length']);self.closed=False
        def read(self,n):return self.response.read(n)
        def close(self):
            if not self.closed:
                self.closed=True
                try:self.response.close()
                finally:self.response.release_conn()
    def open_stream(ref):
        owned_bucket,key,version=locations[(ref.artifact_id,ref.sha256)]
        assert owned_bucket==bucket and version and version!='null'
        return Stream(client.get_object(bucket,key,version_id=version))
    staged={}
    def upload(path,role,media_type,*,expected_identity=None):
        assert expected_identity==(path.stat().st_dev,path.stat().st_ino)
        raw=path.read_bytes()
        ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        key="fixture-exposure-output/"+str(ref.artifact_id)
        result=client.put_object(bucket,key,BytesIO(raw),len(raw),content_type=media_type)
        assert result.version_id and result.version_id!="null"
        staged[ref.artifact_id]=(ref,role,media_type,raw,key,result.version_id)
        return ref
    progress=[]
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None,progress=lambda done,total:progress.append((done,total)))
    database_port=TaskDatabase(runtime_dsn)
    icc=ICCRegistry(Path(__file__).resolve().parents[2]/'resources'/'icc')
    factory=sql_local_exposure_factory(database=database_port,objects=client,bucket=bucket,icc_registry=icc,**{k:profile[k] for k in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    root.mkdir(mode=0o700,parents=True,exist_ok=False)
    with ScratchSpace(root,tenant,task,1,profile['resource_estimate']['scratch_bytes'],min_free_bytes=0) as scratch:
        index=V2InputIndex.build(task_id=task,inputs=command.inputs,params=command.params,execution_profile=profile,scratch=scratch,open_stream=open_stream,check_active=ctx.check_active)
        with TaskArtifacts(ctx,SimpleNamespace(open_stream=open_stream,upload=upload),scratch,index) as artifacts:
            installed=factory(ctx,artifacts)
            edit=artifacts.params(ApprovedSemanticEdit)
            assert installed.verify_runtime(edit,profile) is True
            assert database_port.admit_semantic_local_exposure(lease,canonical_bytes(edit)) is True
            protection=ArtifactRef.model_validate(installed.resolve_protection(edit))
            mask=artifacts.contract(protection,MaskRef)
            assert database_port.local_exposure_protection_origin(lease,canonical_bytes(edit),protection,mask.artifact) is True
            from photo_worker.operations.semantic.exposure_adapter import execute_exposure
            from photo_worker.operations.optical.contracts import ImageResult,ImageReport
            from photo_worker.contracts.images import WorkingImage
            import numpy as np
            result=execute_exposure(ctx,artifacts,installed)
            assert progress==[(0,1),(1,1)]
            assert len(result.outputs)==1 and len(staged)==4
            root_output=ImageResult.model_validate_json(staged[result.outputs[0].artifact_id][3])
            output_image=WorkingImage.model_validate_json(staged[root_output.working_image.artifact_id][3])
            report=ImageReport.model_validate_json(staged[root_output.report.artifact_id][3])
            assert report.operation=='semantic.local_exposure' and report.status in ('pass','review')
            assert report.outside_mask_max_error==0 and report.clipping_fraction==0
            source=artifacts.contract(edit.source.source,WorkingImage)
            assert output_image.canvas==source.canvas and output_image.color==source.color
            original=np.load(artifacts.input(source.pixels).path,allow_pickle=False)
            changed=np.load(BytesIO(staged[output_image.pixels.artifact_id][3]),allow_pickle=False)
            assert changed.shape==original.shape and changed.dtype==original.dtype
            assert np.isfinite(changed).all() and np.any(changed!=original)
            assert result.bytes_written==sum(len(v[3]) for v in staged.values())
            for ref,role,media,raw,key,version in staged.values():
                response=client.get_object(bucket,key,version_id=version)
                try:assert response.read()==raw and sha256(raw).hexdigest()==ref.sha256
                finally:response.close();response.release_conn()
    assert not scratch.directory.exists()

    return task
