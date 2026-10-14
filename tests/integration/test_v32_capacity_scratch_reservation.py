"""Authored SQL scratch budget boundaries; not execution evidence."""
from uuid import uuid4

import pytest

from test_v32_capacity_rotation_concurrency import _install


@pytest.mark.parametrize('base,expected', [
    (0, 0), (1, 2), (5, 6), (1024, 1229),
    (7505999378950825, 9007199254740990),
    (9007199254740991, None), (-1, None), (None, None),
])
def test_installed_policy_scratch_reservation_exact_ceiling(db, base, expected):
    pin = _install(db, 60)
    node, worker = 'scratch-' + uuid4().hex, uuid4()
    db.execute(
        'INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,'
        'scratch_budget_bytes,capacity_report_worker_id,capacity_report_policy_sha) '
        'VALUES(%s,1000,0,2000,%s,%s)', (node, worker, pin),
    )
    assert db.execute(
        'SELECT photo.capacity_policy_scratch_reservation(n,%s::bigint) '
        'FROM photo.resource_nodes n WHERE node_id=%s', (base, node),
    ).fetchone() == (expected,)
    db.execute('DELETE FROM photo.capacity_policy_timings WHERE policy_sha=%s', (pin,))
    assert db.execute(
        'SELECT photo.capacity_policy_scratch_reservation(n,%s::bigint) '
        'FROM photo.resource_nodes n WHERE node_id=%s', (base, node),
    ).fetchone() == (None,)


def test_legacy_scratch_reservation_retains_estimate(db):
    node = 'scratch-legacy-' + uuid4().hex
    db.execute(
        'INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,'
        'scratch_budget_bytes) VALUES(%s,1000,0,2000)', (node,),
    )
    assert db.execute(
        'SELECT photo.capacity_policy_scratch_reservation(n,1024) '
        'FROM photo.resource_nodes n WHERE node_id=%s', (node,),
    ).fetchone() == (1024,)


@pytest.mark.parametrize('role', ['photo_api', 'photo_worker', 'photo_scheduler'])
def test_runtime_roles_cannot_call_private_scratch_policy_helpers(db, role):
    for signature in (
        'photo.capacity_policy_scratch_valid(jsonb)',
        'photo.capacity_policy_scratch_reservation(photo.resource_nodes,bigint)',
    ):
        assert db.execute(
            "SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature),
        ).fetchone() == (False,)
