"""W-2: transient report errors are tolerated; explicit rejection fails closed."""
from threading import Event

import photo_worker.runtime.capacity as cap


def _reporter(report, aborted):
    r = cap.CapacityReporter(report, lambda: None, lambda: aborted.append(1), interval_seconds=0.01)
    return r


def test_transient_errors_do_not_abort_within_grace():
    calls = {'n': 0}
    stop, aborted = Event(), []

    def report(_):
        calls['n'] += 1
        if calls['n'] < 4:
            raise OSError('vpn blip')
        if calls['n'] >= 6:
            stop.set()
        return True

    r = _reporter(report, aborted)
    r.run(stop)
    assert not aborted and not r.failed.is_set()


def test_rejection_fails_closed_immediately():
    stop, aborted = Event(), []
    r = _reporter(lambda _: False, aborted)
    r.run(stop)
    assert aborted and r.failed.is_set() and stop.is_set()


def test_outage_beyond_grace_aborts():
    stop, aborted = Event(), []

    def report(_):
        raise OSError('down')

    r = _reporter(report, aborted)
    r.grace = 0.05
    r.run(stop)
    assert aborted and r.failed.is_set()
