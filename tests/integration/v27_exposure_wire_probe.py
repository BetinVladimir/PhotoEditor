"""Owned integration subprocess: real Python worker readers against Go mTLS gateway."""
from specialized_go_child_evidence import run_named_go
import os
import json
from pathlib import Path
from uuid import UUID,uuid4
import psycopg
from minio import Minio
from photo_worker.contracts.wire import TaskCommand
from photo_worker.runtime.database import Lease,TaskDatabase
from photo_worker.runtime.executor import ExecutionContext
from photo_worker.runtime.gateway import client_tls
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts


from photo_worker.operations.semantic.exposure_installation import sql_local_exposure_factory
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.semantic.exposure_adapter import execute_exposure
from photo_worker.operations.optical.contracts import ImageResult
from photo_worker.contracts.base import canonical_bytes
from hashlib import sha256


def main(runtime_dsn):
    dsn=os.environ['PHOTO_TEST_DATABASE_URL']
    with psycopg.connect(dsn) as db:
        row=db.execute('SELECT tenant_id,run_id,id,worker_id,attempt,fencing_token,lease_until,deadline,execution_profile,input_digest,inputs,params_artifact_id,dispatch_command FROM photo.tasks WHERE id=%s',(UUID(os.environ['PHOTO_TEST_V27_GATEWAY_TASK']),)).fetchone()
    command=TaskCommand.model_validate_json(json.dumps(row[12]))
    assert command.task_id==row[2] and command.attempt==row[4]
    lease=Lease(command,row[3],row[5],row[6],row[7],row[8],row[9]);ctx=ExecutionContext(lease)
    directory=Path(os.environ['PHOTO_TEST_V27_CERT_DIR'])
    tls=client_tls(directory/'ca.pem',directory/'client.pem',directory/'client.key')
    scratch=directory/'scratch';scratch.mkdir()
    config=ArtifactConfig(scratch,os.environ['PHOTO_TEST_V27_GATEWAY_ORIGIN'],tls,1024**3,8*1024**2,1024**3)
    database=TaskDatabase(runtime_dsn)
    objects=Minio(os.environ['PHOTO_TEST_MINIO_URL'].removeprefix('http://'),access_key=os.environ['PHOTO_TEST_MINIO_ACCESS'],secret_key=os.environ['PHOTO_TEST_MINIO_SECRET'],secure=False)
    factory=sql_local_exposure_factory(database=database,objects=objects,bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'),**{key:row[8][key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    with TaskArtifacts.create(ctx,config,frozenset({'pixels','working_image','report','document'})) as artifacts:
        installed=factory(ctx,artifacts)
        result=execute_exposure(ctx,artifacts,installed)
        assert len(result.outputs)==1 and result.bytes_written>0
        with psycopg.connect(dsn) as verification:
            stored=verification.execute('SELECT bucket,object_key,object_version,sha256,verified_at,output_role FROM photo.artifacts WHERE tenant_id=%s AND id=%s',(command.tenant_id,result.outputs[0].artifact_id)).fetchone()
        assert stored[4] is not None and stored[5]=='document'
        response=objects.get_object(stored[0],stored[1],version_id=stored[2])
        try:body=response.read()
        finally:response.close();response.release_conn()
        assert sha256(body).hexdigest()==stored[3]==result.outputs[0].sha256
        root=ImageResult.model_validate_json(body)
        with psycopg.connect(dsn) as verification:
            roles=verification.execute('SELECT output_role,verified_at,object_version FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id=%s AND produced_attempt=%s AND produced_fence=%s',(command.tenant_id,command.task_id,command.attempt,lease.fence)).fetchall()
        assert len(roles)==4 and {value[0] for value in roles}=={'pixels','working_image','report','document'}
        assert all(value[1] is not None and value[2] and value[2]!='null' for value in roles)
    assert not artifacts.scratch.directory.exists()
    payload=canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in result.outputs],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=result.bytes_written),review_required=result.review_required))
    committed=database.commit_success(lease,payload)
    assert committed[0]=='committed'
    assert database.commit_success(lease,payload)==('replayed',*committed[1:])
    with psycopg.connect(dsn) as verification:
        task=verification.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.task_id)).fetchone()
        assert task==('succeeded',committed[1])
        assert verification.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=%s AND attempt=%s AND fencing_token=%s',(command.tenant_id,command.task_id,command.attempt,lease.fence)).fetchone()[0]==1
        assert verification.execute('SELECT count(*) FROM photo.resource_slots WHERE tenant_id=%s AND task_id=%s',(command.tenant_id,command.task_id)).fetchone()[0]==0
    # Owned-fixture fault injection is rolled back; production finalization is
    # exercised with the real admitted child before any adoption marker exists.
    for terminal in ('failed','cancelled'):
        with psycopg.connect(dsn,autocommit=True) as fault:
            with fault.transaction():
                if terminal=='failed':
                    fault.execute("UPDATE photo.tasks SET state='failed' WHERE tenant_id=%s AND id=%s",(command.tenant_id,command.task_id))
                else:
                    fault.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.run_id))
                fault.execute('SET LOCAL ROLE photo_scheduler')
                assert fault.execute('SELECT photo.finalize_next_semantic_apply_child()').fetchone()[0]==1
                assert fault.execute('SELECT photo.finalize_semantic_apply_child(%s,%s)',(command.tenant_id,command.run_id)).fetchone()[0]==terminal
                fault.execute('RESET ROLE')
                assert fault.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.run_id)).fetchone()[0]==terminal
                assert fault.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(command.tenant_id,command.run_id)).fetchall()==[(terminal,)]
                assert fault.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(command.tenant_id,command.run_id)).fetchone()==('released',0)
                assert fault.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type=%s',(command.tenant_id,command.run_id,'semantic.child.'+terminal)).fetchone()[0]==1
                raise psycopg.Rollback()
    from photo_worker.v2.service import Adopter
    from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
    adoption_scratch=directory/'adoption-scratch';adoption_scratch.mkdir()
    with psycopg.connect(dsn,autocommit=True) as read,psycopg.connect(dsn) as write:
        read.execute('SET ROLE photo_scheduler')
        write.execute('SET ROLE photo_scheduler');write.commit()
        assert read.execute('SELECT photo.finalize_semantic_apply_child(%s,%s)',(command.tenant_id,command.run_id)).fetchone()[0]=='pending'
        coordinator=Adopter(read,write,objects,os.environ['PHOTO_TEST_MINIO_BUCKET'],layer_scratch_root=adoption_scratch,layer_scratch_bytes=1024**3,exposure_working_icc_sha=WORKING_PROFILE_SHA).coordinator
        assert coordinator.adopt(command.tenant_id,command.task_id) is True
        assert coordinator.adopt(command.tenant_id,command.task_id) is False
    with psycopg.connect(dsn) as verification:
        parent_before=verification.execute('SELECT r.id,r.status,r.revision FROM photo.semantic_targets target JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id JOIN photo.v2_workflows workflow ON workflow.tenant_id=intent.tenant_id AND workflow.id=intent.workflow_id JOIN photo.runs r ON r.tenant_id=workflow.tenant_id AND r.id=workflow.run_id WHERE target.tenant_id=%s AND target.child_run_id=%s',(command.tenant_id,command.run_id)).fetchone()
        from concurrent.futures import ThreadPoolExecutor
        from threading import Barrier
        barrier=Barrier(2)
        def finalize_concurrently():
            with psycopg.connect(dsn,autocommit=True) as contender:
                contender.execute("SET statement_timeout='10s'")
                contender.execute('SET ROLE photo_scheduler')
                barrier.wait(timeout=10)
                return contender.execute('SELECT photo.finalize_semantic_apply_child(%s,%s)',(command.tenant_id,command.run_id)).fetchone()[0]
        with ThreadPoolExecutor(max_workers=2) as contenders:
            futures=[contenders.submit(finalize_concurrently) for _ in range(2)]
            assert [future.result(timeout=20) for future in futures]==['completed','completed']
        verification.execute('SET ROLE photo_scheduler')
        assert verification.execute('SELECT photo.finalize_next_semantic_apply_child()').fetchone()[0]==0
        assert verification.execute('SELECT photo.finalize_semantic_apply_child(%s,%s)',(command.tenant_id,command.run_id)).fetchone()[0]=='completed'
        assert verification.execute('SELECT photo.finalize_next_semantic_apply_child()').fetchone()[0]==0
        assert verification.execute('SELECT photo.settle_semantic_child(%s,%s)',(command.tenant_id,command.run_id)).fetchone()[0]==0
        verification.commit()
    with psycopg.connect(dsn) as verification:
        assert verification.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,command.run_id)).fetchone()[0]=='completed'
        assert verification.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(command.tenant_id,command.run_id)).fetchall()==[('applied',)]
        assert verification.execute('SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type=\'run\' AND holder_id=%s',(command.tenant_id,command.run_id)).fetchone()==('consumed',0)
        assert verification.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='semantic.child.completed'",(command.tenant_id,command.run_id)).fetchone()[0]==1
        assert verification.execute('SELECT id,status,revision FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,parent_before[0])).fetchone()==parent_before
    with psycopg.connect(dsn,autocommit=True) as preparation:
        target=preparation.execute('SELECT id FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(command.tenant_id,command.run_id)).fetchone()[0]
        preparation.execute('SET ROLE photo_scheduler')
        source=preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',(command.tenant_id,target)).fetchone()
        assert source is not None and source[1:6]==(command.run_id,command.task_id,committed[1],command.attempt,lease.fence)
        assert source[6]==result.outputs[0].artifact_id and source[7]==result.outputs[0].sha256
        assert source[8:11]==stored[:3] and source[12]=='application/json'
        from photo_worker.v2.semantic_presentation import read_exposure_presentation_result
        prepared=read_exposure_presentation_result(preparation,objects,tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'])
        assert prepared==root
        inherited=[]
        for pinned_ref in command.inputs:
            item=preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_input(%s,%s,%s)',(command.tenant_id,target,pinned_ref.artifact_id)).fetchone()
            if item is not None:
                assert item[3]==pinned_ref.sha256 and item[2] not in ('','null')
                inherited.append(pinned_ref)
        assert inherited
        assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_input(%s,%s,%s)',(command.tenant_id,target,result.outputs[0].artifact_id)).fetchone() is None
        assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_input(%s,%s,%s)',(uuid4(),target,inherited[0].artifact_id)).fetchone() is None
        from photo_worker.v2.semantic_presentation import read_exposure_presentation_working_image
        working=read_exposure_presentation_working_image(preparation,objects,tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'])
        pixels=preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',(command.tenant_id,target,working.pixels.artifact_id)).fetchone()
        assert pixels is not None and pixels[3]==working.pixels.sha256 and pixels[5:]==('application/x-npy','pixels')
        from photo_worker.v2.semantic_presentation import exposure_presentation_pixels
        presentation_scratch=directory/'presentation-scratch';presentation_scratch.mkdir()
        with exposure_presentation_pixels(preparation,objects,tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],scratch_root=presentation_scratch,max_bytes=1024**3,icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc')) as (image,mapped):
            assert image==working and mapped.shape==(working.canvas.height,working.canvas.width,3)
            assert str(mapped.dtype)==working.dtype and not mapped.flags.writeable
        assert list(presentation_scratch.iterdir())==[]
        from photo_worker.v2.semantic_presentation import exposure_presentation_alpha
        with exposure_presentation_alpha(preparation,objects,tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],scratch_root=presentation_scratch,max_bytes=1024**3) as opacity:
            if working.alpha is None:
                assert opacity is None
            else:
                assert opacity.shape==(working.canvas.height,working.canvas.width) and not opacity.flags.writeable
                assert (str(opacity.dtype)=='uint16' and (opacity==32768).all()) or (str(opacity.dtype)=='float32' and (opacity==.5).all())
        assert list(presentation_scratch.iterdir())==[]
        from photo_worker.v2.semantic_presentation import exposure_presentation_image
        options=dict(tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],scratch_root=presentation_scratch,icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'))
        with exposure_presentation_image(preparation,objects,**options,max_bytes=1024**3) as (image,rgb,alpha):
            assert image==working and rgb.shape==(working.canvas.height,working.canvas.width,3)
            assert (alpha is None)==(working.alpha is None)
        assert list(presentation_scratch.iterdir())==[]
        from photo_worker.v2.adoption import AdoptionInvalid
        try:
            with exposure_presentation_image(preparation,objects,**options,max_bytes=pixels[4]-1):
                raise AssertionError('Combined scratch budget exceeded')
        except AdoptionInvalid as error:
            assert str(error)=='SEMANTIC_PRESENTATION_SCRATCH_EXCEEDED'
        assert list(presentation_scratch.iterdir())==[]
        from photo_worker.v2.semantic_presentation import encode_exposure_presentation_preview
        from PIL import Image
        with encode_exposure_presentation_preview(preparation,objects,**options,max_bytes=1024**3,execution_profile={'resource_estimate':{'ram_bytes':512*1024**2}}) as (preview,media):
            assert preview.exists() and media==('image/webp' if working.alpha is not None else 'image/jpeg')
            with Image.open(preview) as encoded:
                assert encoded.size==(working.canvas.width,working.canvas.height)
                if working.alpha is not None:
                    assert abs(int(encoded.getpixel((0,0))[3])-128)<=1
        assert not preview.exists() and list(presentation_scratch.iterdir())==[]
        from photo_worker.v2.semantic_presentation import prepare_exposure_preview_plan
        plan=prepare_exposure_preview_plan(preparation,objects,tenant_id=command.tenant_id,target_id=target,allowed_bucket=os.environ['PHOTO_TEST_MINIO_BUCKET'],icc_registry=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'))
        assert plan.source_result==result.outputs[0]
        assert list(plan.inputs)==[root.working_image,working.pixels,*([working.alpha] if working.alpha else [])]
        assert sha256(plan.params_bytes).hexdigest()==plan.params_sha256
        srgb=ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc').resolve('srgb_v1').sha256
        assert preparation.execute('SELECT photo.enqueue_next_semantic_exposure_preview(%s)',(srgb,)).fetchone()[0] is True
        assert preparation.execute('SELECT photo.enqueue_next_semantic_exposure_preview(%s)',(srgb,)).fetchone()[0] is False
        preview_run=preparation.execute('SELECT photo.enqueue_semantic_exposure_preview(%s,%s,%s,%s)',(command.tenant_id,target,committed[1],srgb)).fetchone()[0]
        assert preview_run is not None
        assert preparation.execute('SELECT photo.enqueue_semantic_exposure_preview(%s,%s,%s,%s)',(command.tenant_id,target,committed[1],srgb)).fetchone()[0]==preview_run
        assert preparation.execute('SELECT photo.enqueue_semantic_exposure_preview(%s,%s,%s,%s)',(command.tenant_id,target,committed[1],'f'*64)).fetchone()[0] is None
        assert preparation.execute('SELECT photo.enqueue_semantic_exposure_preview(%s,%s,%s,%s)',(command.tenant_id,target,uuid4(),srgb)).fetchone()[0] is None
        preparation.execute('RESET ROLE')
        preview_state=preparation.execute('SELECT kind,status,graph_sealed,v26_derivative_task_id,config_snapshot FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,preview_run)).fetchone()
        assert preview_state[:4]==('preview','draft',False,command.task_id)
        assert preview_state[4]['source_result_id']==str(committed[1])
        preparation.execute('SET ROLE photo_scheduler')
        for planned_ref in (plan.source_result,*plan.inputs):
            derivative_input=preparation.execute('SELECT * FROM photo.v26_render_derivative_artifact(%s,%s,%s,%s)',(command.tenant_id,preview_run,planned_ref.artifact_id,planned_ref.sha256)).fetchone()
            assert derivative_input is not None and derivative_input[0]==planned_ref.artifact_id and derivative_input[4]==planned_ref.sha256
        assert preparation.execute('SELECT * FROM photo.v26_render_derivative_artifact(%s,%s,%s,%s)',(command.tenant_id,preview_run,plan.inputs[0].artifact_id,'f'*64)).fetchone() is None
        assert preparation.execute('SELECT * FROM photo.v26_render_derivative_artifact(%s,%s,%s,%s)',(uuid4(),preview_run,plan.inputs[0].artifact_id,plan.inputs[0].sha256)).fetchone() is None
        preparation.execute('RESET ROLE')
        preparation.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1073741824'::jsonb) WHERE id=%s",(command.tenant_id,))
        usage_before=preparation.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(command.tenant_id,)).fetchone()[0]
        preparation.execute('SET ROLE photo_scheduler')
        assert preparation.execute('SELECT photo.reserve_v26_render_derivative(%s,%s)',(command.tenant_id,preview_run)).fetchone()[0] is True
        assert preparation.execute('SELECT photo.reserve_v26_render_derivative(%s,%s)',(command.tenant_id,preview_run)).fetchone()[0] is True
        preparation.execute('RESET ROLE')
        quota=preparation.execute("SELECT bytes,state,principal_id FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(command.tenant_id,preview_run)).fetchone()
        assert quota is not None and quota[0:2]==(111149056,'reserved')
        assert preparation.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(command.tenant_id,)).fetchone()[0]==usage_before+111149056
        preparation.execute('SET ROLE photo_scheduler')
        document_args=(command.tenant_id,preview_run,'preview1600',canonical_bytes(root).decode(),canonical_bytes(working).decode(),plan.params_bytes.decode(),os.environ['PHOTO_TEST_MINIO_BUCKET'])
        pinned_document=preparation.execute('SELECT photo.pin_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s)',document_args).fetchone()[0]
        assert pinned_document is not None
        assert preparation.execute('SELECT photo.pin_v26_render_derivative_document(%s,%s,%s,%s,%s,%s,%s)',document_args).fetchone()[0]==pinned_document
        preparation.execute('RESET ROLE')
        document_state=preparation.execute('SELECT state,sha256,bytes,v26_derivative_params FROM photo.backend_documents WHERE tenant_id=%s AND artifact_id=%s',(command.tenant_id,pinned_document)).fetchone()
        assert document_state[1:]==(plan.params_sha256,len(plan.params_bytes),plan.params_bytes.decode())
        import subprocess
        publish_env={**os.environ,'PHOTO_TEST_V27_PREVIEW_RUN':str(preview_run)}
        publication=run_named_go(['go','test','./internal/workflow','-run','^TestIntegrationSemanticPreviewPublish$','-count=1','-timeout=40s','-v'],cwd=Path(__file__).resolve().parents[2]/'backend',env=publish_env,capture_output=True,text=True,timeout=50)
        assert publication.returncode==0,publication.stdout+publication.stderr
        assert 'V27_PREVIEW_PUBLISH_OK' in publication.stdout
        published_documents=preparation.execute("SELECT state,object_version FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND purpose='task_params'",(command.tenant_id,preview_run)).fetchall()
        assert len(published_documents)==2 and all(state=='ready' and version not in (None,'','null') for state,version in published_documents)
        assert preparation.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,preview_run)).fetchone()==('running',True)
        preview_tasks=preparation.execute('SELECT state,operation,params_artifact_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',(command.tenant_id,preview_run)).fetchall()
        assert len(preview_tasks)==2 and all(state=='queued' and operation=='media.preview' and params is not None for state,operation,params in preview_tasks)
        dispatched_previews=preparation.execute('SELECT id,dispatch_worker_id,dispatch_command FROM photo.tasks WHERE tenant_id=%s AND run_id=%s ORDER BY node_key',(command.tenant_id,preview_run)).fetchall()
        claimed_previews=[]
        for preview_task,preview_worker,envelope in dispatched_previews:
            preview_command=TaskCommand.model_validate_json(canonical_bytes(envelope))
            assert preview_command.task_id==preview_task and preview_command.run_id==preview_run
            rejected=database.claim(preview_command,uuid4())
            assert rejected.outcome!='claimed' and rejected.lease is None
            accepted=database.claim(preview_command,preview_worker)
            assert accepted.outcome=='claimed' and accepted.lease is not None
            assert accepted.lease.fence==1 and accepted.lease.command==preview_command
            duplicate=database.claim(preview_command,preview_worker)
            assert duplicate.outcome!='claimed' and duplicate.lease is None
            claimed_previews.append(accepted.lease)
        assert len(claimed_previews)==2
        assert preparation.execute('SELECT state,fencing_token FROM photo.tasks WHERE tenant_id=%s AND run_id=%s ORDER BY node_key',(command.tenant_id,preview_run)).fetchall()==[('running',1),('running',1)]
        from photo_worker.operations.media.derivative import execute as execute_preview
        preview_results=[]
        for preview_lease in claimed_previews:
            preview_ctx=ExecutionContext(preview_lease)
            with TaskArtifacts.create(preview_ctx,config,frozenset({'derivative','derivative_manifest'})) as preview_artifacts:
                encoded_result=execute_preview(preview_ctx,preview_artifacts,ICCRegistry(Path(__file__).resolve().parents[2]/'resources/icc'),'media.preview')
                assert len(encoded_result.outputs)==2 and encoded_result.bytes_written>0
            assert not preview_artifacts.scratch.directory.exists()
            verified_outputs=preparation.execute('SELECT output_role,verified_at,object_version FROM photo.artifacts WHERE tenant_id=%s AND created_by_task_id=%s AND produced_attempt=%s AND produced_fence=%s',(command.tenant_id,preview_lease.command.task_id,preview_lease.command.attempt,preview_lease.fence)).fetchall()
            assert len(verified_outputs)==2 and {r[0] for r in verified_outputs}=={'derivative','derivative_manifest'}
            assert all(r[1] is not None and r[2] not in (None,'','null') for r in verified_outputs)
            preview_results.append(encoded_result)
            preview_payload=canonical_bytes(dict(outputs=[ref.model_dump(mode='json') for ref in encoded_result.outputs],metrics=dict(duration_ms=0,peak_memory_bytes=0,bytes_written=encoded_result.bytes_written),review_required=encoded_result.review_required))
            assert database.commit_success(preview_lease,preview_payload)[0]=='committed'
            assert database.commit_success(preview_lease,preview_payload)[0]=='replayed'
        from photo_worker.v2.render_derivative_service import RenderDerivativeAdoptionCoordinator
        with psycopg.connect(dsn,autocommit=True) as preview_read,psycopg.connect(dsn,autocommit=True) as preview_write:
            preview_read.execute('SET ROLE photo_scheduler');preview_write.execute('SET ROLE photo_scheduler')
            adopter=RenderDerivativeAdoptionCoordinator(preview_read,preview_write,objects,os.environ['PHOTO_TEST_MINIO_BUCKET'],target_icc_sha256=srgb,working_icc_sha256=working.color.icc_sha256)
            for variant in ('preview1600','preview2560'):
                assert adopter.adopt(command.tenant_id,preview_run,variant) is True
                assert adopter.adopt(command.tenant_id,preview_run,variant) is True
        assert preparation.execute('SELECT count(*) FROM photo.task_results WHERE tenant_id=%s AND task_id=ANY(%s)',(command.tenant_id,[l.command.task_id for l in claimed_previews])).fetchone()[0]==2
        preparation.execute('SET ROLE photo_scheduler')
        assert preparation.execute('SELECT photo.finalize_successful_v26_render_derivative()').fetchone()[0] is True
        assert preparation.execute('SELECT photo.finalize_successful_v26_render_derivative()').fetchone()[0] is False
        preparation.execute('RESET ROLE')
        assert preparation.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s',(command.tenant_id,preview_run)).fetchone()[0]=='completed'
        user,session=preparation.execute("SELECT child.created_by,(parent.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid FROM photo.runs child JOIN photo.runs parent ON parent.tenant_id=child.tenant_id AND parent.id=%s WHERE child.tenant_id=%s AND child.id=%s",(parent_before[0],command.tenant_id,command.run_id)).fetchone()
        preparation.execute('SET ROLE photo_api')
        public_outputs=preparation.execute('SELECT * FROM photo.v26_render_derivative_outputs(%s,%s,%s,%s)',(user,session,command.tenant_id,command.run_id)).fetchall()
        assert len(public_outputs)==4 and {r[1] for r in public_outputs}=={'derivative','derivative_manifest'}
        assert preparation.execute('SELECT * FROM photo.v26_render_derivative_outputs(%s,%s,%s,%s)',(user,session,uuid4(),command.run_id)).fetchall()==[]
        preparation.execute('RESET ROLE')
        preparation.execute('SET ROLE photo_scheduler')
        for accepted_ref in (root.working_image,root.report,result.outputs[0]):
            evidence=preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',(command.tenant_id,target,accepted_ref.artifact_id)).fetchone()
            assert evidence is not None and evidence[3]==accepted_ref.sha256 and evidence[2] not in ('','null')
        assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',(uuid4(),target,root.working_image.artifact_id)).fetchone() is None
        assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_evidence(%s,%s,%s)',(command.tenant_id,target,uuid4())).fetchone() is None
        assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',(uuid4(),target)).fetchone() is None
        preparation.execute('RESET ROLE')
        for denied_role in ('photo_api','photo_worker','photo_portal','photo_gateway'):
            # Function metadata is private even when the caller knows valid IDs.
            with preparation.transaction():
                preparation.execute('SET LOCAL ROLE '+denied_role)
                try:
                    preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',(command.tenant_id,target))
                except psycopg.errors.InsufficientPrivilege:
                    pass
                else:
                    raise AssertionError('presentation metadata accessible to '+denied_role)
                raise psycopg.Rollback()
        with preparation.transaction():
            preparation.execute('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=%s AND id=%s',(command.tenant_id,source[0]))
            preparation.execute('SET LOCAL ROLE photo_scheduler')
            assert preparation.execute('SELECT * FROM photo.semantic_exposure_presentation_source(%s,%s)',(command.tenant_id,target)).fetchone() is None
            raise psycopg.Rollback()
    print('V27_WORKER_WIRE_OK')


if __name__ == '__main__':
    from wire_runtime_login import run_worker_probe
    run_worker_probe(main, os.environ['PHOTO_TEST_DATABASE_URL'])
