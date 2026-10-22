"""Scheduler wire identity and cleanup ordering; no PostgreSQL or credentials output."""
from contextlib import contextmanager
import importlib.util
from pathlib import Path

import pytest
from psycopg.conninfo import conninfo_to_dict

spec = importlib.util.spec_from_file_location(
    'shown_scheduler_login',
    Path(__file__).resolve().parents[1] / 'integration/shown_scheduler_login.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

ADMIN_DSN = 'host=127.0.0.1 dbname=private user=admin password=admin-secret options=-crole=privileged'


def fake_connections(monkeypatch, *, grant_failure=False):
    events = []
    queries = []
    identity = {}
    active = set()

    class Row:
        def fetchone(self):
            return identity['login'], 'photo_scheduler'

    class Admin:
        def execute(self, query):
            text = query.as_string()
            queries.append(text)
            if text.startswith('GRANT '):
                events.append('grant')
                if grant_failure:
                    raise RuntimeError('grant refused')
            elif text.startswith('DROP ROLE '):
                # Both validation and user runtime sessions must close first.
                assert not active
                events.append('drop')
            else:
                assert text.startswith('CREATE ROLE ')
                events.append('create')

    class Runtime:
        def execute(self, query):
            assert query in ('SET ROLE photo_scheduler', 'SELECT session_user,current_user')
            events.append(query)
            return Row()

    @contextmanager
    def connect(dsn, **kwargs):
        if dsn == ADMIN_DSN:
            assert kwargs == {'autocommit': True}
            events.append('admin-open')
            try:
                yield Admin()
            finally:
                events.append('admin-close')
            return
        parsed = conninfo_to_dict(dsn)
        assert parsed['user'].startswith('wire_scheduler_')
        assert parsed['password'] != 'admin-secret'
        assert parsed['dbname'] == 'private' and parsed['host'] == '127.0.0.1'
        assert 'options' not in parsed
        identity['login'] = parsed['user']
        number = len([e for e in events if e == 'runtime-open'])
        assert number not in active
        active.add(number)
        events.append('runtime-open')
        try:
            yield Runtime()
        finally:
            active.remove(number)
            events.append('runtime-close')

    monkeypatch.setattr(module.psycopg, 'connect', connect)
    return events, queries, identity


def assert_exact_role_cleanup(events, queries):
    assert events.count('create') == events.count('drop') == 1
    assert events[-2:] == ['drop', 'admin-close']
    create, grant, drop = queries
    assert 'NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION' in create
    assert grant.startswith('GRANT photo_scheduler TO ')
    assert drop == 'DROP ROLE ' + grant.split(' TO ', 1)[1]
    assert 'photo_worker' not in grant and 'photo_api' not in grant


def test_grant_failure_drops_created_role_before_admin_close(monkeypatch):
    events, queries, _ = fake_connections(monkeypatch, grant_failure=True)
    with pytest.raises(RuntimeError, match='grant refused'):
        with module.scheduler_login(ADMIN_DSN):
            pytest.fail('grant failure must never publish runtime DSN')
    assert 'runtime-open' not in events
    assert_exact_role_cleanup(events, queries)


def test_callback_exception_preserves_error_and_cleans_role(monkeypatch):
    events, queries, identity = fake_connections(monkeypatch)
    with pytest.raises(RuntimeError, match='callback failed'):
        with module.scheduler_login(ADMIN_DSN) as dsn:
            assert conninfo_to_dict(dsn)['user'] == identity['login']
            # The helper's identity-validation connection is already closed.
            assert events[-1] == 'runtime-close'
            events.append('callback')
            raise RuntimeError('callback failed')
    assert events.index('runtime-close') < events.index('callback') < events.index('drop')
    assert_exact_role_cleanup(events, queries)


def test_user_runtime_connections_close_before_drop(monkeypatch):
    events, queries, _ = fake_connections(monkeypatch)
    with module.scheduler_login(ADMIN_DSN) as dsn:
        # Same nesting as the actual adopter probe: caller sessions close before
        # the scheduler-login context's finally removes its owned role.
        with module.psycopg.connect(dsn, autocommit=True) as read, module.psycopg.connect(dsn) as write:
            read.execute('SET ROLE photo_scheduler')
            write.execute('SET ROLE photo_scheduler')
            events.append('adopt')
            assert 'drop' not in events
    assert events.count('runtime-open') == events.count('runtime-close') == 3
    assert events[-3:] == ['runtime-close', 'drop', 'admin-close']
    assert_exact_role_cleanup(events, queries)
