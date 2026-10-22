"""Authored SQL receipt boundary, not a training/graph execution qualification.

Uses the existing real V23 project/actor/session fixture. Accepted and pending
catalogue records are explicitly seeded by the fixture administrator; replay
itself uses only photo_api and never calls a scheduler admission function.
"""
from uuid import uuid4, uuid5

import pytest
from psycopg.errors import InsufficientPrivilege
from psycopg.types.json import Jsonb

from test_v23_record_correction import _fixture


def _receipt_fixture(db, accepted):
    tenant, actor, session, project, _, _ = _fixture(db)
    db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
    db.execute('SET LOCAL ROLE photo_api')
    profile = db.execute(
        'SELECT photo.create_v23_profile(%s,%s,%s,%s,%s,%s,%s)',
        (tenant, project, 'Receipt boundary', 'photographer', None,
         actor, session)).fetchone()[0]
    db.execute('RESET ROLE')
    key, request_sha = 'accepted-only-' + uuid4().hex, 'a' * 64
    run = uuid5(tenant, f'V2.Admit:{actor}:{key}')
    workflow, snapshot = uuid5(run, 'workflow'), uuid5(run, 'training.snapshot')
    db.execute('INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,'
               'principal_id,digest,item_count) '
               "VALUES(%s,%s,'v2','user',%s,%s,1)",
               (snapshot, tenant, actor, 'b' * 64))
    config = {'project_id': str(project), 'v23_training': {
        'schema_version': 1, 'profile_id': str(profile),
        'expected_revision': '1', 'manifest_sha': 'c' * 64,
        'snapshot_digest': 'b' * 64, 'snapshot_body_sha': 'd' * 64}}
    db.execute('INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,'
               'config_snapshot,config_hash,created_by,failure_policy,graph_sealed) '
               "VALUES(%s,%s,'v2',%s,%s,%s,%s,%s,'fail_fast',%s)",
               (run, tenant, 'queued' if accepted else 'draft', snapshot,
                Jsonb(config), 'e' * 64, actor, accepted))
    db.execute('INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,'
               'input_snapshot_id,workflow_type,renderer_version,policy_sha) '
               "VALUES(%s,%s,%s,%s,%s,'learning','v2.2',%s)",
               (workflow, tenant, project, run, snapshot, 'f' * 64))
    ticket = {'run_id': str(run), 'workflow_id': str(workflow), 'state': 'queued'}
    db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,'
               'principal_id,route,key,request_hash,resource_id,response_status,'
               'response_body,expires_at) '
               "VALUES(%s,'user',%s,'V2.Admit',%s,%s,%s,%s,%s,"
               "clock_timestamp()-interval '1 day')",
               (tenant, actor, key, request_sha, run,
                202 if accepted else None, Jsonb(ticket) if accepted else None))
    return (tenant, actor, session, profile, run, key, request_sha), ticket


def _read(db, args):
    return db.execute('SELECT photo.v23_training_accepted_receipt('
                      + ','.join(['%s'] * 7) + ')', args).fetchone()[0]


def test_accepted_training_receipt_is_api_only_readonly_and_scope_bound(db):
    args, ticket = _receipt_fixture(db, True)
    tenant, actor, session, profile, run, key, request_sha = args
    before = db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',
                        (run,)).fetchone()[0]
    ledger = db.execute('SELECT to_jsonb(i) FROM photo.idempotency_records i '
                        'WHERE tenant_id=%s AND principal_id=%s AND key=%s',
                        (tenant, actor, key)).fetchone()[0]
    counts = db.execute('SELECT (SELECT count(*) FROM photo.runs),'
                        '(SELECT count(*) FROM photo.tasks),'
                        '(SELECT count(*) FROM photo.quota_reservations)').fetchone()
    db.execute('SET LOCAL ROLE photo_api')
    # No scheduler session or grant is needed, even for an expired receipt TTL.
    assert db.execute("SELECT has_function_privilege(current_user,"
                      "'photo.v23_training_replay(uuid,uuid,uuid,uuid,text,text)',"
                      "'EXECUTE')").fetchone()[0] is False
    assert _read(db, args) == ticket
    assert _read(db, args) == ticket
    for index, replacement in ((0, uuid4()), (1, uuid4()), (2, uuid4()),
                               (3, uuid4()), (4, uuid4()), (6, '9' * 64)):
        altered = list(args)
        altered[index] = replacement
        assert _read(db, tuple(altered)) is None
    db.execute('RESET ROLE')
    assert db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',
                      (run,)).fetchone()[0] == before
    assert db.execute('SELECT to_jsonb(i) FROM photo.idempotency_records i '
                      'WHERE tenant_id=%s AND principal_id=%s AND key=%s',
                      (tenant, actor, key)).fetchone()[0] == ledger
    assert db.execute('SELECT (SELECT count(*) FROM photo.runs),'
                      '(SELECT count(*) FROM photo.tasks),'
                      '(SELECT count(*) FROM photo.quota_reservations)').fetchone() == counts
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',
               (session,))
    db.execute('SET LOCAL ROLE photo_api')
    assert _read(db, args) is None


def test_pending_training_is_not_exposed_or_admitted_by_api_receipt_lookup(db):
    args, _ = _receipt_fixture(db, False)
    db.execute('SET LOCAL ROLE photo_api')
    assert _read(db, args) is None
    assert _read(db, args) is None
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE id=%s',
                      (args[4],)).fetchone() == ('draft', False)
    assert db.execute('SELECT response_status,response_body FROM photo.idempotency_records '
                      'WHERE tenant_id=%s AND principal_id=%s AND key=%s',
                      (args[0], args[1], args[5])).fetchone() == (None, None)


@pytest.mark.parametrize("role", ["photo_scheduler", "photo_worker", "photo_portal"])
def test_training_accepted_receipt_rejects_other_application_roles(db, role):
    args, _ = _receipt_fixture(db, True)
    # Exercise EXECUTE itself: a negative catalogue privilege assertion alone
    # would not prove that a real application connection cannot read receipts.
    with pytest.raises(InsufficientPrivilege):
        with db.transaction():
            db.execute('SET LOCAL ROLE ' + role)
            _read(db, args)
    # The rejected call cannot consume or mutate the accepted receipt.
    db.execute('SET LOCAL ROLE photo_api')
    expected = {'run_id': str(args[4]),
                'workflow_id': str(uuid5(args[4], 'workflow')), 'state': 'queued'}
    assert _read(db, args) == expected
    db.execute('RESET ROLE')
