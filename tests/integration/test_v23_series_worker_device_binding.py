"""Operator-provisioned series device pins are enforced by installed SQL."""

from uuid import uuid4


def test_series_device_binding_is_private_and_fails_closed(db):
    signature = 'photo.v23_series_worker_device_matches(uuid,uuid,jsonb)'
    assert db.execute('SELECT to_regprocedure(%s) IS NOT NULL',
                      (signature,)).fetchone()[0]
    for role in ('photo_worker', 'photo_api', 'photo_gateway',
                 'photo_scheduler', 'photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                              (role, signature, 'EXECUTE')).fetchone()[0]
    assert not db.execute(
        'SELECT photo.v23_series_worker_device_matches(%s,%s,%s::jsonb)',
        (uuid4(), uuid4(), '{"device_profile_sha":"' + 'a' * 64 +
         '","runtime_lock_sha":"' + 'b' * 64 + '"}')).fetchone()[0]


def test_series_device_binding_requires_operator_column(db):
    assert db.execute(
        "SELECT EXISTS(SELECT 1 FROM information_schema.columns "
        "WHERE table_schema='photo' AND table_name='worker_registrations' "
        "AND column_name='v23_series_device_profile_sha')"
    ).fetchone()[0]
