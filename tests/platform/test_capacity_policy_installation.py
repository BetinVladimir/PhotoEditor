"""Operator adapter tests; real SQL constraints/ACL qualification is separate."""
from pathlib import Path
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.capacity_policy_installation import install_capacity_policy_timing

POLICY = Path(__file__).resolve().parents[2] / 'configs/capacity-metal-verification.json'


def test_installs_exact_canonical_policy_inside_caller_transaction():
    policy = load_capacity_policy(POLICY)
    calls = []

    def execute(sql, args):
        calls.append((sql, args))
        return SimpleNamespace(fetchone=lambda: (canonical_bytes(policy).decode(), policy.offline_seconds, policy.lease_seconds))

    connection = SimpleNamespace(autocommit=False, execute=execute)
    assert install_capacity_policy_timing(connection, policy) == policy.sha256
    assert len(calls) == 2
    assert calls[0][1] == (policy.sha256, canonical_bytes(policy).decode(), policy.offline_seconds, policy.lease_seconds)
    assert 'ON CONFLICT(policy_sha) DO NOTHING' in calls[0][0]
    assert calls[1][1] == (policy.sha256,)


@pytest.mark.parametrize('row', [None, ('different', 30, 60), ('{}', 120, 300)])
def test_existing_policy_disagreement_cannot_be_acknowledged(row):
    policy = load_capacity_policy(POLICY)
    connection = SimpleNamespace(autocommit=False, execute=lambda *a: SimpleNamespace(fetchone=lambda: row))
    with pytest.raises(ValueError, match='CONFLICT'):
        install_capacity_policy_timing(connection, policy)


@pytest.mark.parametrize('autocommit', [None, True, 0, 'false'])
def test_refuses_implicit_or_autocommit_operator_transaction(autocommit):
    connection = SimpleNamespace(autocommit=autocommit, execute=lambda *a: pytest.fail('unexpected SQL'))
    with pytest.raises(ValueError, match='INSTALLATION_INVALID'):
        install_capacity_policy_timing(connection, load_capacity_policy(POLICY))


def test_revalidates_bypassed_policy_before_any_sql():
    policy = load_capacity_policy(POLICY).model_copy(update={'lease_seconds': 0})
    connection = SimpleNamespace(autocommit=False, execute=lambda *a: pytest.fail('unexpected SQL'))
    with pytest.raises(ValueError):
        install_capacity_policy_timing(connection, policy)


@pytest.mark.parametrize('count', [1, 3, None, False, True, 0, '1'])
def test_rotation_installs_then_cas_binds_exact_operator_scope(count):
    from uuid import uuid4
    from photo_worker.runtime.capacity_policy_installation import rotate_worker_capacity_policy
    policy = load_capacity_policy(POLICY)
    worker = uuid4()
    calls = []

    def execute(sql, args):
        calls.append((sql, args))
        row = (count,) if 'rotate_worker_capacity_policy' in sql else (canonical_bytes(policy).decode(), policy.offline_seconds, policy.lease_seconds)
        return SimpleNamespace(fetchone=lambda: row)

    connection = SimpleNamespace(autocommit=False, execute=execute)
    kwargs = dict(worker_id=worker, principal='worker_login', node='metal-node', expected_sha='b' * 64)
    if type(count) is int and count > 0:
        assert rotate_worker_capacity_policy(connection, policy, **kwargs) == count
    else:
        with pytest.raises(ValueError, match='CONFLICT'):
            rotate_worker_capacity_policy(connection, policy, **kwargs)
    assert calls[-1] == ('SELECT photo.rotate_worker_capacity_policy(%s,%s,%s,%s,%s)',
                         (worker, 'worker_login', 'metal-node', 'b' * 64, policy.sha256))
    assert len(calls) == 3


@pytest.mark.parametrize('patch', [{'principal': ''}, {'principal': 'x' * 64}, {'node': ''},
                                  {'expected_sha': 'invalid'}, {'worker_id': None}])
def test_invalid_rotation_scope_never_installs_policy(patch):
    from uuid import uuid4
    from photo_worker.runtime.capacity_policy_installation import rotate_worker_capacity_policy
    kwargs = dict(worker_id=uuid4(), principal='worker_login', node='metal-node', expected_sha=None)
    kwargs.update(patch)
    connection = SimpleNamespace(autocommit=False, execute=lambda *a: pytest.fail('unexpected SQL'))
    with pytest.raises(ValueError, match='ROTATION_INVALID'):
        rotate_worker_capacity_policy(connection, load_capacity_policy(POLICY), **kwargs)
