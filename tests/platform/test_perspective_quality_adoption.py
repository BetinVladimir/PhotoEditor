"""Coordinator boundary tests, authored without executing the suite."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import json
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2 import perspective_quality_adoption as module


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
                        uuid4(), 'a'*64, uuid4(), 'b'*64, uuid4(), 'c'*64)
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
        if 'quality_context' in sql: return Cursor([self.context])
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
    monkeypatch.setattr(module, 'PostgresPerspectiveQualityReader', Reader)
    monkeypatch.setattr(module, 'verify_perspective_quality_result', verify)
    coordinator = module.PerspectiveQualityAdoptionCoordinator(read, write, object(),
        'photos', target_icc_sha256='c'*64, working_icc_sha256='d'*64)
    return coordinator, read, write, locations


def test_verified_decode_commits_owned_locator_witness(monkeypatch):
    coordinator, read, write, locations = fixture(monkeypatch)
    tenant, run = uuid4(), uuid4()
    assert coordinator.adopt(tenant, run) is True
    sql, args = write.calls[-1]
    assert 'record_v29_perspective_quality_receipt' in sql
    assert args[:2] == (tenant, run)
    assert args[2:5] == read.context[1:4]
    assert json.loads(args[5]) == {'verified': True}
    assert json.loads(args[6]) == locations
    assert read.commits == write.commits == 1



@pytest.mark.parametrize('error',['PERSPECTIVE_QUALITY_RESULT_STALE','PERSPECTIVE_QUALITY_BYTES_INVALID'])
def test_invalid_or_stale_report_never_commits(monkeypatch,error):
    coordinator,read,write,_=fixture(monkeypatch,error)
    with pytest.raises(AdoptionInvalid,match=error):coordinator.adopt(uuid4(),uuid4())
    assert write.calls==[]

def test_receipt_race_is_not_success(monkeypatch):
    coordinator,read,write,_=fixture(monkeypatch)
    write.accept=False
    with pytest.raises(AdoptionInvalid,match='COMMIT_STALE'):coordinator.adopt(uuid4(),uuid4())
    assert write.commits==1

@pytest.mark.parametrize('error',['PERSPECTIVE_QUALITY_CONTRACT_INVALID','PERSPECTIVE_QUALITY_DECISION_INVALID','PERSPECTIVE_QUALITY_BINDING_INVALID'])
def test_invalid_report_rejects_exact_result(monkeypatch,error):
    coordinator,read,write,_=fixture(monkeypatch,error)
    tenant,run=uuid4(),uuid4()
    assert coordinator.adopt(tenant,run) is False
    sql,args=write.calls[-1]
    assert 'reject_v29_perspective_quality' in sql
    assert args==(tenant,run,*read.context[1:4],error)
