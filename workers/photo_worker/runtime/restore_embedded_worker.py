"""Run restore probes beside the real main-thread worker and shared barrier."""
from threading import Event, Thread, current_thread, main_thread

from .restore_render_barrier import RestoreBarrierRenderer
from .restore_probe_dispatch import RestoreProbeDispatch, run_installed_restore_acceptance
from .restore_recovery import RestoreComputeRecoveryEvidence
from .service import run_built_worker


def run_embedded_restore_acceptance(config, database, transport, *, dispatch,
        renderer_barrier, recovery_path, recovery_sha256, replay_path, replay_sha256,
        evidence, build_sha256, fixture_pins, assert_probe_isolation,
        verify_current_authorities, check_active, metal_budget=None, stop_event=None, cancel_probe_io=None):
    """Return actual acceptance only after worker shutdown succeeds.

    The dispatch connection belongs to the probe thread. Worker TaskDatabase
    owns separate connections. No acceptance observation crosses processes.
    A failed worker cancels the probe; a failed probe stops and aborts the worker.
    """
    if (current_thread() is not main_thread()
            or not isinstance(dispatch, RestoreProbeDispatch)
            or not isinstance(renderer_barrier, RestoreBarrierRenderer)
            or dispatch.revoke.renderer_barrier is not renderer_barrier
            or not isinstance(evidence, RestoreComputeRecoveryEvidence)
            or any(not callable(fn) for fn in
                   (assert_probe_isolation, verify_current_authorities, check_active))):
        raise ValueError('RESTORE_EMBEDDED_INSTALLATION')
    if stop_event is not None and not isinstance(stop_event, Event):
        raise ValueError('RESTORE_EMBEDDED_STOP_INSTALLATION')
    if cancel_probe_io is not None and not callable(cancel_probe_io):
        raise ValueError('RESTORE_EMBEDDED_CANCEL_INSTALLATION')
    stop = stop_event if stop_event is not None else Event()
    finished = Event()
    values, errors = [], []
    def active():
        return not stop.is_set() and check_active() is not False
    def probe():
        try:
            values.append(run_installed_restore_acceptance(dispatch,
                recovery_path, recovery_sha256, replay_path, replay_sha256,
                evidence=evidence, build_sha256=build_sha256, fixture_pins=fixture_pins,
                assert_probe_isolation=assert_probe_isolation,
                verify_current_authorities=verify_current_authorities,
                check_active=active))
        except BaseException as exc:
            errors.append(exc)
        finally:
            finished.set()
            stop.set()
    thread = Thread(target=probe, name='restore-installed-probes', daemon=False)
    worker_error = None
    abort_error = None
    try:
        thread.start()
        run_built_worker(config, database, transport, metal_budget=metal_budget,
                         external_stop=stop)
    except BaseException as exc:
        worker_error = exc
    finally:
        stop.set()
        try:
            transport.executor.abort_active()
        except BaseException as exc:
            abort_error = exc
        if thread.ident is not None:
            thread.join(timeout=30)
    if thread.is_alive():
        # Retain phase ownership until the dedicated mutator has terminated.
        # Python threads cannot be killed safely; cleanup must never race it.
        try:
            if cancel_probe_io is not None:
                cancel_probe_io()
        finally:
            thread.join()
        raise RuntimeError('RESTORE_EMBEDDED_PROBE_DID_NOT_STOP')
    if worker_error is not None:
        raise worker_error
    if abort_error is not None:
        raise abort_error
    if errors:
        raise errors[0]
    if not finished.is_set() or len(values) != 1 or check_active() is False:
        raise RuntimeError('RESTORE_EMBEDDED_INCOMPLETE')
    return values[0]
