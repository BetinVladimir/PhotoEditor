"""Work-only Go consumer wrapper; requires a legally prepared detail graph.

This file deliberately cannot substitute the generic no-detail training fixture.
The controlled producer and real compiler wiring are work-only and unexecuted.
"""
import json
import os
import re
import signal
from hashlib import sha256
from pathlib import Path
import psycopg
import pytest
import subprocess

from psycopg.conninfo import make_conninfo
from conftest import ROOT


def run_detail_binding_go(database, ids, *, evidence_path):
    """Run exactly the existing Go negative matrix; require an executed PASS."""
    assert len(ids) == 5 and all(ids)
    assert re.fullmatch(r'photo_test_[0-9a-f]{32}', database['dbname'])
    with psycopg.connect(**database) as db:
        assert db.execute('SELECT current_database()').fetchone() == (database['dbname'],)
        assert_detail_graph(db, ids)
    source = ROOT / 'backend/internal/v2learning/evaluate_detail_protection_integration_test.go'
    before = sha256(source.read_bytes()).hexdigest()
    expected = json.loads((Path(__file__).with_name('source-sha256.json')).read_text())
    assert before == expected['backend/internal/v2learning/evaluate_detail_protection_integration_test.go']
    env = {key: value for key, value in os.environ.items() if not key.startswith('PHOTO_')}
    env.update(PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
               PHOTO_TEST_V23_DETAIL_EVALUATE_IDS=','.join(map(str, ids)),
               GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local',
               GOCACHE=str(ROOT / 'work' / 'go-cache'),
               GOMODCACHE=str(ROOT / '.cache' / 'go-mod'),
               GOPATH=str(ROOT / '.runtime' / 'go'),
               TMPDIR=str(ROOT / '.cache' / 'tmp'))
    command = ['go', 'test', '-json', '-p', '1', './internal/v2learning',
               '-run', '^TestIntegrationEvaluateDetailProtectionBinding$',
               '-count=1', '-timeout=60s']
    evidence_path.mkdir(parents=True, exist_ok=False)
    timed_out = False
    with (evidence_path / 'go.jsonl').open('w') as stdout, (evidence_path / 'stderr.log').open('w') as stderr:
        process = subprocess.Popen(command, cwd=ROOT / 'backend', env=env,
                                   stdout=stdout, stderr=stderr, start_new_session=True)
        try:
            code = process.wait(timeout=90)
        except subprocess.TimeoutExpired:
            timed_out = True
            try: os.killpg(process.pid, signal.SIGINT)
            except ProcessLookupError: pass
            try:
                code = process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try: os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError: pass
                code = process.wait(timeout=5)
    after = sha256(source.read_bytes()).hexdigest()
    result = dict(command=command, exit=code, timed_out=timed_out,
                  source_before=before, source_after=after,
                  source_unchanged=before == after)
    (evidence_path / 'result.json').write_text(json.dumps(result, indent=2)+'\n')
    assert before == after, result
    assert not timed_out and code == 0, result
    output = (evidence_path / 'go.jsonl').read_text()
    events = [json.loads(line) for line in output.splitlines()
              if line.startswith('{')]
    name = 'TestIntegrationEvaluateDetailProtectionBinding'
    exact = [event for event in events if event.get('Test') == name]
    assert any(event.get('Action') == 'run' for event in exact)
    assert [event['Action'] for event in exact if event.get('Action')
            in ('pass', 'fail', 'skip')] == ['pass'], exact
    assert not any(event.get('Action') in ('skip', 'fail') for event in events)


def assert_detail_graph(db, ids):
    """Reject vacuous protection-current=true and mismatched lineage before Go."""
    tenant, run, expansion, token, task = ids
    row = db.execute('''SELECT t.state,t.operation,t.operation_version,
      t.execution_profile ? 'v2_input_manifest',e.kind,e.state,
      e.expansion_lease_until>clock_timestamp(),fit.state,fit.canonical_result_id IS NOT NULL
      FROM photo.tasks t JOIN photo.graph_expansions e
      ON e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.id=%s
      JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND fit.id=e.parent_task_id
      AND fit.run_id=e.run_id AND fit.operation='learning.fit'
      WHERE t.tenant_id=%s AND t.run_id=%s AND t.id=%s AND e.expansion_token=%s''',
      (expansion, tenant, run, task, token)).fetchone()
    assert row == ('blocked', 'learning.evaluate', '1', True,
                   'v2.learning.evaluate/1', 'open', True, 'succeeded', True)
    rows = db.execute('''SELECT d.owner_id,i.artifact_id,i.sha256
      FROM photo.v2_task_inputs i JOIN photo.backend_documents d
      ON d.tenant_id=i.tenant_id AND d.run_id=%s AND d.artifact_id=i.artifact_id
      AND d.sha256=i.sha256 AND d.purpose='v23_detail_holdout' AND d.state='ready'
      WHERE i.tenant_id=%s AND i.task_id=%s
      AND i.contract='FrozenPortraitDetailHoldoutEvidence' ''',
      (run, tenant, task)).fetchall()
    assert len(rows) == 1, 'one real linked detail holdout required, not zero-input success'
    for predicate in ('v23_evaluate_detail_closure_current',
                      'v23_evaluate_detail_protection_current'):
        assert db.execute('SELECT photo.'+predicate+'(%s,%s,%s,%s,%s)', ids).fetchone() == (True,)
    return rows[0]


def test_detail_evaluate_protection_real_consumer(database):
    """Controlled relational producer -> real sealed closure -> existing Go matrix.

    This proves SQL lineage and binding only, not S3 bytes, inference or quality.
    The companion Go exporter must first be landed in internal/v2learning.
    """
    from uuid import uuid4
    from detail_closure_compiler import ClosureCompiler
    from detail_training_fixture import detail_training
    evidence = ROOT / 'work' / ('v23-detail-evaluate-executed-' + uuid4().hex)
    compiler = ClosureCompiler(evidence / 'compiler')
    with psycopg.connect(**database) as db:
        assert re.fullmatch(r'photo_test_[0-9a-f]{32}', database['dbname'])
        assert db.execute('SELECT current_database()').fetchone() == (database['dbname'],)
        ids = detail_training(db, qualification={}, compiler=compiler)
        assert_detail_graph(db, ids)
        db.commit()  # Go uses another connection; fixture database owns cleanup.
    run_detail_binding_go(database, ids, evidence_path=evidence / 'consumer')


@pytest.mark.migration_head(2591)
def test_detail_evaluate_upgrade_actual_apply_preserves_history(database):
    from uuid import uuid4
    from detail_closure_compiler import ClosureCompiler
    from detail_training_fixture import detail_training
    from detail_producer_checks import upgrade_at_fresh_event
    patch=(ROOT/'backend/db/migrations/02592_v23_apply_detail_producer_domain.sql').read_text().split('-- +goose Down')[0]
    lock_patch=(ROOT/'backend/db/migrations/02593_v23_detail_event_row_lock_privilege.sql').read_text().split('-- +goose Down')[0]
    link_patch=(ROOT/'backend/db/migrations/02594_v23_detail_link_node_row_lock_privilege.sql').read_text().split('-- +goose Down')[0]
    evidence=ROOT/'work'/('v23-detail-evaluate-upgrade-'+uuid4().hex)
    compiler=ClosureCompiler(evidence/'compiler')
    earlier=[]
    controls=[]
    qualification={}
    def upgrade(db,tenant,project,actor,session,event,producer):
        fresh,old=upgrade_at_fresh_event(db,tenant,project,actor,session,event,producer,(patch,lock_patch,link_patch),preservation_tenants=[controls[0]['tenant']])
        earlier.append((tenant,old))
        return fresh
    with psycopg.connect(**database) as db:
        from test_consent_authorized_training_withdrawal import _adopted_training, _fixture
        # Existing independent granted-training metadata graph; no detail-positive claim.
        control_base=_fixture(db,qualification=qualification)
        control_training=_adopted_training(db,qualification=qualification,base=control_base,stop_before_evaluate_result=True)
        controls.append(dict(tenant=control_base[0],training=control_training))
        ids=detail_training(db,qualification=qualification,compiler=compiler,before_detail_bind=upgrade)
        assert_detail_graph(db,ids)
        assert len(earlier)==1
        assert db.execute('SELECT count(*) FROM photo.v23_portrait_detail_event_links WHERE tenant_id=%s AND event_id=%s',earlier[0]).fetchone()==(0,)
        db.commit()
    run_detail_binding_go(database,ids,evidence_path=evidence/'consumer')


def test_detail_producer_rejects_committed_prior_event(database):
    """Late bind refuses actual committed xmin; fresh event still fully usable."""
    from uuid import uuid4
    from detail_closure_compiler import ClosureCompiler
    from detail_training_fixture import detail_training
    from detail_producer_checks import select_pair
    evidence=ROOT/'work'/('v23-detail-historical-event-'+uuid4().hex)
    compiler=ClosureCompiler(evidence/'compiler')
    old_events=[]
    def committed_prior(db,tenant,project,actor,session,event,producer):
        baseline=select_pair(db,tenant,project,actor,session,event)
        assert len(baseline[0])==len(baseline[1])==1
        db.commit()  # Own DSN connection, no conftest db rollback transaction.
        xid=db.execute('SELECT xmin::text::bigint,txid_current()%%4294967296 FROM photo.correction_events WHERE tenant_id=%s AND id=%s',
            (tenant,event)).fetchone()
        assert xid[0]!=xid[1]
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        assert db.execute('SELECT photo.bind_v23_portrait_detail_event('+','.join(['%s']*9)+')',
            (tenant,project,actor,session,event,producer['run'],producer['task'],producer['result'],producer['attempt'])).fetchone()==(False,)
        db.execute('RESET ROLE')
        assert select_pair(db,tenant,project,actor,session,event)==baseline
        assert db.execute('SELECT count(*) FROM photo.v23_portrait_detail_event_links WHERE tenant_id=%s AND event_id=%s',
            (tenant,event)).fetchone()==(0,)
        fresh=db.execute('INSERT INTO photo.correction_events(tenant_id,project_id,session_id,asset_version_id,'
            'base_document_id,approved_document_id,changed_paths,reason,learn,consent_id,consent_epoch,approved_by,client_event_id) '
            'SELECT tenant_id,project_id,session_id,asset_version_id,base_document_id,approved_document_id,changed_paths,'
            'reason,learn,consent_id,consent_epoch,approved_by,%s FROM photo.correction_events WHERE tenant_id=%s AND id=%s RETURNING id',
            (uuid4(),tenant,event)).fetchone()[0]
        assert fresh!=event
        old_events.append((tenant,event))
        return fresh  # Normal builder invokes actual bind in this INSERT transaction.
    with psycopg.connect(**database) as db:
        assert re.fullmatch(r'photo_test_[0-9a-f]{32}',database['dbname'])
        assert db.execute('SELECT current_database()').fetchone()==(database['dbname'],)
        ids=detail_training(db,qualification={},compiler=compiler,before_detail_bind=committed_prior)
        assert_detail_graph(db,ids)
        assert len(old_events)==1
        assert db.execute('SELECT count(*) FROM photo.v23_portrait_detail_event_links WHERE tenant_id=%s AND event_id=%s',old_events[0]).fetchone()==(0,)
        db.commit()
    run_detail_binding_go(database,ids,evidence_path=evidence/'consumer')
