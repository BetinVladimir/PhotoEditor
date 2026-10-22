"""Transactional Download event projection; authored, not executed."""
from uuid import uuid4
import pytest
from test_database import tenant


def download_job(db):
    tid, actor = tenant(db, str(uuid4())+'@download-events.test')
    project = db.execute("INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES(%s,'Events','workspace',true) RETURNING id", (tid,)).fetchone()[0]
    collection = db.execute("INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Events','draft') RETURNING id", (tid, project)).fetchone()[0]
    snapshot = db.execute("INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES(%s,'selection',%s,1,'user',%s,%s,1) RETURNING id", (tid, collection, actor, 'a'*64)).fetchone()[0]
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'download','queued',%s,'{}',%s,%s,false) RETURNING id", (tid, snapshot, 'b'*64, actor)).fetchone()[0]
    job = db.execute("INSERT INTO photo.download_jobs(tenant_id,collection_id,principal_type,principal_id,snapshot_id,preset,status,run_id) VALUES(%s,%s,'user',%s,%s,'{}','queued',%s) RETURNING id", (tid, collection, actor, snapshot, run)).fetchone()[0]
    return tid, actor, collection, run, job


def events(db, job):
    return db.execute("SELECT principal_id,collection_id,payload FROM photo.run_events WHERE type='download_status' AND payload->>'download_job_id'=%s", (str(job),)).fetchall()


@pytest.mark.parametrize('state', ['ready', 'failed', 'cancelled', 'expired'])
def test_download_terminal_status_has_one_transactional_event_and_outbox(db, state):
    tid, actor, collection, run, job = download_job(db)
    db.execute('UPDATE photo.download_jobs SET status=%s WHERE id=%s', (state, job))
    db.execute('SET CONSTRAINTS photo.download_terminal_events IMMEDIATE')
    rows = events(db, job)
    assert len(rows) == 1 and rows[0][:2] == (actor, collection)
    assert rows[0][2] == {'download_job_id': str(job), 'run_id': str(run), 'status': state}
    outbox = db.execute("SELECT payload FROM photo.outbox WHERE tenant_id=%s AND routing_key='download_status' AND payload->>'download_job_id'=%s", (tid, str(job))).fetchall()
    assert len(outbox) == 1 and outbox[0][0]['status'] == state
    db.execute('UPDATE photo.download_jobs SET status=%s WHERE id=%s', (state, job))
    assert len(events(db, job)) == 1


def test_download_nonterminal_progress_does_not_emit_terminal_event(db):
    _, _, _, _, job = download_job(db)
    for state in ['preparing', 'building', 'uploading']:
        db.execute('UPDATE photo.download_jobs SET status=%s WHERE id=%s', (state, job))
    db.execute('SET CONSTRAINTS photo.download_terminal_events IMMEDIATE')
    assert events(db, job) == []


def test_download_event_rollback_and_final_projection(db):
    _, _, _, _, job = download_job(db)
    db.execute('SAVEPOINT download_event')
    db.execute("UPDATE photo.download_jobs SET status='failed' WHERE id=%s", (job,))
    db.execute('SET CONSTRAINTS photo.download_terminal_events IMMEDIATE')
    assert len(events(db, job)) == 1
    db.execute('ROLLBACK TO SAVEPOINT download_event')
    assert events(db, job) == []
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE routing_key='download_status' AND payload->>'download_job_id'=%s", (str(job),)).fetchone()[0] == 0
    db.execute('SET CONSTRAINTS photo.download_terminal_events DEFERRED')
    db.execute("UPDATE photo.download_jobs SET status='ready' WHERE id=%s", (job,))
    db.execute("UPDATE photo.download_jobs SET status='expired' WHERE id=%s", (job,))
    db.execute('SET CONSTRAINTS photo.download_terminal_events IMMEDIATE')
    assert [row[2]['status'] for row in events(db, job)] == ['expired']


def test_portal_event_expiry_uses_same_principal_scope_as_rows(db):
    from test_portal_item_download import setup_downloadable_item
    tid, share, session, session_hash, _, _, _, _ = setup_downloadable_item(db)
    collection = db.execute('SELECT collection_id FROM photo.share_links WHERE id=%s', (share,)).fetchone()[0]
    for cursor, principal in [(10, uuid4()), (20, session)]:
        db.execute("INSERT INTO photo.run_events(tenant_id,cursor,collection_id,principal_id,audience,type,payload) VALUES(%s,%s,%s,%s,'principal','download_status','{}')", (tid, cursor, collection, principal))
    # A foreign principal's retained event must not suppress the scoped expiry.
    assert db.execute('SELECT outcome FROM photo.list_portal_events(%s,15,100)', (session_hash,)).fetchone()[0] == 'expired'
    page = db.execute('SELECT outcome,cursor FROM photo.list_portal_events(%s,0,100)', (session_hash,)).fetchall()
    assert page == [('ok', 20)]


def test_download_repeated_terminal_transitions_in_one_commit_publish_once(db):
    _, _, _, _, job = download_job(db)
    # Every queued deferred trigger observes the same final row. Deterministic
    # identity must suppress duplicate events from repeated surviving status.
    for state in ['ready', 'expired', 'expired']:
        db.execute('UPDATE photo.download_jobs SET status=%s WHERE id=%s', (state, job))
    db.execute('SET CONSTRAINTS photo.download_terminal_events IMMEDIATE')
    assert [row[2]['status'] for row in events(db, job)] == ['expired']
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE routing_key='download_status' AND payload->>'download_job_id'=%s", (str(job),)).fetchone()[0] == 1
