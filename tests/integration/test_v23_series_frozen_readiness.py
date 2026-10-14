"""The future V23 series freeze check is owner-only and fails closed.

Positive physical Apply/Training publication is exercised in the later full
workflow suite, after the code-first phase applies the forward migrations.
"""

from uuid import uuid4


def test_series_frozen_readiness_denies_missing_authority(db):
    signature = 'photo.v23_series_frozen_ready(uuid,uuid,jsonb)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    assert db.execute(
        'SELECT p.proowner::regrole::text FROM pg_proc p '
        'WHERE p.oid=to_regprocedure(%s)',
        (signature,)).fetchone()[0] == 'photo_fn_owner'
    for role in ('photo_api', 'photo_scheduler', 'photo_worker',
                 'photo_gateway', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
    assert not db.execute(
        'SELECT photo.v23_series_frozen_ready(%s,%s,%s::jsonb)',
        (uuid4(), uuid4(), '{"examples":[]}')).fetchone()[0]
