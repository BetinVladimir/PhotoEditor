"""Direct reconciliation must compose both live cancellation authorities."""
from contextlib import nullcontext
from types import SimpleNamespace
import pytest
from photo_worker.runtime import backup_restore_operator as m


class NoIO:
    def __getattr__(self, name):
        raise AssertionError('IO before cancellation refusal: ' + name)


def call(target, active, connection=None):
    # Exercise the implementation beneath the separately covered host lease.
    return m.reconcile_checkpoint.__wrapped__(
        {'database': 'isolated', 'system_identifier': '123'},
        {'database': 'isolated', 'system_identifier': '123'}, target,
        connection if connection is not None else NoIO(), NoIO(), NoIO(),
        check_active=active)


def test_supplied_cancellation_refuses_before_compose_or_sql():
    target = SimpleNamespace(commands=NoIO())
    with pytest.raises(RuntimeError, match='RESTORE_OPERATOR_INACTIVE'):
        call(target, lambda: False)


def test_supplied_cancellation_rechecked_after_host_guard():
    state = {'active': True}
    def host():
        state['active'] = False
    target = SimpleNamespace(commands=SimpleNamespace(active=host))
    with pytest.raises(RuntimeError, match='RESTORE_OPERATOR_INACTIVE'):
        call(target, lambda: state['active'])


def test_host_cancellation_cannot_be_overridden_by_supplied_true():
    def host():
        raise RuntimeError('host stopped')
    target = SimpleNamespace(commands=SimpleNamespace(active=host))
    with pytest.raises(RuntimeError, match='host stopped'):
        call(target, lambda: True)


def test_initial_database_fence_installs_local_timeouts_before_alter(monkeypatch):
    statements = []
    class StopAtFence(Exception):
        pass
    class Connection:
        def transaction(self):
            return nullcontext()
        def execute(self, query):
            statements.append(query)
            if len(statements) == 3:
                raise StopAtFence()
    monkeypatch.setattr(m, 'assert_isolated_compose', lambda target: None)
    target = SimpleNamespace(commands=SimpleNamespace(active=lambda: None),
                             running=lambda: {'postgres', 'minio'})
    with pytest.raises(StopAtFence):
        call(target, lambda: True, Connection())
    assert statements[:2] == ["SET LOCAL statement_timeout='5s'", "SET LOCAL lock_timeout='1s'"]
    assert 'ALTER DATABASE' in str(statements[2])
