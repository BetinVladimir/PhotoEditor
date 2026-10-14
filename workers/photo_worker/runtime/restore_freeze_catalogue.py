"""Independent current-draft/no-idempotency SQL guard for a freeze denial."""
from dataclasses import dataclass
from uuid import UUID
import re
import psycopg
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeDenialTarget
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture
from photo_worker.runtime.restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreFreezeCatalogueEvidence:
    schema_version: str
    database: str
    tenant_id: UUID
    document_id: UUID
    asset_version_id: UUID
    revision: int


def verify_fixture_freeze_catalogue(connection, target, fixture, asset_version_id, document_sha256, *,
                                    expected_database, assert_fixture_scope, check_active):
    if (not isinstance(target, RestoreFreezeDenialTarget) or not isinstance(fixture, RestoreRevokeFixture)
            or target.tenant_id != fixture.tenant_id or not isinstance(asset_version_id, UUID)
            or asset_version_id.int == 0 or not isinstance(document_sha256, str)
            or re.fullmatch('[0-9a-f]{64}', document_sha256) is None
            or type(target.revision) is not int or not 1 <= target.revision <= 2**63-1
            or not isinstance(expected_database, str) or not 1 <= len(expected_database.encode()) <= 63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_FREEZE_CATALOGUE_INSTALLATION')
    def guard():
        require_restore_fixture(check_active, assert_fixture_scope, target, fixture)
    guard()
    if connection.info.transaction_status != psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope = connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope != (expected_database, True): raise ValueError('RESTORE_FREEZE_CATALOGUE_AUTHORITY')
        row = connection.execute('''SELECT EXISTS(SELECT FROM photo.assets a
 JOIN photo.tenants t ON t.id=a.tenant_id AND t.status='active'
 JOIN LATERAL (SELECT id,master_artifact_id,content_hash,review_state FROM photo.asset_versions
 WHERE tenant_id=a.tenant_id AND asset_id=a.id ORDER BY version_no DESC LIMIT 1) v ON true
 JOIN photo.artifacts m ON m.tenant_id=a.tenant_id AND m.id=v.master_artifact_id AND m.sha256=v.content_hash
 JOIN photo.v2_document_streams s ON s.tenant_id=a.tenant_id AND s.project_id=a.project_id AND s.asset_version_id=v.id
 JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.project_id=s.project_id AND d.stream_id=s.id AND d.id=s.current_document_id
 JOIN photo.artifacts b ON b.tenant_id=d.tenant_id AND b.id=d.artifact_id AND b.sha256=d.sha256
 JOIN photo.model_use_links l ON l.tenant_id=d.tenant_id AND l.project_id=d.project_id AND l.document_id=d.id
 WHERE a.tenant_id=%s AND a.project_id=%s AND a.id=%s AND a.deleted_at IS NULL AND a.upload_state='verified'
 AND v.id=%s AND v.review_state='accepted' AND m.state='ready' AND m.verified_at IS NOT NULL
 AND m.object_version IS NOT NULL AND m.object_version NOT IN ('','null')
 AND (m.expires_at IS NULL OR m.expires_at>transaction_timestamp())
 AND s.stream_type='draft' AND s.revision=%s AND d.id=%s AND d.sha256=%s
 AND d.document_type='EditingRecipeV3' AND l.version_id=%s AND b.state='ready' AND b.output_role='document'
 AND b.verified_at IS NOT NULL AND b.object_version IS NOT NULL AND b.object_version NOT IN ('','null')
 AND (b.expires_at IS NULL OR b.expires_at>transaction_timestamp()))''',
            (fixture.tenant_id, fixture.project_id, target.asset_id, asset_version_id, target.revision,
             target.document_id, document_sha256, fixture.learning_version_id)).fetchone()
        if row != (True,): raise ValueError('RESTORE_FREEZE_CATALOGUE_DRAFT')
        guard()
        row = connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
 WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route=%s AND key=%s)''',
            (fixture.tenant_id, fixture.actor_id, 'Render.Freeze.' + str(target.asset_id), target.idempotency_key)).fetchone()
        if row != (False,): raise ValueError('RESTORE_FREEZE_CATALOGUE_REPLAY')
        guard()
    guard()
    return RestoreFreezeCatalogueEvidence('restore-freeze-catalogue/v1', expected_database,
        fixture.tenant_id, target.document_id, asset_version_id, target.revision)


def observe_guarded_fixture_freeze_denial(connection, target, fixture, asset_version_id, document_sha256, *,
                                         expected_database, tls_context, assert_fixture_scope, check_active):
    """Recheck committed consent and unchanged draft/no receipt around actual POST.

    Qualification/positive execution baseline remains separate: this composition
    must not alone be promoted to complete restore acceptance.
    """
    from photo_worker.runtime.restore_revoke_probe import observe_fixture_learning_consent
    from photo_worker.runtime.restore_revoke_freeze import observe_fixture_freeze_denied
    def guard(_):
        require_restore_fixture(check_active, assert_fixture_scope, target, fixture)
        consent = observe_fixture_learning_consent(connection, fixture, expected_database=expected_database,
            revoked=True, assert_fixture_scope=lambda _: assert_fixture_scope(target, fixture), check_active=check_active)
        catalogue = verify_fixture_freeze_catalogue(connection, target, fixture, asset_version_id, document_sha256,
            expected_database=expected_database, assert_fixture_scope=assert_fixture_scope, check_active=check_active)
        return consent, catalogue
    before = guard(target)
    denial = observe_fixture_freeze_denied(target, tls_context=tls_context,
        assert_current_fixture=guard, check_active=check_active)
    after = guard(target)
    if before != after: raise ValueError('RESTORE_FREEZE_CATALOGUE_CHANGED')
    return before, denial, after
