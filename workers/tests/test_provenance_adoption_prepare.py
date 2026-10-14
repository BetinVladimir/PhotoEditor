from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef, AdoptionInvalid
import photo_worker.v2.provenance_adoption_prepare as module


class Connection:
    autocommit = True
    def __init__(self, rows):
        self.rows = iter(rows)
        self.calls = []
    def execute(self, statement, values):
        self.calls.append(values)
        return SimpleNamespace(fetchone=lambda: next(self.rows))


def context():
    return SimpleNamespace(operation='provenance.sign', version='1',
        attempt=2, fence=9, result_id=uuid4(), root=ArtifactRef(uuid4(), 'b'*64))


@pytest.mark.parametrize('mode', ['valid', 'revoked', 'changed', 'initial-denied'])
def test_fenced_prepare_rechecks_after_object_io(monkeypatch, mode):
    params = (uuid4(), 'a'*64)
    final = None if mode == 'revoked' else (uuid4(), 'c'*64) if mode == 'changed' else params
    db = Connection([None] if mode == 'initial-denied' else [params, final])
    tenant, task, ctx = uuid4(), uuid4(), context()
    observed = []
    expected = object()
    sdk = object()
    def prepare(**kwargs):
        observed.append(kwargs)
        assert len(db.calls) == 1
        assert kwargs['params_ref'] == ArtifactRef(*params)
        assert kwargs['sdk'] is sdk
        return expected
    monkeypatch.setattr(module, 'prepare_signing_adoption', prepare)
    call = lambda: module.prepare_current_signing(db, object(), tenant, task, ctx, object(), object(), sdk=sdk)
    if mode == 'valid':
        assert call() is expected
        assert db.calls == [(tenant, task, 2, 9, ctx.result_id)] * 2
    else:
        with pytest.raises(AdoptionInvalid):
            call()
    assert len(observed) == (0 if mode == 'initial-denied' else 1)


def test_prepare_rejects_transactional_reader_before_io(monkeypatch):
    db = Connection([])
    db.autocommit = False
    with pytest.raises(ValueError, match='autocommit'):
        module.prepare_current_signing(db, object(), uuid4(), uuid4(), context(), object(), object())
    assert db.calls == []


def test_prepare_cancellation_prevents_authority_read():
    db = Connection([])
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        module.prepare_current_signing(db, object(), uuid4(), uuid4(), context(), object(), object(), check_active=cancelled)
    assert db.calls == []


def test_prepare_false_activity_prevents_authority_read():
    db = Connection([])
    with pytest.raises(AdoptionInvalid, match='SIGNING_ADOPTION_INACTIVE'):
        module.prepare_current_signing(db, object(), uuid4(), uuid4(), context(), object(), object(),
                                       check_active=lambda: False)
    assert db.calls == []


def test_prepare_false_activity_after_authority_read_prevents_object_io(monkeypatch):
    db = Connection([(uuid4(), 'a'*64)])
    calls = 0
    def active():
        nonlocal calls
        calls += 1
        return calls == 1
    def forbidden(**_kwargs):
        raise AssertionError('physical verification started after cancellation')
    monkeypatch.setattr(module, 'prepare_signing_adoption', forbidden)
    with pytest.raises(AdoptionInvalid, match='SIGNING_ADOPTION_INACTIVE'):
        module.prepare_current_signing(db, object(), uuid4(), uuid4(), context(), object(), object(),
                                       check_active=active)
    assert len(db.calls) == 1
