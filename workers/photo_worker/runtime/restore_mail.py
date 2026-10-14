"""Fence restored mail workers through the canonical mail reconciler.

No SMTP calls are made. Message IDs and encrypted bodies follow the existing
retry/expiry policy; terminal rows are never resurrected for another delivery.
"""
from dataclasses import dataclass
import psycopg
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreMailEvidence:
    schema_version: str
    database: str
    sending_leases_expired: int
    reconciled_payloads: int


def recover_restored_mail(connection,*,expected_database,assert_isolated_and_paused,
                          check_active,maximum_payloads=10_000_000):
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_isolated_and_paused) or not callable(check_active)
            or type(maximum_payloads) is not int or not 1<=maximum_payloads<=10_000_000):
        raise ValueError('RESTORE_MAIL_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
        if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    def authority():
        connection.execute("SET LOCAL statement_timeout = '30s'")
        connection.execute("SET LOCAL lock_timeout = '5s'")
        row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_MAIL_DATABASE_AUTHORITY')
    def before_commit():
        require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    with connection.transaction():
        authority()
        total=connection.execute("SELECT count(*) FROM photo.mail_payloads WHERE status IN ('queued','retry','sending')").fetchone()[0]
        if type(total) is not int or not 0<=total<=maximum_payloads:
            raise ValueError('RESTORE_MAIL_PAYLOAD_LIMIT')
        expired=connection.execute("UPDATE photo.mail_payloads SET lease_until='-infinity'::timestamptz WHERE status='sending'").rowcount
        if type(expired) is not int or not 0<=expired<=total:
            raise ValueError('RESTORE_MAIL_RECEIPT')
        before_commit()
    recovered=0
    while True:
        guard()
        with connection.transaction():
            authority()
            count=connection.execute('SELECT photo.reconcile_mail(100)').fetchone()[0]
            if type(count) is not int or not 0<=count<=100:
                raise ValueError('RESTORE_MAIL_RECEIPT')
            recovered+=count
            if recovered>maximum_payloads:raise ValueError('RESTORE_MAIL_PAYLOAD_LIMIT')
            before_commit()
        if count==0:break
    guard()
    with connection.transaction():
        authority()
        remaining=connection.execute("SELECT count(*) FROM photo.mail_payloads WHERE status='sending' OR (status IN ('queued','retry') AND expires_at<=clock_timestamp())").fetchone()
        if remaining!=(0,):raise ValueError('RESTORE_MAIL_LEASES_REMAIN')
        before_commit()
    guard()
    return RestoreMailEvidence('restore-mail-recovery/v1',expected_database,expired,recovered)
