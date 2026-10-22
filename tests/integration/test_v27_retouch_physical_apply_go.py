"""Real CPU preview -> Go physical resolver -> atomic Apply -> saved replay.

No synthetic verified preview markers or manual chosen output refs are supplied.
Imported qualification/landmark fixture data do not certify photographic quality.
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
from retouch_operator_fixture import retouch_preview_operator_enabled

pytestmark = pytest.mark.usefixtures('retouch_preview_operator_enabled')
from retouch_physical_apply_fixture import physical_apply_document
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize('transport', ['service', 'http'])
@pytest.mark.parametrize('alpha', [False, True])
def test_physical_retouch_resolver_and_apply_from_real_cpu_result(database, lens_objects, tmp_path, alpha, transport, record_property):
    repo = Path(__file__).resolve().parents[2]
    scratch = tmp_path / 'scratch'
    scratch.mkdir(mode=0o700)
    with psycopg.connect(**database) as db:
        raw, prepared, _, _ = physical_apply_document(db, lens_objects,
            scratch, with_alpha=alpha)
        assert db.execute('SELECT v27_retouch_preview_receipt FROM photo.runs WHERE id=%s',
            (prepared['run'],)).fetchone() == (None,)
        db.commit()
    document = tmp_path / 'retouch-apply-fixture.json'
    document.write_bytes(raw)
    document.chmod(0o600)
    client, bucket = lens_objects
    credentials = client._provider.retrieve()
    environment = dict(os.environ,
        PHOTO_TEST_DATABASE_URL=make_conninfo(**database), PHOTO_TEST_DISPOSABLE='1',
        PHOTO_TEST_V27_RETOUCH_APPLY_FIXTURE=str(document),
        PHOTO_TEST_MINIO_URL='http://' + client._base_url.host,
        PHOTO_TEST_MINIO_BUCKET=bucket, PHOTO_TEST_MINIO_ACCESS=credentials.access_key,
        PHOTO_TEST_MINIO_SECRET=credentials.secret_key)
    package='./internal/semanticreview' if transport=='service' else './internal/httpapi'
    test_name='TestIntegrationShownRetouchSQLResolverAndBatchPublication' if transport=='service' else 'TestIntegrationRetouchSelectedHTTPCapture'
    marker='V27_RETOUCH_PHYSICAL_APPLY_OK' if transport=='service' else 'V27_RETOUCH_SELECTED_HTTP_OK'
    accepted_file=tmp_path/'retouch-selected-apply.json'
    environment['PHOTO_TEST_V27_APPLY_RESPONSE_FILE']=str(accepted_file)
    def go(package,name,marker):
        evidence_root=Path(os.environ.get('PHOTO_TEST_PHYSICAL_CHILD_EVIDENCE_ROOT',str(repo/'work/removal-cleanup-child-evidence'))).resolve()
        assert evidence_root.is_relative_to(repo.resolve()) and not evidence_root.is_symlink()
        evidence=evidence_root/uuid4().hex;evidence.mkdir(parents=True,mode=0o700)
        command=['go','test','-json',package,'-run','^'+name+'$','-count=1','-timeout=90s']
        output='';stderr='';code=None;timed_out=False;original_error=None
        try:
            run=subprocess.run(command,cwd=repo/'backend',env=environment,capture_output=True,text=True,timeout=120)
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
        return run
    from preview_late_access_fixture import assert_preview_late_access_loss
    assert_preview_late_access_loss(repo=repo,directory=tmp_path,environment=environment,go=go,
        family='retouch',params=prepared['scope']['retouch_params'],
        actor=prepared['args'][2],session=prepared['args'][3],srgb_sha=json.loads(raw)['srgb_sha'])
    result = go(package,test_name,marker)
    assert result.returncode == 0, result.stdout + result.stderr
    assert marker in result.stdout
    assert '--- PASS: '+test_name in result.stdout

    with psycopg.connect(**database) as db:
        tenant = prepared['args'][0]
        target_state, child, batch = db.execute(
            'SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
            (tenant, prepared['target'])).fetchone()
        assert target_state == 'queued' and child is not None and batch is not None
        assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
            (tenant, child)).fetchone() == ('queued', True)
        assert db.execute('SELECT operation,state FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',
            (tenant, child)).fetchall() == [('semantic.adopt_shown_retouch_region', 'ready')]
        assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND kind='apply'",
            (tenant,)).fetchone() == (1,)
        assert db.execute('SELECT child_run_id FROM photo.v2_review_batches WHERE tenant_id=%s AND id=%s',
            (tenant, batch)).fetchone() == (child,)
        assert db.execute("SELECT state FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",
            (tenant, child)).fetchone() == ('reserved',)
        assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v27.review.batch'",
            (tenant,)).fetchone() == (1,)
        params = prepared['scope']['retouch_params']
        assert db.execute('SELECT revision FROM photo.runs WHERE id=%s',
            (prepared['parent'],)).fetchone() == (int(params.workflow_revision) + 1,)

    if transport=='service':
        return  # Preserve the original service/resolver/replay case unchanged.
    accepted=json.loads(accepted_file.read_text())
    assert accepted['child']['run_id']==str(child)
    with psycopg.connect(**database) as db:
        rows=db.execute("SELECT id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND operation='semantic.adopt_shown_retouch_region' AND state='ready'",(tenant,child)).fetchall()
        assert len(rows)==1
        task=str(rows[0][0])
    environment.update(PHOTO_TEST_V27_GATEWAY_TASK=task,PHOTO_TEST_V27_REPO_ROOT=str(repo),
        PHOTO_TEST_V27_SHOWN_OPERATION='semantic.adopt_shown_retouch_region')
    # Real leased copy, canonical commit, independent adopter and mounted recovery.
    go('./internal/gateway','TestIntegrationV27IndexedDownloads','V27_SHOWN_WORKER_WIRE_OK')
    capture=tmp_path/'retouch-shown-applied-preview.json'
    environment.update(PHOTO_TEST_V27_SHOWN_TASK=task,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture))
    go('./internal/httpapi','TestIntegrationShownAppliedPreviewCapture','V27_SHOWN_APPLIED_PREVIEW_HTTP_OK')
    from test_v27_semantic_qualification_objectstore import assert_live_vitest_report
    report=tmp_path/'retouch-shown-ui.json'
    ui_env=dict(os.environ,PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture))
    result=subprocess.run(['npm','test','--','--run','src/semantic-preview.integration.test.tsx',
        '--reporter=json','--outputFile='+str(report)],cwd=repo/'apps/photographer',env=ui_env,
        capture_output=True,text=True,timeout=40)
    assert result.returncode==0,result.stdout+result.stderr
    assert_live_vitest_report(report,1,record_property)
    capture.unlink()
