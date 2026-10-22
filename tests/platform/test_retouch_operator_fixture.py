"""Fake-only lifecycle tests for the integration operator fixture."""
from contextlib import contextmanager
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import pytest

spec=importlib.util.spec_from_file_location('retouch_operator_work',Path(__file__).resolve().parents[1]/'integration/retouch_operator_fixture.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class Connection:
    def __init__(self, flag):
        self.flag=flag;self.calls=[]
        self.info=SimpleNamespace(dbname='photo_test_'+'a'*32,transaction_status=module.TransactionStatus.IDLE)
    def execute(self, query, params=None):
        self.calls.append((query,params))
        if query==module._QUERY:return SimpleNamespace(fetchone=lambda:(self.flag,))
        if query==module._UPDATE:self.flag=params[0]
        return SimpleNamespace(rowcount=1)

class ReportManager:
    def register(self, plugin): self.plugin=plugin
    def unregister(self, plugin):
        assert self.plugin is plugin
        self.plugin=None


def request(primary=None,kind=None):
    sections=[]
    return SimpleNamespace(fixturenames=['db'] if primary else [],
        config=SimpleNamespace(pluginmanager=ReportManager()),
        getfixturevalue=lambda name:primary,node=SimpleNamespace(callspec=SimpleNamespace(params={'kind':kind}),
            sections=sections,add_report_section=lambda *args:sections.append(args)))

@pytest.mark.parametrize('original',[False,True])
def test_transaction_scope_restores_original_without_separate_admin(monkeypatch,original):
    db=Connection(original)
    monkeypatch.setattr(module.psycopg,'connect',lambda **kw:pytest.fail('separate admin would self-block'))
    gen=module.retouch_preview_operator_enabled.__wrapped__(db,request())
    next(gen);assert db.flag is True
    db.flag=False # model a withdrawal that remains transaction-owned
    with pytest.raises(StopIteration):next(gen)
    assert db.flag is original
    assert any(q=='RESET ROLE' for q,_ in db.calls)


def test_primary_db_path_never_explicitly_rolls_back_outer_transaction(monkeypatch):
    db=Connection(False)
    monkeypatch.setattr(module.psycopg,'connect',lambda **kw:pytest.fail('separate admin would self-block'))
    gen=module.retouch_preview_operator_enabled.__wrapped__({'dbname':'photo_test_'+'a'*32},request(db))
    next(gen);assert db.flag is True
    gen.close();assert db.flag is False
    # Fake has no rollback method: an explicit rollback would fail this scenario.


@pytest.mark.parametrize('original',[False,True])
def test_physical_admin_scope_spans_callback_and_restores_on_failure(monkeypatch,original):
    db=Connection(original);events=[]
    @contextmanager
    def connect(**kwargs):
        assert kwargs=={'dbname':'photo_test_'+'a'*32,'autocommit':True}
        events.append('open')
        try:yield db
        finally:events.append('close')
    monkeypatch.setattr(module.psycopg,'connect',connect)
    gen=module.retouch_preview_operator_enabled.__wrapped__({'dbname':'photo_test_'+'a'*32},request())
    next(gen);assert db.flag is True and events==['open']
    with pytest.raises(ValueError,match='callback'):
        gen.throw(ValueError('callback'))
    assert db.flag is original and events==['open','close']
    assert db.calls[:2]==[("SET statement_timeout='10s'",None),("SET lock_timeout='5s'",None)]


def test_restore_failure_is_not_suppressed(monkeypatch):
    db=Connection(False);execute=db.execute
    def guarded(query,params=None):
        if query==module._UPDATE and params==(False,):raise RuntimeError('restore lock timeout')
        return execute(query,params)
    db.execute=guarded
    @contextmanager
    def connect(**kwargs):yield db
    monkeypatch.setattr(module.psycopg,'connect',connect)
    gen=module.retouch_preview_operator_enabled.__wrapped__({'dbname':'photo_test_'+'a'*32},request())
    next(gen)
    with pytest.raises(RuntimeError,match='restore lock timeout'):next(gen)


def test_shown_compatibility_case_does_not_enable_preview(monkeypatch):
    monkeypatch.setattr(module.psycopg,'connect',lambda **kw:pytest.fail('shown changes preview registry'))
    gen=module.retouch_preview_operator_enabled.__wrapped__({'dbname':'photo_test_'+'a'*32},request(kind='shown'))
    next(gen)
    with pytest.raises(StopIteration):next(gen)


@pytest.mark.parametrize('name',[None,'postgres','production','photo_test_notcanonical'])
def test_transaction_foreign_database_rejected_before_any_update(name):
    db=Connection(False);db.info.dbname=name
    gen=module.retouch_preview_operator_enabled.__wrapped__(db,request())
    with pytest.raises(ValueError,match='DISPOSABLE_DATABASE_REQUIRED'):next(gen)
    assert db.calls==[]


@pytest.mark.parametrize('name',[None,'postgres','production','photo_test_notcanonical'])
def test_physical_foreign_config_rejected_before_connect(monkeypatch,name):
    monkeypatch.setattr(module.psycopg,'connect',lambda **kw:pytest.fail('foreign connection opened'))
    gen=module.retouch_preview_operator_enabled.__wrapped__({'dbname':name},request())
    with pytest.raises(ValueError,match='DISPOSABLE_DATABASE_REQUIRED'):next(gen)


def test_aborted_primary_preserves_original_exception_and_records_cleanup_note():
    db=Connection(False);gen=module.retouch_preview_operator_enabled.__wrapped__(db,request())
    next(gen);before=list(db.calls);db.info.transaction_status=module.TransactionStatus.INERROR
    error=ValueError('original SQL failure')
    with pytest.raises(ValueError,match='original SQL failure') as caught:gen.throw(error)
    assert caught.value is error and 'owning fixture rollback required' in error.__notes__[0]
    assert db.calls==before # no RESET/UPDATE attempted inside aborted transaction


def test_aborted_primary_without_original_exception_is_explicit_cleanup_failure():
    db=Connection(False);gen=module.retouch_preview_operator_enabled.__wrapped__(db,request())
    next(gen);db.info.transaction_status=module.TransactionStatus.INERROR
    with pytest.raises(RuntimeError,match='owning fixture rollback required'):next(gen)


def test_aborted_primary_after_failed_pytest_call_defers_to_owner_without_second_error():
    db=Connection(False); req=request()
    gen=module.retouch_preview_operator_enabled.__wrapped__(db,req)
    next(gen); before=list(db.calls)
    plugin=req.config.pluginmanager.plugin
    hook=plugin.pytest_runtest_makereport(req.node,None)
    next(hook)
    report=SimpleNamespace(when='call',failed=True)
    with pytest.raises(StopIteration):hook.send(SimpleNamespace(get_result=lambda:report))
    db.info.transaction_status=module.TransactionStatus.INERROR
    with pytest.raises(StopIteration):next(gen)
    assert db.calls==before
    assert req.config.pluginmanager.plugin is None
    assert req.node.sections==[('teardown','retouch operator rollback',
        'Retouch operator restore deferred: primary transaction aborted; owning fixture rollback required')]
