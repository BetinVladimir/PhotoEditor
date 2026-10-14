"""Observe the actual consent-gated immutable recipe HTTP route."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from photo_worker.runtime.restore_access_probe import (
    RestoreAccessTarget, observe_restore_access_readable, observe_restore_access_denied)
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRevokeDocumentTarget:
    fixture: RestoreRevokeFixture
    document_id: UUID
    document_sha256: str
    access: RestoreAccessTarget


def observe_fixture_revoke_document(connection, target, *, expected_database, revoked,
                                     tls_context, assert_fixture_scope, check_active):
    """200 exact immutable bytes before; domain-specific409 after, with live controls.

    Does not assert that original photos or approved exports must be deleted.
    The operator must fence fixture mutation and bind the control to this run.
    """
    import re
    if (not isinstance(target, RestoreRevokeDocumentTarget)
            or not isinstance(target.fixture, RestoreRevokeFixture)
            or not isinstance(target.document_id, UUID) or target.document_id.int == 0
            or type(revoked) is not bool or not isinstance(target.access, RestoreAccessTarget)
            or not isinstance(target.document_sha256, str)
            or re.fullmatch('[0-9a-f]{64}', target.document_sha256) is None
            or not isinstance(expected_database, str) or not 1 <= len(expected_database.encode()) <= 63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_ACCESS_INSTALLATION')
    f = target.fixture
    if (target.access.denied_path != f'/api/v1/tenants/{f.tenant_id}/v2-documents/{target.document_id}'
            or target.access.expected_status != 409 or target.access.expected_code != 'PROFILE_CONSENT_REVOKED'):
        raise ValueError('RESTORE_REVOKE_ACCESS_ROUTE')
    def guard():
        require_restore_fixture(check_active,assert_fixture_scope,target)
    def catalogue():
        guard()
        if connection.info.transaction_status != psycopg.pq.TransactionStatus.IDLE:
            raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
            connection.execute("SET LOCAL statement_timeout='5s'")
            connection.execute('SET LOCAL row_security=off')
            scope = connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if scope != (expected_database, True): raise ValueError('RESTORE_REVOKE_ACCESS_AUTHORITY')
            row = connection.execute('''SELECT EXISTS(SELECT FROM photo.v2_documents d
 JOIN photo.model_use_links l ON l.tenant_id=d.tenant_id AND l.project_id=d.project_id AND l.document_id=d.id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 WHERE d.tenant_id=%s AND d.project_id=%s AND d.id=%s AND d.document_type='EditingRecipeV3'
 AND d.sha256=%s AND l.version_id=%s AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp()))''',
                (f.tenant_id, f.project_id, target.document_id, target.document_sha256, f.learning_version_id)).fetchone()
            if row != (True,): raise ValueError('RESTORE_REVOKE_ACCESS_LINEAGE')
            guard()
        guard()
    catalogue()
    kwargs = dict(tls_context=tls_context, assert_fixture_scope=lambda _: guard(), check_active=check_active)
    result = (observe_restore_access_denied(target.access, **kwargs) if revoked else
        observe_restore_access_readable(target.access, target.document_sha256, **kwargs))
    catalogue()
    return result
