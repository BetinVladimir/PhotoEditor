"""Explicit test-only operator scope, lasting through claim and execution."""
import re
import sys
import psycopg
from psycopg.pq import TransactionStatus
import pytest

_QUERY = "SELECT v2_enabled FROM photo.operation_registry WHERE operation='semantic.preview_retouch_region' AND version='1'"
_UPDATE = "UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation='semantic.preview_retouch_region' AND version='1'"


def _disposable_database(name):
    if not isinstance(name, str) or re.fullmatch(r'photo_test_[0-9a-f]{32}', name) is None:
        raise ValueError('RETOUCH_OPERATOR_DISPOSABLE_DATABASE_REQUIRED')


@pytest.fixture
def retouch_preview_operator_enabled(database, request):
    if getattr(request.node, 'callspec', None) is not None and request.node.callspec.params.get('kind') == 'shown':
        yield
        return
    primary = request.getfixturevalue('db') if 'db' in request.fixturenames else None
    transaction_connection = primary if primary is not None else (database if not isinstance(database, dict) else None)
    if transaction_connection is not None:
        _disposable_database(transaction_connection.info.dbname)
        # Stay on the primary transaction so its registry SHARE locks cannot
        # block a separate admin restore. conftest owns rollback/transaction exit.
        previous = transaction_connection.execute(_QUERY).fetchone()
        assert previous is not None and isinstance(previous[0], bool)
        assert transaction_connection.execute(_UPDATE, (True,)).rowcount == 1
        class CallOutcome:
            failed = False

            @pytest.hookimpl(hookwrapper=True)
            def pytest_runtest_makereport(self, item, call):
                outcome = yield
                report = outcome.get_result()
                if item is request.node and report.when == 'call':
                    self.failed = report.failed

        outcome = CallOutcome()
        manager = request.config.pluginmanager
        manager.register(outcome)
        try:
            yield
        finally:
            manager.unregister(outcome)
            if transaction_connection.info.transaction_status == TransactionStatus.INERROR:
                # Executing RESET in an aborted transaction would mask the real
                # SQL failure. The owning database fixture must perform rollback.
                note = 'Retouch operator restore deferred: primary transaction aborted; owning fixture rollback required'
                original = sys.exc_info()[1]
                if original is not None:
                    original.add_note(note)
                elif outcome.failed:
                    # Pytest does not throw call failures into yield fixtures.
                    # Preserve that primary report and disclose deferred cleanup;
                    # the owning database fixture subsequently rolls back.
                    request.node.add_report_section('teardown', 'retouch operator rollback', note)
                else:
                    raise RuntimeError(note)
            else:
                transaction_connection.execute('RESET ROLE')
                assert transaction_connection.execute(_UPDATE, previous).rowcount == 1
                assert transaction_connection.execute(_QUERY).fetchone() == previous
        return
    # Physical tests close their own with-connections/Executor before returning.
    # Committed setup is needed for their separate runtime worker sessions.
    _disposable_database(database.get('dbname'))
    with psycopg.connect(**database, autocommit=True) as admin:
        _disposable_database(admin.info.dbname)
        admin.execute("SET statement_timeout='10s'")
        admin.execute("SET lock_timeout='5s'")
        previous = admin.execute(_QUERY).fetchone()
        assert previous is not None and isinstance(previous[0], bool)
        assert admin.execute(_UPDATE, (True,)).rowcount == 1
        try:
            yield
        finally:
            assert admin.execute(_UPDATE, previous).rowcount == 1
            assert admin.execute(_QUERY).fetchone() == previous
