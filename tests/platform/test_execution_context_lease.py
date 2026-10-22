"""Local lease authority: delayed replies never resurrect an expired attempt."""
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
import pytest
from photo_worker.runtime import executor
from photo_worker.runtime.database import Heartbeat


def context(monkeypatch):
    clock = [0.0]
    monkeypatch.setattr(executor, 'monotonic', lambda: clock[0])
    now = datetime.now(timezone.utc)
    lease = SimpleNamespace(until=now+timedelta(seconds=60),
                            deadline=now+timedelta(seconds=600))
    return executor.ExecutionContext(lease), clock, now


def test_observed_expiry_is_irreversible(monkeypatch):
    ctx, clock, now = context(monkeypatch)
    clock[0] = 61
    with pytest.raises(executor.LeaseLost):
        ctx.check_active()
    assert ctx.accept_heartbeat(Heartbeat(True, now+timedelta(seconds=120), False)) is False
    with pytest.raises(executor.LeaseLost):
        ctx.check_active()


def test_delayed_heartbeat_cannot_bridge_unobserved_local_expiry(monkeypatch):
    ctx, clock, now = context(monkeypatch)
    clock[0] = 61
    assert ctx.accept_heartbeat(Heartbeat(True, now+timedelta(seconds=120), False)) is False
    with pytest.raises(executor.LeaseLost):
        ctx.check_active()


def test_live_renewal_remains_active(monkeypatch):
    ctx, clock, now = context(monkeypatch)
    clock[0] = 10
    assert ctx.accept_heartbeat(Heartbeat(True, now+timedelta(seconds=120), False)) is True
    clock[0] = 70
    ctx.check_active()


def test_cancellation_prevents_work_even_after_other_live_reply(monkeypatch):
    ctx, _, now = context(monkeypatch)
    assert ctx.accept_heartbeat(Heartbeat(True, now+timedelta(seconds=120), True)) is False
    with pytest.raises(executor.TaskCancelled):
        ctx.check_active()
    ctx.accept_heartbeat(Heartbeat(True, now+timedelta(seconds=120), False))
    with pytest.raises(executor.TaskCancelled):
        ctx.check_active()


@pytest.mark.parametrize('beat', [Heartbeat(False, None, False),
    Heartbeat(True, None, False), Heartbeat(True, datetime(2000,1,1,tzinfo=timezone.utc), False)])
def test_rejected_or_expired_renewal_loses_authority(monkeypatch, beat):
    ctx, _, _ = context(monkeypatch)
    assert ctx.accept_heartbeat(beat) is False
    with pytest.raises(executor.LeaseLost):
        ctx.check_active()
