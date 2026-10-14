"""Runtime sweep cursors, fair retry and stop behavior; authored, not run."""
from uuid import UUID
from threading import Event
from types import SimpleNamespace
import pytest
from photo_worker.v2.multi_quality_runner import MultiQualityRunner
from photo_worker.v2.service import Adopter


class Coordinator:
    read = object()
    def __init__(self): self.tenants = [UUID(int=1), UUID(int=2)]; self.calls = []
    def check_active(self): pass
    def _query(self, connection, tenant, sql, args):
        assert connection is self.read and tenant == ''
        self.calls.append(args[0])
        rows = [item for item in self.tenants if args[0] is None or item.int > args[0].int]
        return [(rows[0],)] if rows else []


class Maintenance:
    def __init__(self): self.calls = []; self.receipt_pages = 0; self.fail = False
    def process_lifecycle_page(self, tenant, cursor, on_retry):
        self.calls.append(('lifecycle', tenant, cursor))
        return None, {'cancel_requested': 1, 'released': 0, 'failed': 0}
    def process_page(self, tenant, cursor, on_retry):
        self.calls.append(('receipts', tenant, cursor))
        if self.fail: raise RuntimeError('temporary tenant failure')
        self.receipt_pages += 1
        return (UUID(int=100) if self.receipt_pages == 1 else None, 1, 0, 0)
    def process_completion_page(self, tenant, cursor, on_retry):
        self.calls.append(('completion', tenant, cursor))
        return None, 1


def fixture():
    c = Coordinator(); runner = MultiQualityRunner(c)
    runner.maintenance = Maintenance()
    return c, runner


def test_independent_phase_cursors_keep_receipts_page_while_other_phases_advance():
    _, runner = fixture()
    runner.run_once(); runner.run_once()
    calls = runner.maintenance.calls
    assert ('receipts', UUID(int=1), UUID(int=100)) in calls
    assert ('lifecycle', UUID(int=2), None) in calls
    assert ('completion', UUID(int=2), None) in calls
    assert len(calls) == 6


def test_tenant_error_does_not_block_other_tenants_or_phases():
    _, runner = fixture(); runner.maintenance.fail = True
    assert runner.run_once()['retry'] == 1
    assert runner.run_once()['retry'] == 1
    assert ('receipts', UUID(int=2), None) in runner.maintenance.calls
    assert any(call[0] == 'completion' for call in runner.maintenance.calls)


def test_empty_sweep_resets_discovery_cursor():
    c, runner = fixture(); c.tenants = []
    assert runner.run_once()['adopted'] == 0
    assert all(position == [None, None, None] for position in runner.positions.values())


def test_stop_propagates_without_work():
    c, runner = fixture()
    def stopped(): raise InterruptedError('stopped')
    c.check_active = stopped
    with pytest.raises(InterruptedError): runner.run_once()
    assert c.calls == [] and runner.maintenance.calls == []


def test_adopter_runtime_merges_preview_counts_without_other_candidates():
    class Cursor:
        def fetchall(self): return []
    class Read:
        def execute(self, *args): return Cursor()
    adopter = Adopter.__new__(Adopter)
    adopter.multi_quality_runner = None
    adopter.multi_preview_runner = None
    adopter.perspective_preview_runner = None
    adopter.perspective_quality_runner = None
    adopter.stop = Event(); adopter.read = Read(); adopter.exposure_enabled = False
    adopter.derivative_coordinator = None
    adopter.multi_quality_runner = SimpleNamespace(run_once=lambda: {
        'adopted': 2, 'rejected': 1, 'finalized': 3, 'retry': 4,
        'cancel_requested': 1, 'released': 1})
    assert adopter.run_once() == {'adopted': 2, 'rejected': 1, 'finalized': 3, 'retry': 4}
    adopter.stop.set()
    assert adopter.run_once() == {'adopted': 0, 'rejected': 0, 'finalized': 0, 'retry': 0}
