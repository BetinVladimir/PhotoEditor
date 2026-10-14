"""Pre-mutation independence of the two consent restore fixtures."""
import psycopg
from .restore_revoke_probe import RestoreRevokeFixture
from .restore_authority import require_restore_active


def verify_revoke_fixture_independence(connection,first,second,*,expected_database,check_active):
    if (not isinstance(first,RestoreRevokeFixture) or not isinstance(second,RestoreRevokeFixture)
            or (first.tenant_id,first.project_id,first.actor_id)!=(second.tenant_id,second.project_id,second.actor_id)
            or first.consent_id==second.consent_id or first.learning_version_id==second.learning_version_id):
        raise ValueError('RESTORE_REVOKE_FIXTURES_NOT_INDEPENDENT')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_REVOKE_INDEPENDENCE_AUTHORITY')
        row=connection.execute('''SELECT EXISTS(SELECT FROM photo.learning_example_links
 WHERE tenant_id=%s AND project_id=%s AND
 ((version_id=%s AND consent_id=%s) OR (version_id=%s AND consent_id=%s)))''',
            (first.tenant_id,first.project_id,first.learning_version_id,second.consent_id,
             second.learning_version_id,first.consent_id)).fetchone()
        if row!=(False,):raise ValueError('RESTORE_REVOKE_FIXTURES_CROSS_LINKED')
        require_restore_active(check_active)
    require_restore_active(check_active)
