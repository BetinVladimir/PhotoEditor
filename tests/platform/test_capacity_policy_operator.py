"""Provisioning transaction sequencing without a live database or credentials."""
from contextlib import contextmanager
from pathlib import Path
from uuid import uuid4
import pytest
from photo_worker.runtime import capacity_policy_operator as operator

POLICY = Path(__file__).resolve().parents[2] / 'configs/capacity-metal-verification.json'


@pytest.mark.parametrize('rotate', [False, True])
@pytest.mark.parametrize('failure', [None, 'write', 'commit'])
def test_operator_acknowledges_only_committed_atomic_installation(monkeypatch, rotate, failure):
    events = []
    worker = uuid4()
    kwargs = dict(worker_id=worker, principal='worker_login', node='metal', expected_sha='a' * 64) if rotate else {}

    class Connection:
        autocommit = False
        def __enter__(self):
            events.append('open')
            return self
        def __exit__(self, *args):
            events.append('close')
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:
                yield
                if failure == 'commit':
                    raise RuntimeError('commit failed')
            except Exception:
                events.append('rollback')
                raise
            else:
                events.append('commit')

    def connect(dsn, **options):
        assert dsn == 'operator-fixture'
        assert options['autocommit'] is False
        assert options['connect_timeout'] == 5
        return Connection()

    def write(connection, policy, **scope):
        events.append('write')
        assert policy.gpu_budget_bytes == 10_000_000_000
        assert scope == kwargs
        if failure == 'write':
            raise RuntimeError('write failed')
        return 3 if rotate else policy.sha256

    monkeypatch.setattr(operator, 'rotate_worker_capacity_policy' if rotate else 'install_capacity_policy_timing', write)
    def run():
        return operator.provision_capacity_policy(POLICY, rotate=rotate, **kwargs,
            environ={'PHOTO_OPERATOR_DATABASE_URL': 'operator-fixture'}, connect=connect)
    if failure:
        with pytest.raises(RuntimeError):
            run()
        assert events == ['open', 'begin', 'write', 'rollback', 'close']
    else:
        receipt = run()
        assert receipt['rotated_registrations'] == (3 if rotate else 0)
        assert 'operator-fixture' not in str(receipt)
        assert events == ['open', 'begin', 'write', 'commit', 'close']


def test_never_falls_back_to_worker_database_credentials():
    with pytest.raises(ValueError, match='OPERATOR_DATABASE_REQUIRED'):
        operator.provision_capacity_policy(POLICY, environ={'DATABASE_URL': 'worker-fixture'},
            connect=lambda *a, **k: pytest.fail('unexpected connection'))


def test_rejects_invalid_rotation_scope_before_database_connection():
    with pytest.raises(ValueError, match='ROTATION_INVALID'):
        operator.provision_capacity_policy(POLICY, rotate=True, worker_id=uuid4(), principal='',
            node='metal', environ={'PHOTO_OPERATOR_DATABASE_URL': 'fixture'},
            connect=lambda *a, **k: pytest.fail('unexpected connection'))


@pytest.mark.parametrize('case,state', [('next','matches_next'),('old','matches_expected'),
    ('mixed','conflict'),('foreign_node','conflict'),('empty','missing_registration')])
@pytest.mark.parametrize('installed', [False, True])
def test_reconciliation_observes_one_read_only_snapshot(case, state, installed):
    from photo_worker.runtime.capacity_policy import load_capacity_policy
    from photo_worker.contracts.base import canonical_bytes
    policy = load_capacity_policy(POLICY)
    worker = uuid4()
    calls = []
    rows = {'next': [('metal',policy.sha256)] * 2, 'old': [('metal',None)],
            'mixed': [('metal',policy.sha256),('metal',None)],
            'foreign_node': [('other',policy.sha256)], 'empty': []}[case]
    class Connection:
        def __enter__(self):return self
        def __exit__(self,*args):pass
        @contextmanager
        def transaction(self):yield
        def execute(self, sql, args=None):
            calls.append((sql,args))
            assert sql.startswith(('SET TRANSACTION','SELECT '))
            if 'policy_jcs' in sql:
                from types import SimpleNamespace
                return SimpleNamespace(fetchone=lambda: (canonical_bytes(policy).decode(),policy.offline_seconds,policy.lease_seconds) if installed else None)
            if 'worker_registrations' in sql:
                assert args == (worker,'worker_login')
                from types import SimpleNamespace
                return SimpleNamespace(fetchall=lambda:rows)
    receipt = operator.reconcile_capacity_policy(POLICY,worker_id=worker,principal='worker_login',node='metal',expected_sha=None,
        environ={'PHOTO_OPERATOR_DATABASE_URL':'operator-fixture'},connect=lambda *a,**k:Connection())
    assert receipt == {'schema_version':1,'policy_sha':policy.sha256,'state':state,'policy_installed':installed,'registrations':len(rows)}
    assert calls[0][0] == 'SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY'
    assert len(calls) == 3
