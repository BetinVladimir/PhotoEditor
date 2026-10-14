"""Database-scoped exclusive operator recovery lock across short transactions."""
from contextlib import contextmanager,nullcontext
import psycopg

# Fixed namespace: two-int advisory locks are scoped to the current database.
_NAMESPACE=1346916180
_OPERATION=32


@contextmanager
def exclusive_restore(connection,*,expected_database,assert_isolated_and_paused,check_active):
    """Use a dedicated session connection, never a transaction-pooling proxy.

    Release must succeed before returning recovery evidence. If session identity
    changes or cleanup fails, close the dedicated connection and refuse success.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)):
        raise ValueError('RESTORE_LOCK_INSTALLATION')
    def active():
        if check_active() is False:raise RuntimeError('RESTORE_LOCK_INACTIVE')
    def guard():
        active()
        if assert_isolated_and_paused() is False:
            raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
        active()
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    acquired=False;pid=None
    try:
        with connection.transaction():
            connection.execute("SET LOCAL statement_timeout = '5s'")
            row=connection.execute('SELECT current_database(),pg_backend_pid(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if not row or len(row)!=3 or row[0]!=expected_database or type(row[1]) is not int or row[2] is not True:
                raise ValueError('RESTORE_LOCK_DATABASE_AUTHORITY')
            pid=row[1]
            # Session locks are reentrant in PostgreSQL; explicitly forbid that.
            held=connection.execute("SELECT EXISTS(SELECT FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=%s AND objid=%s AND objsubid=2 AND granted)",(_NAMESPACE,_OPERATION)).fetchone()
            if held!=(False,):raise ValueError('RESTORE_LOCK_ALREADY_HELD')
            receipt=connection.execute('SELECT pg_try_advisory_lock(%s,%s)',(_NAMESPACE,_OPERATION)).fetchone()
            acquired=receipt==(True,)
            if not acquired:raise ValueError('RESTORE_CONCURRENT_RECOVERY')
            guard()
        def assert_owned():
            guard()
            status=connection.info.transaction_status
            if status not in (psycopg.pq.TransactionStatus.IDLE,psycopg.pq.TransactionStatus.INTRANS):
                raise ValueError('RESTORE_LOCK_CONNECTION_STATE')
            idle=status==psycopg.pq.TransactionStatus.IDLE
            with connection.transaction() if idle else nullcontext():
                if idle:connection.execute("SET LOCAL statement_timeout = '5s'")
                row=connection.execute("SELECT current_database(),pg_backend_pid(),EXISTS(SELECT FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=%s AND objid=%s AND objsubid=2 AND granted)",(_NAMESPACE,_OPERATION)).fetchone()
                if row!=(expected_database,pid,True):
                    raise ValueError('RESTORE_LOCK_OWNERSHIP_LOST')
                guard()
        assert_owned()
        yield assert_owned
        assert_owned()
    finally:
        if acquired:
            try:
                if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
                    raise ValueError('RESTORE_LOCK_CONNECTION_NOT_IDLE')
                with connection.transaction():
                    connection.execute("SET LOCAL statement_timeout = '5s'")
                    row=connection.execute('SELECT current_database(),pg_backend_pid()').fetchone()
                    if row!=(expected_database,pid):raise ValueError('RESTORE_LOCK_SESSION_CHANGED')
                    receipt=connection.execute('SELECT pg_advisory_unlock(%s,%s)',(_NAMESPACE,_OPERATION)).fetchone()
                    if receipt!=(True,):raise ValueError('RESTORE_LOCK_RELEASE_FAILED')
            except BaseException:
                connection.close()
                raise
