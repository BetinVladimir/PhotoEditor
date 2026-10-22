"""Readiness uses actual connection outcomes, never assumes Docker start is SQL ready."""
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime import backup_restore_operator as m


def clock(monkeypatch):
    import time
    value = [0.0]
    monkeypatch.setattr(time, 'monotonic', lambda: value[0])
    monkeypatch.setattr(time, 'sleep', lambda seconds: value.__setitem__(0, value[0] + seconds))
    return value


def test_startup_unavailable_then_actual_connection_returned(monkeypatch):
    clock(monkeypatch)
    result = object()
    calls = []
    def connect(**options):
        calls.append(options)
        if len(calls) == 1:
            raise psycopg.OperationalError('not ready')
        return result
    monkeypatch.setattr(psycopg, 'connect', connect)
    options = {'connect_timeout': 5, 'autocommit': True, 'dbname': 'isolated'}
    assert m.connect_restored_postgres(options, check_active=lambda: True) is result
    assert calls == [options, options]


def test_readiness_timeout_does_not_return_fake_connection(monkeypatch):
    clock(monkeypatch)
    monkeypatch.setattr(psycopg, 'connect', lambda **k: (_ for _ in ()).throw(psycopg.OperationalError('not ready')))
    with pytest.raises(TimeoutError, match='RESTORE_DATABASE_READINESS_TIMEOUT'):
        m.connect_restored_postgres({}, check_active=lambda: True, maximum_seconds=1)


def test_explicit_authentication_failure_not_retried(monkeypatch):
    clock(monkeypatch)
    calls = []
    def connect(**options):
        calls.append(options)
        raise psycopg.errors.InvalidPassword('wrong password')
    monkeypatch.setattr(psycopg, 'connect', connect)
    with pytest.raises(psycopg.errors.InvalidPassword):
        m.connect_restored_postgres({}, check_active=lambda: True)
    assert len(calls) == 1


def test_cancel_after_connect_closes_actual_connection(monkeypatch):
    clock(monkeypatch)
    active = [True]
    closed = []
    handle = SimpleNamespace(close=lambda: closed.append(True))
    def connect(**options):
        active[0] = False
        return handle
    monkeypatch.setattr(psycopg, 'connect', connect)
    with pytest.raises(RuntimeError, match='RESTORE_OPERATOR_INACTIVE'):
        m.connect_restored_postgres({}, check_active=lambda: active[0])
    assert closed == [True]
