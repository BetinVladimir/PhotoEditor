"""Coordinator boundary tests, authored without executing the suite."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import json
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2 import multi_preview_adoption as module


class Cursor:
    def __init__(self, rows): self.rows = rows
    def fetchone(self): return self.rows[0] if self.rows else None
    def fetchall(self): return self.rows


class Connection:
    autocommit = True
    def __init__(self):
        self.info = SimpleNamespace(transaction_status=TransactionStatus.IDLE)
        self.role, self.accept, self.calls = 'photo_scheduler', True, []
        self.context = (*[uuid4() for _ in range(2)], 2, 3,
                        uuid4(), 'a'*64, uuid4(), 'b'*64)
        self.commits, self.rollbacks = 0, 0
    @contextmanager
    def transaction(self):
        assert self.info.transaction_status == TransactionStatus.IDLE
        self.info.transaction_status = TransactionStatus.INTRANS
        try:
            yield
        except BaseException:
            self.rollbacks += 1
            raise
        else:
            self.commits += 1
        finally:
            self.info.transaction_status = TransactionStatus.IDLE
    def execute(self, sql, args=None):
        assert self.info.transaction_status == TransactionStatus.INTRANS
        self.calls.append((sql, args))
        if sql == 'SELECT current_user': return Cursor([(self.role,)])
        if 'set_config' in sql or 'statement_timeout' in sql: return Cursor([])
        if 'preview_context' in sql: return Cursor([self.context])
        return Cursor([(self.accept,)])


def fixture(monkeypatch, error=None):
    read, write = Connection(), Connection()
    locations = [{'artifact_id': str(uuid4()), 'version': 'immutable'}]
    class Reader:
        def __init__(self, *args, **kwargs): pass
        def verified_locations(self): return locations
    def verify(*args, **kwargs):
        assert read.info.transaction_status == write.info.transaction_status == TransactionStatus.IDLE
        if error: raise AdoptionInvalid(error)
        return {'verified': True}
    monkeypatch.setattr(module, 'PostgresMultiPreviewReader', Reader)
    monkeypatch.setattr(module, 'verify_render_derivative_result', verify)
    coordinator = module.MultiPreviewAdoptionCoordinator(read, write, object(),
        'photos', target_icc_sha256='c'*64, working_icc_sha256='d'*64)
    return coordinator, read, write, locations


def test_verified_decode_commits_owned_locator_witness(monkeypatch):
    coordinator, read, write, locations = fixture(monkeypatch)
    tenant, run = uuid4(), uuid4()
    assert coordinator.adopt(tenant, run, 'preview1600') is True
    sql, args = write.calls[-1]
    assert 'record_v29_multi_preview_receipt' in sql
    assert args[:3] == (tenant, run, 'preview1600')
    assert args[3:6] == read.context[1:4]
    assert json.loads(args[6]) == {'verified': True}
    assert json.loads(args[7]) == locations
    assert read.commits == write.commits == 1


@pytest.mark.parametrize('error', ['RENDER_DERIVATIVE_ENCODING_INVALID',
                                  'RENDER_DERIVATIVE_CONTRACT_INVALID'])
def test_invalid_encoded_result_rejected_without_acceptance(monkeypatch, error):
    coordinator, _, write, _ = fixture(monkeypatch, error)
    assert coordinator.adopt(uuid4(), uuid4(), 'preview2560') is False
    assert 'reject_v29_multi_preview' in write.calls[-1][0]
    assert write.calls[-1][1][-1] == error
    assert not any('record_v29' in sql for sql, _ in write.calls)


def test_revoked_authority_is_retriable_without_permanent_rejection(monkeypatch):
    coordinator, _, write, _ = fixture(monkeypatch, 'MULTI_PREVIEW_RESULT_STALE')
    with pytest.raises(AdoptionInvalid, match='RESULT_STALE'):
        coordinator.adopt(uuid4(), uuid4(), 'preview1600')
    assert write.calls == []


def test_false_commit_is_not_accepted(monkeypatch):
    coordinator, _, write, _ = fixture(monkeypatch)
    write.accept = False
    with pytest.raises(AdoptionInvalid, match='COMMIT_STALE'):
        coordinator.adopt(uuid4(), uuid4(), 'preview1600')


def test_cancellation_after_write_rolls_back_receipt(monkeypatch):
    coordinator, _, write, _ = fixture(monkeypatch)
    def check():
        if any('record_v29' in sql for sql, _ in write.calls):
            raise RuntimeError('cancelled')
    coordinator.check_active = check
    with pytest.raises(RuntimeError, match='cancelled'):
        coordinator.adopt(uuid4(), uuid4(), 'preview1600')
    assert write.commits == 0 and write.rollbacks == 1


def test_wrong_write_role_refuses_receipt(monkeypatch):
    coordinator, _, write, _ = fixture(monkeypatch)
    write.role = 'photo_api'
    with pytest.raises(AdoptionInvalid, match='ROLE_INVALID'):
        coordinator.adopt(uuid4(), uuid4(), 'preview1600')
    assert not any('record_v29' in sql for sql, _ in write.calls)
