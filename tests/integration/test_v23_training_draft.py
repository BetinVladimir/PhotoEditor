"""Learning draft pins its future ArtifactSnapshot and document identities."""

from uuid import uuid4, uuid5

import pytest
from psycopg import errors
from psycopg.types.json import Jsonb

from test_v23_record_correction import _fixture


def test_learning_draft_and_deterministic_document_reservations(db):
    tenant, actor, auth_session, project, correction_request, consent_id = _fixture(db)
    db.execute('INSERT INTO photo.tenant_usage(tenant_id) VALUES(%s)', (tenant,))
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    db.execute('SET LOCAL ROLE photo_api')
    profile = db.execute(
        'SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)',
        (tenant, project, 'Training', 'photographer', None,
         actor, auth_session)).fetchone()[0]
    key = 'train-draft-' + uuid4().hex
    run = uuid5(tenant, f'V2.Admit:{actor}:{key}')
    workflow = uuid5(run, 'workflow')
    snapshot = uuid5(run, 'training.snapshot')
    config = {'project_id': str(project), 'v23_training': {
        'schema_version': 1, 'profile_id': str(profile),
        'expected_revision': '1', 'manifest_sha': 'a' * 64,
        'snapshot_digest': 'b' * 64, 'snapshot_body_sha': 'c' * 64}}
    args = (tenant, run, workflow, profile, 1, actor, auth_session,
            'd' * 64, 'e' * 64, Jsonb(config), 4096, key, 'f' * 64)
    call = 'SELECT * FROM photo.begin_v23_training_draft(' + ','.join(['%s'] * 13) + ')'
    assert db.execute(call, args).fetchone() == ('created', run, snapshot)
    assert db.execute(call, args).fetchone() == ('pending', run, snapshot)
    item_count, sealed, expires = db.execute(
        'SELECT s.item_count,r.graph_sealed,s.expires_at FROM photo.snapshots s '
        'JOIN photo.runs r ON r.tenant_id=s.tenant_id AND r.input_snapshot_id=s.id '
        'WHERE r.id=%s', (run,)).fetchone()
    assert item_count == 1 and sealed is False and expires is not None
    assert db.execute('SELECT count(*) FROM photo.snapshot_items WHERE snapshot_id=%s',
                      (snapshot,)).fetchone()[0] == 0
    db.execute('RESET ROLE')
    db.execute('SET LOCAL ROLE photo_scheduler')
    reserve = 'SELECT * FROM photo.reserve_v23_training_document(%s,%s,%s,%s,%s,%s,%s)'
    reservations = []
    for owner, purpose, sha, size in (
        (profile, 'v23_training_manifest', 'a' * 64, 512),
        (run, 'v23_training_snapshot', 'c' * 64, 256),
    ):
        outcome, artifact_id, token, ready = db.execute(
            reserve, (tenant, run, owner, purpose, sha, size, 'test-bucket')).fetchone()
        assert outcome == 'created' and artifact_id == uuid5(run, purpose)
        assert token is not None and ready is False
        reservations.append((owner, purpose, sha, size, artifact_id, token))
    assert db.execute(reserve, (tenant, run, uuid4(),
                               'v23_training_manifest', 'a' * 64, 512,
                               'test-bucket')).fetchone()[0] == 'forbidden'
    for owner, purpose, sha, size, artifact_id, token in reservations:
        assert db.execute(
            'SELECT photo.complete_v23_training_document('
            + ','.join(['%s'] * 10) + ')',
            (tenant, run, owner, purpose, artifact_id, token, sha,
             size, 'test-bucket', 'version-1')).fetchone()[0] is True
    db.execute('RESET ROLE')
    collection = db.execute('SELECT collection_id FROM photo.shoot_sessions '
                            'WHERE id=%s', (correction_request['session_id'],)).fetchone()[0]
    sessions = [correction_request['session_id']]
    for index in range(5):
        sid = db.execute('INSERT INTO photo.shoot_sessions('
                         'tenant_id,project_id,collection_id,name,state,policy,consent_epoch) '
                         "VALUES(%s,%s,%s,%s,'open','{}',1) RETURNING id",
                         (tenant, project, collection, f'Extra {index}')).fetchone()[0]
        sessions.append(str(sid))
    manifest = {'schema_version': 1, 'profile_id': str(profile),
                'mode': 'photographer', 'person_id': None,
                'policy_sha': 'd' * 64,
                'consents': [{'consent_id': str(consent_id), 'epoch': '1'}],
                'examples': [
                    {'event_id': str(uuid4()), 'session_id': sessions[i % 6]}
                    for i in range(40)]}
    db.execute('SET LOCAL ROLE photo_scheduler')
    frozen = db.execute('SELECT photo.freeze_v23_training_snapshot('
                        + ','.join(['%s'] * 9) + ')',
                        (tenant, run, profile, actor, auth_session, 1,
                         Jsonb(manifest), 'a' * 64, 'c' * 64)).fetchone()[0]
    assert frozen is False
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.snapshot_items '
                      'WHERE snapshot_id=%s', (snapshot,)).fetchone()[0] == 0
    with pytest.raises(errors.CheckViolation), db.transaction():
        db.execute("UPDATE photo.runs SET status='queued' WHERE id=%s", (run,))
    assert db.execute('SELECT status FROM photo.runs WHERE id=%s',
                      (run,)).fetchone()[0] == 'draft'
