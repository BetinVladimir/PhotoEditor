"""Independent one-frame snapshot and absent batch-adoption observation."""
from dataclasses import dataclass
from uuid import UUID, uuid5
import psycopg
from .restore_revoke_batch import RestoreRenderBatchTarget
from .restore_revoke_freeze import RestoreFreezeBaselineBinding
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreBatchCatalogueEvidence:
    schema_version: str
    database: str
    batch_id: UUID
    snapshot_id: UUID


def verify_fixture_batch_not_adopted(connection, target, binding, *, expected_database, session_id,
                                     assert_fixture_scope, check_active):
    if (not isinstance(target, RestoreRenderBatchTarget) or not isinstance(binding, RestoreFreezeBaselineBinding)
            or any(not isinstance(v, UUID) or v.int == 0 for v in
                (target.tenant_id,target.project_id,target.snapshot_id,binding.actor_id,binding.asset_version_id,binding.source_artifact_id))
            or not isinstance(session_id,UUID) or session_id.int==0
            or not isinstance(target.idempotency_key,str) or not 1<=len(target.idempotency_key)<=128
            or any(not 33<=ord(c)<=126 for c in target.idempotency_key)
            or not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_BATCH_CATALOGUE_INSTALLATION')
    batch=uuid5(target.tenant_id,'V2.Admit:'+str(binding.actor_id)+':'+target.idempotency_key)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        scope=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if scope!=(expected_database,True):raise ValueError('RESTORE_BATCH_CATALOGUE_AUTHORITY')
        ready=connection.execute('SELECT photo.v25_lens_snapshot_ready(%s,%s,%s,%s,%s)',
            (target.tenant_id,target.project_id,target.snapshot_id,binding.actor_id,session_id)).fetchone()
        if ready!=(True,):raise ValueError('RESTORE_BATCH_CATALOGUE_READINESS')
        row=connection.execute('''SELECT EXISTS(SELECT FROM photo.snapshots s
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.tenants t ON t.id=a.tenant_id AND t.status='active'
 WHERE s.tenant_id=%s AND s.id=%s AND s.principal_type='user' AND s.principal_id=%s
 AND s.item_count=1 AND (s.expires_at IS NULL OR s.expires_at>transaction_timestamp())
 AND i.ordinal=0 AND i.asset_version_id=%s AND i.artifact_id=%s AND i.sha256=%s
 AND a.project_id=%s AND a.deleted_at IS NULL
 AND (SELECT count(*) FROM photo.snapshot_items n WHERE n.tenant_id=s.tenant_id AND n.snapshot_id=s.id)=1)''',
            (target.tenant_id,target.snapshot_id,binding.actor_id,binding.asset_version_id,binding.source_artifact_id,
             binding.source_sha256,target.project_id)).fetchone()
        if row!=(True,):raise ValueError('RESTORE_BATCH_CATALOGUE_SNAPSHOT')
        guard()
        row=connection.execute('''SELECT
 EXISTS(SELECT FROM photo.runs WHERE tenant_id=%s AND id=%s),
 EXISTS(SELECT FROM photo.v2_workflows WHERE tenant_id=%s AND run_id=%s),
 EXISTS(SELECT FROM photo.tasks WHERE tenant_id=%s AND run_id=%s),
 EXISTS(SELECT FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user'
 AND principal_id=%s AND route='V2.Admit' AND key=%s)''',
            (target.tenant_id,batch,target.tenant_id,batch,target.tenant_id,batch,
             target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if row!=(False,False,False,False):raise ValueError('RESTORE_BATCH_CATALOGUE_ADOPTED')
        guard()
    guard()
    return RestoreBatchCatalogueEvidence('restore-batch-catalogue/v1',expected_database,batch,target.snapshot_id)


def observe_guarded_fixture_batch_refusal(connection,target,binding,frozen,fixture,*,expected_database,
                                          session_id,tls_context,assert_fixture_scope,check_active):
    """Canonical snapshot readiness and committed consent surround actual POST.

    Generic422 still requires qualified positive batch/worker evidence separately.
    """
    from .restore_revoke_probe import RestoreRevokeFixture,observe_fixture_learning_consent
    from .restore_revoke_batch import observe_fixture_render_batch_refused
    if (not isinstance(fixture,RestoreRevokeFixture) or not isinstance(target,RestoreRenderBatchTarget)
            or not isinstance(binding,RestoreFreezeBaselineBinding) or fixture.tenant_id!=target.tenant_id
            or fixture.project_id!=target.project_id or fixture.actor_id!=binding.actor_id):
        raise ValueError('RESTORE_BATCH_CATALOGUE_FIXTURE')
    def guard(*_):
        require_restore_fixture(check_active,assert_fixture_scope,target,binding,frozen,fixture)
        consent=observe_fixture_learning_consent(connection,fixture,expected_database=expected_database,
            revoked=True,assert_fixture_scope=lambda _:assert_fixture_scope(target,binding,frozen,fixture),check_active=check_active)
        catalogue=verify_fixture_batch_not_adopted(connection,target,binding,expected_database=expected_database,
            session_id=session_id,assert_fixture_scope=lambda *_:assert_fixture_scope(target,binding,frozen,fixture),check_active=check_active)
        return consent,catalogue
    before=guard()
    refusal=observe_fixture_render_batch_refused(target,binding,frozen,tls_context=tls_context,
        assert_current_fixture=guard,check_active=check_active)
    after=guard()
    if before!=after:raise ValueError('RESTORE_BATCH_CATALOGUE_CHANGED')
    return before,refusal,after
