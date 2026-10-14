"""Bounded work and receipt-to-completion recovery; authored, not executed."""
from uuid import UUID, uuid4
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.multi_preview_maintenance import MultiPreviewReceiptMaintenance


class Coordinator:
    read, write = object(), object()
    def __init__(self, rows, completion=()):
        self.rows, self.completion = rows, completion
        self.calls, self.adoptions = [], []
        self.finalized = set()
    def _query(self, connection, tenant, sql, args, one=False):
        self.calls.append((connection, tenant, sql, args))
        if 'pending_v29_multi_preview_receipts' in sql: return self.rows
        if 'pending_v29_multi_preview_completion' in sql: return self.completion
        assert connection is self.write and one
        run = args[1]
        if run in self.finalized: return (False,)
        self.finalized.add(run)
        return (True,)
    def adopt(self, tenant, run, variant):
        self.adoptions.append((tenant, run, variant))
        return True


def test_two_variants_only_finalize_after_whole_page():
    tenant, run = uuid4(), uuid4()
    c = Coordinator([(run, 'preview1600', UUID(int=1)), (run, 'preview2560', UUID(int=2))])
    assert MultiPreviewReceiptMaintenance(c).process_page(tenant) == (None, 2, 0, 1)
    assert len(c.adoptions) == 2
    assert len(c.finalized) == 1
    assert all(call[1] == tenant for call in c.calls)


def test_full_page_cursor_preserves_keyset_boundary():
    c = Coordinator([(uuid4(), 'preview1600', UUID(int=n)) for n in range(1, 33)])
    assert MultiPreviewReceiptMaintenance(c).process_page(uuid4())[0] == UUID(int=32)


@pytest.mark.parametrize('rows', [
    [(uuid4(), 'preview1600', UUID(int=2)), (uuid4(), 'preview2560', UUID(int=1))],
    [(uuid4(), 'foreign', UUID(int=1))],
    [(uuid4(), 'preview1600', UUID(int=1))]*33,
])
def test_invalid_batch_refused_before_any_mutation(rows):
    c = Coordinator(rows)
    with pytest.raises(AdoptionInvalid, match='PENDING_INVALID'):
        MultiPreviewReceiptMaintenance(c).process_page(uuid4())
    assert c.adoptions == [] and not c.finalized


def test_completion_recovers_after_last_receipt_commit_without_decode():
    run = uuid4()
    c = Coordinator([], [(run,)])
    maintenance = MultiPreviewReceiptMaintenance(c)
    assert maintenance.process_completion_page(uuid4()) == (None, 1)
    assert maintenance.process_completion_page(uuid4()) == (None, 0)
    assert c.adoptions == []


def test_stale_completion_batch_refused_before_finalization():
    c = Coordinator([], [(UUID(int=2),), (UUID(int=1),)])
    with pytest.raises(AdoptionInvalid, match='PENDING_INVALID'):
        MultiPreviewReceiptMaintenance(c).process_completion_page(uuid4())
    assert not c.finalized


def test_invalid_scope_does_not_query():
    c = Coordinator([])
    for method in (MultiPreviewReceiptMaintenance(c).process_page,
                   MultiPreviewReceiptMaintenance(c).process_completion_page):
        with pytest.raises(AdoptionInvalid, match='TENANT_INVALID'): method(UUID(int=0))
        with pytest.raises(AdoptionInvalid, match='CURSOR_INVALID'): method(uuid4(), 'cursor')
    assert c.calls == []


class LifecycleCoordinator(Coordinator):
    def __init__(self, rows, outcomes):
        super().__init__([])
        self.lifecycle_rows, self.outcomes = rows, outcomes
    def _query(self, connection, tenant, sql, args, one=False):
        self.calls.append((connection, tenant, sql, args))
        if 'pending_v29_multi_preview_lifecycle' in sql:
            assert connection is self.read
            return self.lifecycle_rows
        assert 'maintain_v29_multi_preview' in sql
        assert connection is self.write and one
        return self.outcomes[args[1]]


def test_lifecycle_distinguishes_cancel_request_failure_and_release():
    tenant = uuid4()
    runs = [UUID(int=n) for n in range(1, 5)]
    c = LifecycleCoordinator([(run,) for run in runs], dict(zip(runs,
        [('cancel_requested',), ('failed',), ('released',), ('none',)])))
    assert MultiPreviewReceiptMaintenance(c).process_lifecycle_page(tenant) == (
        None, {'cancel_requested': 1, 'failed': 1, 'released': 1})
    assert all(call[1] == tenant and call[3][0] == tenant for call in c.calls)
    assert c.adoptions == []


def test_busy_uploads_report_no_release_and_are_revisited_next_sweep():
    run = uuid4()
    c = LifecycleCoordinator([(run,)], {run: ('none',)})
    maintenance = MultiPreviewReceiptMaintenance(c)
    expected = (None, {'cancel_requested': 0, 'failed': 0, 'released': 0})
    assert maintenance.process_lifecycle_page(uuid4()) == expected
    c.outcomes[run] = ('released',)
    assert maintenance.process_lifecycle_page(uuid4())[1]['released'] == 1


def test_invalid_lifecycle_batch_does_not_mutate():
    c = LifecycleCoordinator([(UUID(int=2),), (UUID(int=1),)], {})
    with pytest.raises(AdoptionInvalid, match='PENDING_INVALID'):
        MultiPreviewReceiptMaintenance(c).process_lifecycle_page(uuid4())
    assert len(c.calls) == 1


@pytest.mark.parametrize('outcome', [(True,), ('completed',), None, ('none', 'extra')])
def test_unknown_lifecycle_outcome_is_not_progress(outcome):
    run = uuid4()
    c = LifecycleCoordinator([(run,)], {run: outcome})
    with pytest.raises(AdoptionInvalid, match='LIFECYCLE_INVALID'):
        MultiPreviewReceiptMaintenance(c).process_lifecycle_page(uuid4())


def test_retryable_decode_does_not_starve_later_rows():
    tenant = uuid4()
    runs = [UUID(int=10), UUID(int=11)]
    c = Coordinator([(runs[0], 'preview1600', UUID(int=1)),
                     (runs[1], 'preview1600', UUID(int=2))])
    def adopt(tenant, run, variant):
        if run == runs[0]: raise RuntimeError('object store unavailable')
        c.adoptions.append((tenant, run, variant))
        return True
    c.adopt = adopt
    retries = []
    assert MultiPreviewReceiptMaintenance(c).process_page(tenant, on_retry=retries.append) == (None, 1, 0, 1)
    assert len(retries) == 1 and c.adoptions[0][1] == runs[1]


def test_stop_is_never_swallowed_by_retry_callback():
    run = uuid4()
    c = Coordinator([(run, 'preview1600', UUID(int=1))])
    def stop(*args): raise InterruptedError('stopped')
    c.adopt = stop
    retries = []
    with pytest.raises(InterruptedError):
        MultiPreviewReceiptMaintenance(c).process_page(uuid4(), on_retry=retries.append)
    assert retries == [] and not c.finalized
