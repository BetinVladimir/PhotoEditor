"""Startup refusal before consuming work when migration ABI is incomplete."""
from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.perspective_quality_schema import REQUIRED_FUNCTIONS, require_perspective_quality_schema


class Cursor:
    def __init__(self, row): self.row = row
    def fetchone(self): return self.row


class Connection:
    autocommit = True
    def __init__(self, unavailable=None):
        self.info = SimpleNamespace(transaction_status=TransactionStatus.IDLE)
        self.role, self.unavailable, self.checked = 'photo_scheduler', unavailable, []
    @contextmanager
    def transaction(self):
        assert self.info.transaction_status == TransactionStatus.IDLE
        self.info.transaction_status = TransactionStatus.INTRANS
        try: yield
        finally: self.info.transaction_status = TransactionStatus.IDLE
    def execute(self, sql, args=None):
        assert self.info.transaction_status == TransactionStatus.INTRANS
        if sql == 'SELECT current_user': return Cursor((self.role,))
        if 'statement_timeout' in sql: return Cursor(None)
        self.checked.append(args[0])
        return Cursor(None if args[0] == self.unavailable else (True,))


def test_exact_abi_checked_and_transaction_closed():
    connection = Connection()
    require_perspective_quality_schema(connection)
    assert tuple(connection.checked) == REQUIRED_FUNCTIONS
    assert connection.info.transaction_status == TransactionStatus.IDLE


@pytest.mark.parametrize('signature', REQUIRED_FUNCTIONS)
def test_missing_function_refuses_startup(signature):
    connection = Connection(signature)
    with pytest.raises(ValueError, match='schema unavailable'):
        require_perspective_quality_schema(connection)
    assert connection.info.transaction_status == TransactionStatus.IDLE


def test_role_and_outer_transaction_refused_before_catalog_scan():
    connection = Connection()
    connection.role = 'photo_worker'
    with pytest.raises(ValueError, match='photo_scheduler'): require_perspective_quality_schema(connection)
    connection.info.transaction_status = TransactionStatus.INTRANS
    with pytest.raises(ValueError, match='idle autocommit'): require_perspective_quality_schema(connection)
    assert connection.checked == []
