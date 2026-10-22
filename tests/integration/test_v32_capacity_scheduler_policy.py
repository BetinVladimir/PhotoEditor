"""Authored installed scheduler policy boundary; not execution evidence."""
from uuid import uuid4

import pytest

from test_v32_capacity_rotation_concurrency import _install


def test_scheduler_reads_exact_installed_policy_but_cannot_install(db):
    pin = _install(db, 60)
    expected = db.execute('SELECT policy_jcs FROM photo.capacity_policy_timings WHERE policy_sha=%s', (pin,)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.capacity_scheduler_policy(%s)', (pin,)).fetchone() == (expected,)
    assert db.execute('SELECT photo.capacity_scheduler_policy(%s)', (uuid4().hex * 2,)).fetchone() == (None,)
    assert db.execute("SELECT has_table_privilege(current_user,'photo.capacity_policy_timings','INSERT')").fetchone() == (False,)


@pytest.mark.parametrize('role,allowed', [('photo_api', False), ('photo_worker', False), ('photo_scheduler', True)])
def test_scheduler_policy_read_acl(db, role, allowed):
    assert db.execute("SELECT has_function_privilege(%s,'photo.capacity_scheduler_policy(text)','EXECUTE')", (role,)).fetchone() == (allowed,)


def test_policy_row_lock_does_not_grant_runtime_table_access_or_mutation(db):
    """Exercise real PostgreSQL column privilege and immutable trigger together."""
    import psycopg
    pin=_install(db,60)
    assert db.execute("SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='photo.capacity_policy_timings'::regclass").fetchone()==('photo_runtime_owner',)
    for role in ('photo_worker','photo_scheduler','photo_api'):
        for privilege in ('SELECT','INSERT','UPDATE','DELETE'):
            assert db.execute("SELECT has_table_privilege(%s,'photo.capacity_policy_timings',%s)",(role,privilege)).fetchone()==(False,)
        assert db.execute("SELECT has_column_privilege(%s,'photo.capacity_policy_timings','policy_sha','UPDATE')",(role,)).fetchone()==(False,)
    assert db.execute("SELECT has_table_privilege('photo_fn_owner','photo.capacity_policy_timings','DELETE')").fetchone()==(False,)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT policy_sha FROM photo.capacity_policy_timings WHERE policy_sha=%s FOR SHARE',(pin,)).fetchone()==(pin,)
        with pytest.raises(psycopg.errors.CheckViolation,match='CAPACITY_POLICY_TIMING_IMMUTABLE'):
            with db.transaction():
                db.execute('UPDATE photo.capacity_policy_timings SET policy_sha=policy_sha WHERE policy_sha=%s',(pin,))
        # The update trigger, not a broad row-write grant, protects the bytes.
        assert db.execute("SELECT has_column_privilege(current_user,'photo.capacity_policy_timings','policy_jcs','UPDATE')").fetchone()==(False,)
        db.execute('RESET ROLE')
