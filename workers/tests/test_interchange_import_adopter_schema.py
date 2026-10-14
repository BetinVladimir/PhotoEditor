"""Import recipe installer requires the current scheduler SQL ABI."""

from contextlib import nullcontext
from types import SimpleNamespace

import pytest
from psycopg.pq import TransactionStatus

from photo_worker.v2.interchange_import_adopter_schema import (
    REQUIRED_FUNCTIONS, require_interchange_import_adopter_schema)


class Connection:
    autocommit = True
    info = SimpleNamespace(transaction_status=TransactionStatus.IDLE)

    def __init__(self, *, role='photo_scheduler', missing=None):
        self.role, self.missing, self.checked = role, missing, []

    def transaction(self):
        return nullcontext()

    def execute(self, query, args=None):
        if query == 'SELECT current_user':
            return SimpleNamespace(fetchone=lambda: (self.role,))
        if query.startswith('SET LOCAL'):
            return self
        assert 'to_regprocedure' in query
        assert args[0] in REQUIRED_FUNCTIONS
        self.checked.append(args[0])
        return SimpleNamespace(fetchone=lambda: (args[0] != self.missing,))


def test_import_adopter_schema_requires_all_scheduler_functions():
    connection = Connection()
    require_interchange_import_adopter_schema(connection)
    assert tuple(connection.checked) == REQUIRED_FUNCTIONS
    with pytest.raises(ValueError, match='schema unavailable'):
        require_interchange_import_adopter_schema(Connection(missing=REQUIRED_FUNCTIONS[-1]))


def test_import_adopter_schema_rejects_foreign_role():
    connection = Connection(role='photo_worker')
    with pytest.raises(ValueError, match='photo_scheduler'):
        require_interchange_import_adopter_schema(connection)
    assert connection.checked == []
