"""Actual preview → selected HTTP Apply → shown-copy → applied-preview capture.

Qualifications are imported fixture reports, not measured model/photo quality.
All task outputs and adoption proofs come from production services/workers.
"""
import json
import os
import subprocess
from psycopg.conninfo import make_conninfo


def run_shown_capture_lifecycle(database,objects,bucket,tenant,scope,root,environment,record_property):
    from pathlib import Path
    import psycopg
    from semantic_qualification_fixture import frozen_qualification
    from test_v27_semantic_qualification_objectstore import assert_live_vitest_report
    repo=Path(__file__).resolve().parents[2]
    with psycopg.connect(**database) as db:
        pin=frozen_qualification(db,tenant,scope['project'],scope['actor'],objects=(objects,bucket),
            operation='semantic.adopt_shown_exposure',backend='cpu')
    env=dict(environment,PHOTO_TEST_V27_SHOWN_PIN=json.dumps(pin),
        PHOTO_TEST_V27_SHOWN_ADMISSION_FILE=str(root/'shown-admission.json'),
        PHOTO_TEST_V27_REPO_ROOT=str(repo))
    def go(package,name,marker):
        result=subprocess.run(['go','test',package,'-run','^'+name+'$','-count=1','-timeout=90s','-v'],
            cwd=repo/'backend',env=env,capture_output=True,text=True,timeout=100)
        assert result.returncode==0,result.stdout+result.stderr
        assert marker in result.stdout,result.stdout
        record_property(name,marker)
    env['PHOTO_TEST_V27_SHOWN_PHASE']='admit'
    go('./cmd/photo','TestIntegrationSemanticExposureBootstrap','V27_SHOWN_PREVIEW_ADMITTED')
    admitted=json.loads((root/'shown-admission.json').read_text())
    assert admitted['status']=='queued' and admitted['task_id'] and admitted['run_id']
    env['PHOTO_TEST_V27_GATEWAY_TASK']=admitted['task_id']
    go('./internal/gateway','TestIntegrationV27IndexedDownloads','V27_SHOWN_WORKER_WIRE_OK')
    env['PHOTO_TEST_V27_SHOWN_PHASE']='apply'
    env['PHOTO_TEST_V27_APPLY_RESPONSE_FILE']=str(root/'shown-apply.json')
    go('./cmd/photo','TestIntegrationSemanticExposureBootstrap','V27_SHOWN_APPLY_QUEUED')
    # The captured request contains the exact selected immutable Preview proof.
    # Run immediately after HTTP capture while signed preview media is fresh.
    decision_report=root/'shown-decision-ui-vitest.json'
    decision_environment=dict(os.environ,PHOTO_TEST_V27_APPLY_RESPONSE_FILE=str(root/'shown-apply.json'))
    decision_ui=subprocess.run(['npm','test','--','--run','src/semantic-review-decisions.integration.test.tsx',
        '--reporter=json','--outputFile='+str(decision_report)],cwd=repo/'apps/photographer',
        env=decision_environment,capture_output=True,text=True,timeout=40)
    assert decision_ui.returncode==0,decision_ui.stdout+decision_ui.stderr
    assert_live_vitest_report(decision_report,1,record_property)
    accepted=json.loads((root/'shown-apply.json').read_text())
    # Derive the copy from the actual immutable admission receipt, not a guessed ID.
    run=accepted['response']['child']['run_id']
    with psycopg.connect(**database) as db:
        rows=db.execute("SELECT id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND operation='semantic.adopt_shown_exposure' AND state='ready'",(tenant,run)).fetchall()
        assert len(rows)==1
        task=str(rows[0][0])
    env['PHOTO_TEST_V27_GATEWAY_TASK']=task
    go('./internal/gateway','TestIntegrationV27IndexedDownloads','V27_SHOWN_WORKER_WIRE_OK')
    capture=root/'shown-applied-preview.json'
    env.update(PHOTO_TEST_V27_SHOWN_TASK=task,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture))
    go('./internal/httpapi','TestIntegrationShownAppliedPreviewCapture','V27_SHOWN_APPLIED_PREVIEW_HTTP_OK')
    report=root/'shown-preview-ui-vitest.json'
    ui_env=dict(os.environ,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture))
    result=subprocess.run(['npm','test','--','--run','src/semantic-preview.integration.test.tsx',
        '--reporter=json','--outputFile='+str(report)],cwd=repo/'apps/photographer',env=ui_env,
        capture_output=True,text=True,timeout=40)
    assert result.returncode==0,result.stdout+result.stderr
    assert_live_vitest_report(report,1,record_property)
    capture.unlink()
