"""A refused SQL retry fence must not be reported as a deferred encode."""
from contextlib import nullcontext
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.delivery_encoded_service import DeliveryEncodeAdoptionService


class Connection:
    autocommit = True

    def __init__(self, rows, deferred=None, terminal=(False,)):
        self.rows = rows
        self.deferred = deferred
        self.terminal = terminal
        self.calls = []

    def execute(self, statement, params):
        self.calls.append((statement, params))
        if 'v30_delivery_pending_encodes' in statement:
            return SimpleNamespace(fetchall=lambda: self.rows)
        if 'defer_v30_delivery_encode' in statement:
            return SimpleNamespace(fetchone=lambda: self.deferred)
        if 'reject_v30_delivery_encode_after_retries' in statement:
            return SimpleNamespace(fetchone=lambda: self.terminal)
        raise AssertionError(statement)

    def transaction(self):
        return nullcontext()


@pytest.mark.parametrize('receipt', [None, (False,), (True, False)])
def test_refused_backoff_is_not_reported_as_deferred(tmp_path, receipt):
    tenant_id, task_id = uuid4(), uuid4()
    reader = Connection([(tenant_id, task_id)])
    writer = Connection([], receipt)
    service = DeliveryEncodeAdoptionService(reader, writer, object(),
        allowed_bucket='delivery', scratch_root=tmp_path, max_bytes=1024,
        max_pixels=100)
    service.adopt = lambda *_: (_ for _ in ()).throw(AdoptionInvalid('INVALID_RESULT'))

    with pytest.raises(AdoptionInvalid, match='DELIVERY_ENCODE_BACKOFF_REFUSED'):
        service.process_pending()
    assert len(writer.calls) == 1
    assert writer.calls[0][1] == (tenant_id, task_id)


def test_confirmed_backoff_is_reported_as_retry(tmp_path):
    tenant_id, task_id = uuid4(), uuid4()
    reader = Connection([(tenant_id, task_id)])
    writer = Connection([], (True,))
    service = DeliveryEncodeAdoptionService(reader, writer, object(),
        allowed_bucket='delivery', scratch_root=tmp_path, max_bytes=1024,
        max_pixels=100)
    service.adopt = lambda *_: (_ for _ in ()).throw(AdoptionInvalid('INVALID_RESULT'))

    page = service.process_pending()
    assert (page.candidates, page.adopted) == (1, 0)
    assert page.rejected == ((tenant_id, task_id, 'INVALID_RESULT'),)
    assert page.terminal == ()
    assert len(writer.calls) == 2


def test_seventh_same_result_refusal_is_terminal(tmp_path):
    tenant_id, task_id = uuid4(), uuid4()
    reader = Connection([(tenant_id, task_id)])
    writer = Connection([], (True,), (True,))
    service = DeliveryEncodeAdoptionService(reader, writer, object(),
        allowed_bucket='delivery', scratch_root=tmp_path, max_bytes=1024,
        max_pixels=100)
    service.adopt = lambda *_: (_ for _ in ()).throw(AdoptionInvalid('INVALID_RESULT'))

    page = service.process_pending()
    assert page.rejected == ()
    assert page.terminal == ((tenant_id, task_id, 'INVALID_RESULT'),)
    assert writer.calls[1][1] == (tenant_id, task_id, 'INVALID_RESULT')


def test_stale_context_never_writes_retry_or_terminal_receipt(tmp_path):
    tenant_id, task_id = uuid4(), uuid4()
    reader = Connection([(tenant_id, task_id)])
    writer = Connection([], (True,), (True,))
    service = DeliveryEncodeAdoptionService(reader, writer, object(),
        allowed_bucket='delivery', scratch_root=tmp_path, max_bytes=1024,
        max_pixels=100)
    service.adopt = lambda *_: (_ for _ in ()).throw(AdoptionInvalid('DELIVERY_ENCODE_CONTEXT_CHANGED'))

    with pytest.raises(AdoptionInvalid, match='DELIVERY_ENCODE_CONTEXT_CHANGED'):
        service.process_pending()
    assert writer.calls == []
