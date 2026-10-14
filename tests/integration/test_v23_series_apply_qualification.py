"""A series Apply profile is pinned only from operator qualification."""

from uuid import uuid4


def test_series_apply_qualification_is_api_scoped_and_defaults_absent(db):
    signature = 'photo.v23_series_apply_qualification(uuid,uuid)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
                      ('photo_api', signature, 'EXECUTE')).fetchone()[0]
    for role in ('photo_worker', 'photo_scheduler', 'photo_gateway',
                 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
    tenant, project = uuid4(), uuid4()
    assert db.execute('SELECT * FROM photo.v23_series_apply_qualification(%s,%s)',
                      (tenant, project)).fetchone() is None
