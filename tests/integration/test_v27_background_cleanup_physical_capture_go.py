"""Real ICC/SQL/objects/worker/HTTP/Paper chain; model provider explicitly doubled.

Imported upstream qualification and native producer metadata are fixture inputs.
No positive preview/copy receipt, output result or terminal child state is seeded.
This is physical plumbing evidence, never installed Big LaMa/photo qualification.
"""
import json
from hashlib import sha256
from uuid import uuid4
import os
from pathlib import Path
import subprocess

import psycopg
from psycopg.conninfo import make_conninfo
import pytest
from photo_worker.contracts.base import canonical_bytes
from background_cleanup_preview_activation_fixture import published_transport_fixture,commit_preview
from semantic_qualification_fixture import frozen_qualification
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('alpha',[False,True])
def test_controlled_background_cleanup_full_physical_capture(database,lens_objects,tmp_path,record_property,alpha):
    repo=Path(__file__).resolve().parents[2]
    objects,bucket=lens_objects
    credentials=objects._provider.retrieve()
    with psycopg.connect(**database) as db:
        args,preview_run,scope=published_transport_fixture(db,objects=lens_objects,with_alpha=alpha)
        preview_task=commit_preview(db,args,preview_run,scope)
        assert preview_task is not None
        assert db.execute('SELECT v27_background_cleanup_preview_receipt FROM photo.runs WHERE id=%s',(preview_run,)).fetchone()==(None,)
        params=scope['preview_params']
        pin_options=dict(objects=lens_objects,backend='cpu',model_lock_sha=params.model_lock_sha,
            runtime_lock_sha=params.runtime_lock_sha,device_profile_sha=params.device_profile_sha)
        # These are explicitly imported fixture reports, not measured model evidence.
        execution=frozen_qualification(db,args[0],scope['project'],args[2],operation='semantic.local_exposure',**pin_options)
        shown=frozen_qualification(db,args[0],scope['project'],args[2],operation='semantic.adopt_shown_background_cleanup',**pin_options)
        def budget(pin):
            return dict(qualification=pin,owner_costs=dict(runtime_ram_bytes=1,input_index_scratch_bytes=16*1024**2),
                limits=dict(ram_bytes=8*1024**3,vram_bytes=0,scratch_bytes=8*1024**3,output_bytes=8*1024**3,pixel_count=1000000),
                **{k:pin[k] for k in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
        from photo_worker.color.icc import ICCRegistry
        icc=ICCRegistry(repo/'resources/icc')
        upstream=scope['qualification']
        catalog=dict(schema_version=1,tenants=[dict(tenant_id=str(args[0]),
            review=dict(schema_version=1,review_model_sha=upstream['model_lock_sha'],review_dataset_sha=upstream['dataset_sha'],document_budget_bytes=64*1024**2),
            execution=budget(execution),shown_background_cleanup_execution=budget(shown))])
        document=dict(tenant_id=str(args[0]),intent_id=str(args[1]),actor_id=str(args[2]),session_id=str(args[3]),target_id=str(args[4]),
            srgb_sha=icc.resolve('srgb_v1').sha256,catalog=catalog,
            preview_fence=dict(ExpectedRevision=params.workflow_revision,TargetRevision=params.target_revision,PlanSHA=params.plan.sha256,CandidateSHA=params.candidate.sha256))
        db.commit()
    fixture=tmp_path/'background_cleanup-physical-http.json';fixture.write_bytes(canonical_bytes(document));fixture.chmod(0o600)
    accepted_path=tmp_path/'background_cleanup-selected-apply.json'
    decision_capture=tmp_path/'background-cleanup-decision-response.json'
    env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_TEST_DISPOSABLE='1',
        PHOTO_TEST_V27_BACKGROUND_CLEANUP_APPLY_FIXTURE=str(fixture),PHOTO_TEST_V27_BACKGROUND_CLEANUP_PROVIDER='controlled',
        PHOTO_TEST_V27_GATEWAY_TASK=str(preview_task),PHOTO_TEST_V27_REPO_ROOT=str(repo),
        PHOTO_TEST_MINIO_URL='http://'+objects._base_url.host,PHOTO_TEST_MINIO_BUCKET=bucket,
        PHOTO_TEST_MINIO_ACCESS=credentials.access_key,PHOTO_TEST_MINIO_SECRET=credentials.secret_key,
        PHOTO_TEST_V27_APPLY_RESPONSE_FILE=str(accepted_path),PHOTO_TEST_V27_DECISION_RESPONSE_FILE=str(decision_capture))
    def go(package,name,marker):
        evidence_root=Path(os.environ.get('PHOTO_TEST_PHYSICAL_CHILD_EVIDENCE_ROOT',str(repo/'work/removal-cleanup-child-evidence'))).resolve()
        assert evidence_root.is_relative_to(repo.resolve()) and not evidence_root.is_symlink()
        evidence=evidence_root/uuid4().hex;evidence.mkdir(parents=True,mode=0o700)
        command=['go','test','-json',package,'-run','^'+name+'$','-count=1','-timeout=90s']
        output='';stderr='';code=None;timed_out=False;original_error=None
        try:
            run=subprocess.run(command,cwd=repo/'backend',env=env,capture_output=True,text=True,timeout=120)
            output,stderr,code=run.stdout,run.stderr,run.returncode
        except subprocess.TimeoutExpired as error:
            original_error=error;timed_out=True
            output=error.stdout or '';stderr=error.stderr or ''
            if isinstance(output,bytes):output=output.decode(errors='replace')
            if isinstance(stderr,bytes):stderr=stderr.decode(errors='replace')
            raise
        except BaseException as error:
            original_error=error
            raise
        finally:
            try:
                for filename,body in [('go.jsonl',output),('stderr.log',stderr)]:
                    target=evidence/filename;target.write_text(body);target.chmod(0o600)
                metadata=dict(package='photoeditor/backend/'+package.removeprefix('./'),test=name,
                    marker=marker,returncode=code,timed_out=timed_out,
                    stdout_sha256=sha256(output.encode()).hexdigest(),stderr_sha256=sha256(stderr.encode()).hexdigest())
                target=evidence/'result.json';target.write_text(json.dumps(metadata,sort_keys=True)+'\n');target.chmod(0o600)
            except BaseException as evidence_error:
                if original_error is None:raise
                try:original_error.add_note('Child evidence persistence failed: '+type(evidence_error).__name__)
                except BaseException:pass
        assert code==0,output+stderr
        events=[json.loads(line) for line in output.splitlines() if line.startswith('{')]
        expected_package='photoeditor/backend/'+package.removeprefix('./')
        root_events=[event for event in events if event.get('Package')==expected_package and event.get('Test')==name]
        assert sum(event.get('Action')=='run' for event in root_events)==1
        assert [event['Action'] for event in root_events if event.get('Action') in ('pass','fail','skip')]==['pass']
        assert not any(event.get('Action') in ('skip','fail') for event in events)
        assert marker in output,output
        record_property(name,marker)

    # This is actual preview compute, typed uploads, commit and mounted preview finalization.
    go('./internal/gateway','TestIntegrationV27IndexedDownloads','V27_SHOWN_WORKER_WIRE_OK')
    from preview_late_access_fixture import assert_preview_late_access_loss
    assert_preview_late_access_loss(repo=repo,directory=tmp_path,environment=env,go=go,
        family='background_cleanup',params=params,actor=args[2],session=args[3],srgb_sha=document['srgb_sha'])
    go('./internal/httpapi','TestIntegrationBackgroundCleanupSelectedHTTPCapture','V27_BACKGROUND_CLEANUP_SELECTED_HTTP_OK')
    # Consume the exact fresh preview/status/targets/request captured above.
    from test_v27_semantic_qualification_objectstore import assert_live_vitest_report
    decision_report=tmp_path/'background-cleanup-decision-ui-vitest.json'
    decision_ui=subprocess.run(['npm','test','--','--run','src/semantic-review-decisions.integration.test.tsx',
        '--reporter=json','--outputFile='+str(decision_report)],cwd=repo/'apps/photographer',
        env=dict(os.environ,PHOTO_TEST_V27_APPLY_RESPONSE_FILE=str(decision_capture)),
        capture_output=True,text=True,timeout=40)
    assert decision_ui.returncode==0,decision_ui.stdout+decision_ui.stderr
    assert_live_vitest_report(decision_report,1,record_property)

    accepted=json.loads(accepted_path.read_text());child=accepted['child']['run_id']
    with psycopg.connect(**database) as db:
        rows=db.execute("SELECT t.id FROM photo.tasks t JOIN photo.semantic_targets s ON s.tenant_id=t.tenant_id AND s.child_run_id=t.run_id WHERE t.tenant_id=%s AND t.run_id=%s AND t.operation='semantic.adopt_shown_background_cleanup' AND t.state='ready' AND s.id=%s",(args[0],child,args[4])).fetchall()
        assert len(rows)==1
        task=str(rows[0][0])
    env.update(PHOTO_TEST_V27_GATEWAY_TASK=task,PHOTO_TEST_V27_SHOWN_OPERATION='semantic.adopt_shown_background_cleanup')
    go('./internal/gateway','TestIntegrationV27IndexedDownloads','V27_SHOWN_WORKER_WIRE_OK')
    capture=tmp_path/'background_cleanup-shown-applied-preview.json'
    env.update(PHOTO_TEST_V27_SHOWN_TASK=task,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture))
    go('./internal/httpapi','TestIntegrationShownAppliedPreviewCapture','V27_SHOWN_APPLIED_PREVIEW_HTTP_OK')
    from test_v27_semantic_qualification_objectstore import assert_live_vitest_report
    report=tmp_path/'background_cleanup-shown-ui.json'
    run=subprocess.run(['npm','test','--','--run','src/semantic-preview.integration.test.tsx','--reporter=json','--outputFile='+str(report)],cwd=repo/'apps/photographer',env=dict(os.environ,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture)),capture_output=True,text=True,timeout=40)
    assert run.returncode==0,run.stdout+run.stderr
    assert_live_vitest_report(report,1,record_property)
    record_property('provider_boundary','controlled big_lama infer only; real ICC/production orchestration, imported qualification; not photo/model quality')
    capture.unlink()
