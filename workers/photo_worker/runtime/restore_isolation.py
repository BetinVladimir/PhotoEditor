"""Concrete SQL isolation checks for a pre-fenced operator restore database.

Operator opens dedicated connection, disables new connections on isolated DB,
then stops all other client sessions. This adapter never terminates sessions or
changes ALLOW_CONNECTIONS. External storage/outgoing isolation remains required.
"""
from contextlib import nullcontext
from pathlib import PurePosixPath
import re
import psycopg


class SQLRestoreIsolationGuard:
    def __init__(self,connection,*,expected_database,expected_system_identifier,
                 expected_data_directory,assert_external_isolation,check_active):
        if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
                or expected_database in ('postgres','template0','template1')
                or not isinstance(expected_system_identifier,str)
                or re.fullmatch('[1-9][0-9]{0,19}',expected_system_identifier) is None
                or not isinstance(expected_data_directory,str)
                or not expected_data_directory.startswith('/')
                or str(PurePosixPath(expected_data_directory))!=expected_data_directory
                or '..' in PurePosixPath(expected_data_directory).parts
                or not callable(assert_external_isolation) or not callable(check_active)):
            raise ValueError('RESTORE_ISOLATION_INSTALLATION')
        self.connection=connection;self.database=expected_database
        self.system=expected_system_identifier;self.directory=expected_data_directory
        self.external=assert_external_isolation;self.active=check_active

    def __call__(self):
        def active():
            if self.active() is False:
                raise RuntimeError('RESTORE_ISOLATION_INACTIVE')
        def external():
            if self.external() is False:
                raise RuntimeError('RESTORE_EXTERNAL_ISOLATION_DENIED')
        active();external();active()
        status=self.connection.info.transaction_status
        if status not in (psycopg.pq.TransactionStatus.IDLE,psycopg.pq.TransactionStatus.INTRANS):
            raise ValueError('RESTORE_ISOLATION_CONNECTION_STATE')
        idle=status==psycopg.pq.TransactionStatus.IDLE
        with self.connection.transaction() if idle else nullcontext():
            if idle:self.connection.execute("SET LOCAL statement_timeout = '5s'")
            self.connection.execute('SELECT pg_stat_clear_snapshot()')
            row=self.connection.execute('''SELECT current_database(),
 (SELECT system_identifier::text FROM pg_control_system()),
 current_setting('data_directory'),pg_is_in_recovery(),
 (SELECT datallowconn FROM pg_database WHERE datname=current_database()),
 EXISTS(SELECT FROM pg_stat_activity WHERE datid=(SELECT oid FROM pg_database WHERE datname=current_database())
   AND pid<>pg_backend_pid() AND backend_type='client backend'),
 (SELECT rolsuper FROM pg_roles WHERE rolname=current_user)''').fetchone()
            if row!=(self.database,self.system,self.directory,False,False,False,True):
                raise ValueError('RESTORE_DATABASE_NOT_ISOLATED')
            active()
        external();active()
