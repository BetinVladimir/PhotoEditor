"""Own retained retouch result projection; metadata authority, not object IO."""
from uuid import uuid4

import pytest
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from test_v27_retouch_child_finalization import finalize


def object_projection(db, values, task, *, actor=None, session=None):
    args = values[0][0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row = db.execute('SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type '
            'FROM photo.semantic_shown_applied_object(%s,%s,%s,%s)',
            (args[0], task['target_id'], args[2] if actor is None else actor,
             args[3] if session is None else session)).fetchone()
        db.execute('RESET ROLE')
        return row


@pytest.mark.parametrize('with_alpha', [False, True])
def test_retouch_object_projection_requires_completed_retained_child(database, with_alpha):
    values, task, commit = successful_copy_metadata_fixture(database, with_alpha=with_alpha)
    assert object_projection(database, values, task) is None
    assert adopt(database, commit) is True
    assert object_projection(database, values, task) is None
    assert finalize(database, commit[0], values[4]) == 'completed'
    stored = database.execute('SELECT id,sha256,bytes,bucket,object_key,object_version,media_type '
        'FROM photo.artifacts WHERE tenant_id=%s AND id=%s', (commit[0], commit[10])).fetchone()
    assert object_projection(database, values, task) == ('WorkingImage', *stored)
    assert object_projection(database, values, task, actor=uuid4()) is None
    assert object_projection(database, values, task, session=uuid4()) is None
    assert object_projection(database, values, task) == ('WorkingImage', *stored)


@pytest.mark.parametrize('mutation', ['expired', 'unverified', 'cancelled_child', 'revoked_session'])
def test_retouch_object_projection_refuses_stale_result(database, mutation):
    values, task, commit = successful_copy_metadata_fixture(database, with_alpha=True)
    assert adopt(database, commit) is True
    assert finalize(database, commit[0], values[4]) == 'completed'
    before = object_projection(database, values, task)
    assert before is not None
    with database.transaction(force_rollback=True):
        if mutation == 'expired':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                'WHERE tenant_id=%s AND id=%s', (commit[0], commit[10]))
        elif mutation == 'unverified':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (commit[0], commit[10]))
        elif mutation == 'cancelled_child':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',
                (commit[0], values[4]))
        else:
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (values[0][0][3],))
        assert object_projection(database, values, task) is None
    assert object_projection(database, values, task) == before
