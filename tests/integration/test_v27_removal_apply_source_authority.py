"""Own removal source authority matrix; real SQL, synthetic object metadata.

Authored for deferred testing. Does not prove physical storage or model quality.
"""
import json
from uuid import uuid4

import psycopg
import pytest

from removal_apply_documents_fixture import prepared_review_fixture


PATHS = ('source', 'reference', 'mask', 'transform', 'protection_pin', 'protection')


def source_call(db, path, common, params, scope):
    if path in ('source', 'reference'):
        image = (json.loads(scope['intent_body'])['reference']['source']
                 if path == 'reference' else params['source']['source'])
        return db.execute(
            'SELECT * FROM photo.semantic_approved_image_object(' + ','.join(['%s'] * 13) + ')',
            (*common, path == 'reference', image['artifact_id'], image['sha256']),
        ).fetchall()
    if path in ('mask', 'transform'):
        return db.execute(
            'SELECT * FROM photo.semantic_approved_candidate_child(' + ','.join(['%s'] * 12) + ')',
            (*common, scope['candidate_body'].decode(), path),
        ).fetchall()
    if path == 'protection_pin':
        return db.execute(
            'SELECT * FROM photo.semantic_approved_protection_pin(' + ','.join(['%s'] * 10) + ')', common,
        ).fetchall()
    protection = scope['protection']['target_protection']
    return db.execute(
        'SELECT * FROM photo.semantic_approved_protection_object(' + ','.join(['%s'] * 12) + ')',
        (*common, protection['artifact_id'], protection['sha256']),
    ).fetchall()


@pytest.mark.parametrize('path', PATHS)
@pytest.mark.parametrize('changed_index', range(10))
def test_removal_apply_source_authority_refuses_changed_scope(db, path, changed_index):
    args, _, scope, _ = prepared_review_fixture(db)
    params = json.loads(args[5])
    common = [args[0], args[1], args[2], args[3], int(params['workflow_revision']),
              args[4], int(params['target_revision']), params['source']['asset_version_id'],
              params['candidate']['artifact_id'], params['candidate']['sha256']]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        rows = source_call(db, path, common, params, scope)
        assert len(rows) == 1, (path, rows)
        candidate = json.loads(scope['candidate_body'])
        expected = scope['protection']['target_protection'] if path.startswith('protection') else (
            candidate[path] if path in ('mask', 'transform') else (
                json.loads(scope['intent_body'])['reference']['source']
                if path == 'reference' else params['source']['source']))
        offset = 0 if path == 'protection_pin' else 1
        assert str(rows[0][offset]) == expected['artifact_id']
        assert rows[0][offset + 1] == expected['sha256']
        db.execute('RESET ROLE')
    changed = list(common)
    changed[changed_index] = (
        common[changed_index] + 1 if changed_index in (4, 6)
        else '0' * 64 if changed_index == 9 else uuid4())
    with db.transaction(force_rollback=True):
        with pytest.raises(psycopg.Error) as failure:
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_api')
                source_call(db, path, changed, params, scope)
        assert failure.value.sqlstate in ('42501', '40001'), failure.value
        assert 'SEMANTIC_' in str(failure.value)
