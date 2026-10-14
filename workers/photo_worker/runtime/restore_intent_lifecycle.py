"""Fresh owner intent admission and registration in the isolated restore DB."""
from dataclasses import dataclass,field
from uuid import UUID
import psycopg
from .restore_download_intent import (RestoreDownloadIntentTarget,RestoreDownloadIntentTicket,
    validate_fixture_download_intent,request_fixture_download_intent)
from .restore_export_volume import RestoreExportVolume
from .restore_intent_catalogue import RestoreIntentRegistration,observe_fixture_owner_intent
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRegisteredIntent:
    ticket: RestoreDownloadIntentTicket=field(repr=False)
    registration: RestoreIntentRegistration


def create_fixture_owner_intent(connection,target,actor_id,volume,*,expected_database,pepper,tls_context,
        assert_installed_fixture,check_active):
    """One POST only. Unknown outcomes require operator reconciliation, never retry.

    Freshness preflight is observational: the independently pinned isolated
    fixture must exclusively own its idempotency key for this operation.
    This receipt grants neither delivery acceptance nor writer activation.
    """
    if (not isinstance(target,RestoreDownloadIntentTarget) or not isinstance(actor_id,UUID) or actor_id.int==0
            or not isinstance(volume,RestoreExportVolume) or type(pepper) is not bytes or len(pepper)<32
            or not isinstance(expected_database,str) or not expected_database
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_INTENT_LIFECYCLE_INSTALLATION')
    validate_fixture_download_intent(target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
    def scope(*_):guard()
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_INTENT_LIFECYCLE_AUTHORITY')
        saved=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
 WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route='Download.Intent' AND key=%s)''',
            (target.tenant_id,actor_id,target.idempotency_key)).fetchone()
        if saved!=(False,):raise ValueError('RESTORE_INTENT_ALREADY_REQUESTED')
        guard()
    guard()
    ticket=request_fixture_download_intent(target,tls_context=tls_context,assert_fixture_scope=scope,check_active=guard)
    guard()
    registration=observe_fixture_owner_intent(connection,target,ticket,actor_id,volume,
        expected_database=expected_database,pepper=pepper,check_active=guard)
    guard()
    return RestoreRegisteredIntent(ticket,registration)
