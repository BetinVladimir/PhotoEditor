"""Worker startup rejects privileged/malformed selected-role evidence."""
from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.runtime.database import TaskDatabase


@pytest.mark.parametrize('reply', [None, (False,), (None,), (1,), ('true',), (True,)])
def test_worker_ping_requires_exact_safe_role_evidence(monkeypatch, reply):
    queries = []
    @contextmanager
    def connect():
        def execute(query):
            queries.append(query)
            return SimpleNamespace(fetchone=lambda: reply)
        yield SimpleNamespace(execute=execute)
    database = TaskDatabase('unused')
    monkeypatch.setattr(database, '_connect', connect)
    if reply == (True,) and type(reply[0]) is bool:
        database.ping()
    else:
        with pytest.raises(ValueError, match='WORKER_DATABASE_ROLE_ISOLATION_INVALID'):
            database.ping()
    assert len(queries) == 1
    assert "current_user='photo_worker'" in queries[0]
    assert 'rolsuper' in queries[0] and 'rolbypassrls' in queries[0]
    assert "privileged.rolname IN('photo_fn_owner','photo_runtime_owner')" in queries[0]
    assert 'rolcreaterole' in queries[0] and 'rolreplication' in queries[0]
    assert 'n.nspowner=privileged.oid' in queries[0]
    assert 'c.relowner=privileged.oid' in queries[0]
    assert 'p.prosecdef AND p.proowner=privileged.oid' in queries[0]
    assert "has_schema_privilege(session_user,'photo','CREATE')" in queries[0]


@pytest.mark.parametrize('safe', [False, True])
def test_every_worker_connection_checks_role_and_closes_on_refusal(monkeypatch, safe):
    closed = []
    connection = SimpleNamespace(execute=lambda sql: SimpleNamespace(fetchone=lambda: (safe,)),
                                 close=lambda: closed.append(True))
    monkeypatch.setattr('photo_worker.runtime.database.psycopg.connect', lambda *a, **kw: connection)
    database = TaskDatabase('unused')
    if safe:
        assert database._connect() is connection
        assert closed == []
    else:
        with pytest.raises(ValueError, match='WORKER_DATABASE_ROLE_ISOLATION_INVALID'):
            database._connect()
        assert closed == [True]
