"""Invalidate expired restored download capabilities without deleting metadata."""
from dataclasses import dataclass
from datetime import datetime
import psycopg
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreIntentEvidence:
    schema_version: str
    database: str
    recovery_time: datetime
    expired_download_intents: int


def invalidate_expired_restore_intents(connection,*,expected_database,
                                      assert_isolated_and_paused,check_active):
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)):
        raise ValueError('RESTORE_INTENT_INSTALLATION')
    def guard():require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        connection.execute('SET LOCAL row_security = off')
        row=connection.execute('SELECT current_database(),transaction_timestamp(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if (not row or len(row)!=3 or row[0]!=expected_database or row[2] is not True
                or not isinstance(row[1],datetime) or row[1].tzinfo is None):
            raise ValueError('RESTORE_INTENT_DATABASE_AUTHORITY')
        cutoff=row[1]
        count=connection.execute('UPDATE photo.download_intents SET consumed_at=%s WHERE consumed_at IS NULL AND expires_at<=%s',(cutoff,cutoff)).rowcount
        if type(count) is not int or count<0:raise ValueError('RESTORE_INTENT_RECEIPT')
        remaining=connection.execute('SELECT count(*) FROM photo.download_intents WHERE consumed_at IS NULL AND expires_at<=%s',(cutoff,)).fetchone()
        if remaining!=(0,):raise ValueError('RESTORE_EXPIRED_INTENTS_REMAIN')
        guard()
    guard()
    return RestoreIntentEvidence('restore-expired-intents/v1',expected_database,cutoff,count)
