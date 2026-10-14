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
