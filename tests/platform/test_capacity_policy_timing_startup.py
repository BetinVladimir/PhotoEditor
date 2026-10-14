"""Timing startup probe precedes registration/reporting/queue consumption."""
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID, uuid4
import pytest
from photo_worker.runtime import service
from photo_worker.runtime.capacity_policy import load_capacity_policy
from photo_worker.runtime.database import TaskDatabase

POLICY = Path(__file__).resolve().parents[2] / 'configs/capacity-metal-verification.json'


@pytest.mark.parametrize('reply', [None, (False,), (None,), (1,), ('true',), (True,)])
def test_database_probe_requires_exact_boolean_and_binds_policy(monkeypatch, reply):
    worker = uuid4()
    calls = []
    db = TaskDatabase('fixture')
    monkeypatch.setattr(db, '_query', lambda *args: calls.append(args) or reply)
    assert db.capacity_policy_timing_ready(worker, 'a' * 64, 120, 300) is (reply == (True,) and type(reply[0]) is bool)
    assert calls == [('SELECT photo.capacity_policy_timing_ready(%s,%s,%s,%s)', (worker, 'a' * 64, 120, 300))]


@pytest.mark.parametrize('worker,pin,offline,lease', [
    (UUID(int=0), 'a' * 64, 30, 60), (uuid4(), 'invalid', 30, 60),
    (uuid4(), 'a' * 64, True, 60), (uuid4(), 'a' * 64, 30, 0),
    (uuid4(), 'a' * 64, 2**53, 60), (uuid4(), 'a' * 64, 30, 1.5),
])
def test_invalid_probe_never_reaches_sql(monkeypatch, worker, pin, offline, lease):
    db = TaskDatabase('fixture')
    monkeypatch.setattr(db, '_query', lambda *args: pytest.fail('unexpected SQL'))
    with pytest.raises(ValueError, match='TIMING_INVALID'):
        db.capacity_policy_timing_ready(worker, pin, offline, lease)


@pytest.mark.parametrize('reply', [False, None, 1, 'true', True])
def test_shared_worker_fences_policy_before_registration_or_reporting(monkeypatch, reply):
    policy = load_capacity_policy(POLICY)
    worker = uuid4()
    events = []
    monkeypatch.setattr(service, '_install_metal_budget', lambda config: None)
    monkeypatch.setattr(service, 'CapacityReporter', lambda *a, **k: pytest.fail('reporter started'))
    executor = SimpleNamespace(operations={})
    transport = SimpleNamespace(executor=executor, run=lambda *a: pytest.fail('consumer started'))

    def ready(*args):
        assert args == (worker, policy.sha256, policy.offline_seconds, policy.lease_seconds)
        events.append('policy')
        return reply

    class RegistrationReached(Exception):
        pass

    def operations(*args):
        events.append('registration')
        raise RegistrationReached()

    database = SimpleNamespace(ping=lambda: events.append('ping'), capacity_policy_timing_ready=ready,
                               runtime_operations=operations)
    config = SimpleNamespace(capacity_policy=policy, worker_id=worker, resource_class='cpu', backend='cpu')
    with pytest.raises(RegistrationReached if reply is True else service.WorkerConfigurationError):
        service._run_built_worker(config, database, transport)
    assert events == (['ping', 'policy', 'registration'] if reply is True else ['ping', 'policy'])
    assert executor.heartbeat_seconds == policy.renew_seconds
