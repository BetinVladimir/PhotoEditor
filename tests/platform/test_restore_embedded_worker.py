"""Lifecycle unit scenarios; physical renderer qualification is deferred."""
from threading import Event
from types import SimpleNamespace
import pytest
from photo_worker.runtime import restore_embedded_worker as m


def installed(monkeypatch):
    barrier = m.RestoreBarrierRenderer.__new__(m.RestoreBarrierRenderer)
    dispatch = m.RestoreProbeDispatch.__new__(m.RestoreProbeDispatch)
    object.__setattr__(dispatch, 'revoke', SimpleNamespace(renderer_barrier=barrier))
    evidence = m.RestoreComputeRecoveryEvidence.__new__(m.RestoreComputeRecoveryEvidence)
    calls = []
    transport = SimpleNamespace(executor=SimpleNamespace(abort_active=lambda: calls.append('abort')))
    kwargs = dict(dispatch=dispatch, renderer_barrier=barrier,
        recovery_path='/private/recovery', recovery_sha256='1'*64,
        replay_path='/private/replay', replay_sha256='2'*64,
        evidence=evidence, build_sha256='3'*64, fixture_pins={},
        assert_probe_isolation=lambda: True, verify_current_authorities=lambda: True,
        check_active=lambda: True)
    return transport, kwargs, calls


def test_probe_success_stops_actual_worker_lifecycle(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    def worker(*args, external_stop, **unused):
        assert isinstance(external_stop, Event)
        assert external_stop.wait(2)
        calls.append('worker-stopped')
    monkeypatch.setattr(m, 'run_built_worker', worker)
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', lambda *a, **k: 'receipt')
    assert m.run_embedded_restore_acceptance(object(), object(), transport, **kwargs) == 'receipt'
    assert calls == ['worker-stopped', 'abort']


def test_probe_error_aborts_worker_and_is_not_acceptance(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    def failure(*a, **k):
        raise ValueError('fixture failed')
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', failure)
    monkeypatch.setattr(m, 'run_built_worker', lambda *a, external_stop, **k: external_stop.wait(2))
    with pytest.raises(ValueError, match='fixture failed'):
        m.run_embedded_restore_acceptance(object(), object(), transport, **kwargs)
    assert 'abort' in calls


def test_worker_error_cancels_probe_and_dominates_late_receipt(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    def failure(*a, **k):
        raise RuntimeError('capacity failed')
    def probe(*a, check_active, **k):
        while check_active():
            Event().wait(.001)
        return 'late receipt'
    monkeypatch.setattr(m, 'run_built_worker', failure)
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', probe)
    with pytest.raises(RuntimeError, match='capacity failed'):
        m.run_embedded_restore_acceptance(object(), object(), transport, **kwargs)
    assert 'abort' in calls


def test_mismatched_in_process_barrier_fails_before_worker(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    object.__setattr__(kwargs['dispatch'], 'revoke', SimpleNamespace(renderer_barrier=object()))
    with pytest.raises(ValueError, match='RESTORE_EMBEDDED_INSTALLATION'):
        m.run_embedded_restore_acceptance(object(), object(), transport, **kwargs)
    assert calls == []


def test_outer_guard_receives_worker_failure_stop(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    stop = Event()
    entered = Event()
    seen_stop = Event()
    def probe(*args, check_active, **unused):
        entered.set()
        stop.wait(2)
        assert stop.is_set()
        assert check_active() is False
        seen_stop.set()
        return 'late receipt'
    def worker(*args, external_stop, **unused):
        assert external_stop is stop
        assert entered.wait(2)
        raise RuntimeError('worker failed')
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', probe)
    monkeypatch.setattr(m, 'run_built_worker', worker)
    with pytest.raises(RuntimeError, match='worker failed'):
        m.run_embedded_restore_acceptance(object(), object(), transport,
            **kwargs, stop_event=stop)
    assert seen_stop.is_set()


def test_shared_stop_normal_success_uses_external_completion_authority(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    stop = Event()
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', lambda *a, **k: 'receipt')
    monkeypatch.setattr(m, 'run_built_worker', lambda *a, external_stop, **k: external_stop.wait(2))
    assert m.run_embedded_restore_acceptance(object(), object(), transport,
        **kwargs, stop_event=stop) == 'receipt'
    assert stop.is_set()


def test_invalid_shared_stop_refused_before_mutation(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    with pytest.raises(ValueError, match='RESTORE_EMBEDDED_STOP_INSTALLATION'):
        m.run_embedded_restore_acceptance(object(), object(), transport,
            **kwargs, stop_event=object())
    assert not calls


def test_abort_error_does_not_release_live_probe(monkeypatch):
    transport, kwargs, calls = installed(monkeypatch)
    stopped = Event()
    def probe(*args, check_active, **unused):
        while check_active():
            Event().wait(.001)
        stopped.set()
        return 'late receipt'
    def abort():
        raise RuntimeError('abort failed')
    transport.executor.abort_active = abort
    monkeypatch.setattr(m, 'run_installed_restore_acceptance', probe)
    monkeypatch.setattr(m, 'run_built_worker', lambda *a, **k: None)
    with pytest.raises(RuntimeError, match='abort failed'):
        m.run_embedded_restore_acceptance(object(), object(), transport, **kwargs)
    assert stopped.is_set()
