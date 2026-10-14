"""No receipt/rejection inside an outer transaction opened during verification."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2 import render_derivative_service as service


@pytest.mark.parametrize('invalid_encoding', [False, True])
def test_verification_cannot_enter_nested_commit(monkeypatch, invalid_encoding):
    task, result, params, manifest = [uuid4() for _ in range(4)]
    rows = [(task, result, 1, 1, params, 'a'*64, manifest, 'b'*64)]
    read = SimpleNamespace(autocommit=True,
        info=SimpleNamespace(transaction_status=TransactionStatus.IDLE),
        execute=lambda *args: SimpleNamespace(fetchall=lambda: rows))
    writes = []
    @contextmanager
    def transaction():
        writes.append('transaction')
        yield
    write = SimpleNamespace(autocommit=True,
        info=SimpleNamespace(transaction_status=TransactionStatus.IDLE),
        transaction=transaction, execute=lambda *args: writes.append(args))
    coordinator = service.RenderDerivativeAdoptionCoordinator(read, write, object(),
        'test', target_icc_sha256='a'*64, working_icc_sha256='b'*64)
    def verify(*args, **kwargs):
        write.info.transaction_status = TransactionStatus.INTRANS
        if invalid_encoding:
            raise AdoptionInvalid('RENDER_DERIVATIVE_ENCODING_INVALID')
        return object()
    monkeypatch.setattr(service, 'verify_render_derivative_result', verify)
    monkeypatch.setattr(service, 'canonical_bytes', lambda _: b'{}')
    with pytest.raises(AdoptionInvalid, match='COORDINATOR_TRANSACTION_ACTIVE'):
        coordinator.adopt(uuid4(), uuid4(), 'preview1600')
    assert writes == []
