"""Fence restored publisher claims without emitting external messages.

Replay is performed later by the existing publisher after explicit operator
approval. Event IDs, payloads, attempts and delivery acknowledgements survive.
"""
from dataclasses import dataclass
import psycopg
from uuid import uuid4
from .restore_outbox_inventory import pending_task_inventory
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreOutboxEvidence:
    schema_version: str
    database: str
    task_claims_invalidated: int
    mail_claims_invalidated: int
    pending_task_events: int
    pending_mail_events: int
    pending_task_sha256: str


def prepare_restore_outbox(connection,*,expected_database,
                           assert_isolated_and_paused,check_active):
    """Atomically remove obsolete claim timestamps from both publisher queues.

    Old mark_outbox_sent calls require the exact nonnull timestamp and thus
    cannot acknowledge a new claim. Already sent records remain sent; delivery
    reconciliation after broker loss requires a separate external checkpoint.
    """
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)):
        raise ValueError('RESTORE_OUTBOX_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        connection.execute('SET LOCAL row_security = off')
        row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_OUTBOX_DATABASE_AUTHORITY')
        task=connection.execute('UPDATE photo.outbox SET publish_lease_until=NULL WHERE publish_lease_until IS NOT NULL').rowcount
        guard()
        mail=connection.execute('UPDATE photo.auth_mail_outbox SET publish_lease_until=NULL WHERE publish_lease_until IS NOT NULL').rowcount
        guard()
        row=connection.execute('''SELECT
 (SELECT count(*) FROM photo.outbox WHERE publish_lease_until IS NOT NULL),
 (SELECT count(*) FROM photo.auth_mail_outbox WHERE publish_lease_until IS NOT NULL),
 (SELECT count(*) FROM photo.outbox WHERE sent_at IS NULL),
 (SELECT count(*) FROM photo.auth_mail_outbox WHERE sent_at IS NULL)''').fetchone()
        if (not row or len(row)!=4 or any(type(n) is not int or n<0 for n in row)
                or row[0]!=0 or row[1]!=0 or type(task) is not int or task<0
                or type(mail) is not int or mail<0):
            raise ValueError('RESTORE_OUTBOX_RECEIPT')
        guard()
        with connection.cursor(name='restore_outbox_'+uuid4().hex) as cursor:
            cursor.itersize=128
            cursor.execute('SELECT id,tenant_id,event_id,destination,routing_key,payload FROM photo.outbox WHERE sent_at IS NULL ORDER BY id')
            inventory_sha=pending_task_inventory(cursor,expected_count=row[2],check_active=guard)
        guard()
        evidence=RestoreOutboxEvidence('restore-outbox-preparation/v1',expected_database,
                                       task,mail,row[2],row[3],inventory_sha)
    guard()
    return evidence
