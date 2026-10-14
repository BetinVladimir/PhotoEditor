"""Current consent authority from independently installed live PostgreSQL.

Operator must attest source freshness/identity independently, including replica
lag if used. Database name and row privileges alone do not prove freshness.
"""
from contextlib import contextmanager
from hashlib import sha256
import psycopg
from photo_worker.contracts.base import canonical_bytes
from .restore_consent import RestoreConsentCheckpoint,_owned_row

_SQL='SELECT tenant_id,project_id,id,purpose,person_id,status,epoch,revision,policy_version FROM photo.consent_grants ORDER BY tenant_id,id'
_FIELDS=('tenant_id','project_id','id','purpose','person_id','status','epoch','revision','policy_version')


class SQLCurrentConsentAuthority:
    def __init__(self,connection,*,expected_database,assert_current_authority,
                 check_active,maximum_grants=10_000_000):
        if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
                or not callable(assert_current_authority) or not callable(check_active)
                or type(maximum_grants) is not int or not 1<=maximum_grants<=10_000_000):
            raise ValueError('CONSENT_AUTHORITY_INSTALLATION')
        self.connection=connection;self.database=expected_database
        self.assert_current=assert_current_authority;self.active=check_active
        self.maximum=maximum_grants

    def _guard(self):
        self.active();self.assert_current();self.active()

    @contextmanager
    def _rows(self,name):
        with self.connection.cursor(name=name) as cursor:
            cursor.itersize=512;cursor.execute(_SQL)
            def rows():
                for row in cursor:
                    self._guard()
                    if len(row)!=9:raise ValueError('CONSENT_AUTHORITY_ROW')
                    yield _owned_row(dict(zip(_FIELDS,(str(row[0]),str(row[1]),str(row[2]),
                        row[3],None if row[4] is None else str(row[4]),*row[5:]))))
            iterator=rows()
            try:yield iterator
            finally:iterator.close()

    @contextmanager
    def snapshot(self):
        self._guard()
        if self.connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('CONSENT_AUTHORITY_CONNECTION_NOT_IDLE')
        with self.connection.transaction():
            self.connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            self.connection.execute('SET LOCAL row_security = off')
            self.connection.execute("SET LOCAL statement_timeout = '30s'")
            row=self.connection.execute('SELECT current_database(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)').fetchone()
            if row!=(self.database,True):raise ValueError('CONSENT_AUTHORITY_DATABASE_SCOPE')
            digest=sha256(b'photoeditor.restore-consent-catalogue/v1\n');count=0;previous=None
            with self._rows('current_consent_digest') as rows:
                for row in rows:
                    key=(row['tenant_id'],row['id'])
                    if previous is not None and key<=previous:raise ValueError('CONSENT_AUTHORITY_ORDER')
                    if count>=self.maximum:raise ValueError('CONSENT_AUTHORITY_LIMIT')
                    digest.update(canonical_bytes(row)+b'\n');count+=1;previous=key
            checkpoint=RestoreConsentCheckpoint(count,digest.hexdigest())
            self._guard()
            with self._rows('current_consent_catalogue') as rows:
                exhausted=False;stream_digest=sha256(b'photoeditor.restore-consent-catalogue/v1\n')
                stream_count=0
                def stream():
                    nonlocal exhausted,stream_count
                    for row in rows:
                        stream_digest.update(canonical_bytes(row)+b'\n');stream_count+=1
                        yield row
                    exhausted=True
                iterator=stream()
                try:
                    yield checkpoint,iterator
                    if (not exhausted or stream_count!=count
                            or stream_digest.hexdigest()!=checkpoint.sha256):
                        raise ValueError('CONSENT_AUTHORITY_STREAM_INCOMPLETE')
                finally:iterator.close()
            self._guard()
        self._guard()

    def latest_checkpoint(self):
        with self.snapshot() as (checkpoint,rows):
            for _ in rows:pass
        return checkpoint
