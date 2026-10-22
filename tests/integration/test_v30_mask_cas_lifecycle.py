"""Rollback-only CAS lifecycle on a dedicated, previously imported fixture.

PHOTO_TEST_MASK_CAS_JSON carries tenant/actor/session/workflow plus a full grant
request with expected_epoch=0. PHOTO_TEST_DATABASE_URL must address an isolated
DB migrated through 02260. This test never creates fake grants or qualifications.
Do not use the suite's fresh-database fixture: the imported artifact closure must
already exist in the explicitly supplied database.
"""
import json
import os

import psycopg
import pytest


def test_mask_cas_grant_revoke_delayed_replay_and_explicit_regrant():
    raw = os.environ.get('PHOTO_TEST_MASK_CAS_JSON')
    dsn = os.environ.get('PHOTO_TEST_DATABASE_URL')
    if not raw or not dsn:
        pytest.skip('isolated imported-mask closure and migrated database required')
    fixture = json.loads(raw)
    request = fixture['grant']
    assert request['expected_epoch'] == 0 and request['action'] == 'grant'
    tenant, actor, session, workflow = (fixture[key] for key in ('tenant', 'actor', 'session', 'workflow'))
    grant_args = (tenant, actor, session, workflow, request['entry_id'], request['parsed_jcs'],
                  request['recipe_artifact_id'], request['recipe_jcs'], request['override_artifact_id'],
                  request['override_jcs'], request['mask_jcs'], request['target_version_id'])
    revoke_args = (tenant, actor, session, workflow, request['entry_id'],
                   request['override_artifact_id'], request['mask_artifact_id'], request['target_version_id'])
    with psycopg.connect(dsn) as db:
        with db.transaction(force_rollback=True):
            db.execute("SELECT set_config('app.tenant_id',%s,true)", (tenant,))
            db.execute('SET LOCAL ROLE photo_api')

            def grant(epoch):
                return db.execute('SELECT photo.grant_v30_import_person_mask_cas(' + ','.join(['%s'] * 13) + ')', grant_args + (epoch,)).fetchone()[0]

            def revoke(epoch):
                return db.execute('SELECT photo.revoke_v30_import_person_mask_cas(' + ','.join(['%s'] * 9) + ')', revoke_args + (epoch,)).fetchone()[0]

            assert grant(0) == 1, 'fixture must admit its complete original grant'
            assert grant(0) == 1, 'exact grant replay must retain epoch'
            assert revoke(1) == 2
            assert revoke(1) == 2, 'exact revoke replay must retain epoch'
            assert grant(0) is None, 'late grant must never undo revoke'
            assert revoke(0) is None
            assert grant(2) == 3, 'new explicit decision may grant again'
            assert grant(2) == 3
            assert revoke(1) is None, 'old revoke must not undo new grant'
            assert revoke(3) == 4
            assert grant(2) is None
            for epoch in (None, -1, 9007199254740991):
                assert grant(epoch) is None
                assert revoke(epoch) is None
            rows = db.execute('SELECT status,epoch FROM photo.v30_import_person_mask_confirmation_status(%s,%s,%s,%s,%s)',
                              (tenant, actor, session, fixture['project'], workflow)).fetchall()
            assert ('revoked', 4) in rows
