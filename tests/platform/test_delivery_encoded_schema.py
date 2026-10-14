from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from psycopg.pq import TransactionStatus
from photo_worker.v2.delivery_encoded_schema import REQUIRED_FUNCTIONS, require_delivery_encode_schema


class Connection:
    autocommit=True
    info=SimpleNamespace(transaction_status=TransactionStatus.IDLE)
    def __init__(self, missing=None, role='photo_scheduler'):
        self.missing=missing;self.role=role;self.calls=[];self.committed=False;self.rolled_back=False
    @contextmanager
    def transaction(self):
        try:yield
        except BaseException:self.rolled_back=True;raise
        else:self.committed=True
    def execute(self,query,args=None):
        self.calls.append((query,args))
        row=(self.role,) if query=='SELECT current_user' else None if args and args[0]==self.missing else (True,)
        return SimpleNamespace(fetchone=lambda:row)


def test_delivery_schema_checks_every_exact_signature():
    connection=Connection()
    require_delivery_encode_schema(connection)
    assert connection.committed and not connection.rolled_back
    assert tuple(args[0] for _,args in connection.calls if args)==REQUIRED_FUNCTIONS


@pytest.mark.parametrize('signature',REQUIRED_FUNCTIONS)
def test_delivery_schema_refuses_missing_functions(signature):
    connection=Connection(missing=signature)
    with pytest.raises(ValueError,match='schema unavailable'):require_delivery_encode_schema(connection)
    assert connection.rolled_back and not connection.committed


def test_delivery_schema_refuses_wrong_role_before_function_reads():
    connection=Connection(role='photo_worker')
    with pytest.raises(ValueError,match='photo_scheduler'):require_delivery_encode_schema(connection)
    assert len(connection.calls)==1 and connection.rolled_back


@pytest.mark.parametrize('autocommit,status',[(False,TransactionStatus.IDLE),(True,TransactionStatus.INTRANS)])
def test_delivery_schema_refuses_ambient_transaction_before_sql(autocommit,status):
    connection=Connection();connection.autocommit=autocommit
    connection.info=SimpleNamespace(transaction_status=status)
    with pytest.raises(ValueError,match='idle autocommit'):require_delivery_encode_schema(connection)
    assert connection.calls==[]
