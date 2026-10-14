"""Canonical completed preview output catalogue, before physical verification."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_inventory import RestoreObject,_validate
from .restore_legacy_preview_catalogue import observe_fixture_legacy_preview_registration
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyPreviewOutput:
    artifact_id: UUID
    role: str
    object: RestoreObject


@dataclass(frozen=True)
class RestoreLegacyPreviewResult:
    registration: object
    derivative: RestoreLegacyPreviewOutput
    manifest: RestoreLegacyPreviewOutput


def observe_fixture_legacy_preview_result(connection,target,binding,ticket,*,expected_database,
                                         assert_fixture_scope,check_active):
    registration=observe_fixture_legacy_preview_registration(connection,target,binding,ticket,
        expected_database=expected_database,assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_PREVIEW_RESULT_AUTHORITY')
        rows=connection.execute('''SELECT a.id,a.output_role,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id
 WHERE r.tenant_id=%s AND r.id=%s AND r.kind='preview' AND r.status='completed'
 AND r.graph_sealed AND NOT r.cancel_requested AND t.id=%s AND t.operation='editor.preview'
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
 AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND jsonb_array_length(z.outputs)=2 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND ((a.output_role='derivative' AND a.media_type='image/jpeg' AND a.bytes BETWEEN 1 AND 67108864)
 OR (a.output_role='derivative_manifest' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576))
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY a.output_role,a.id LIMIT 3''',(target.tenant_id,ticket.run_id,registration.task_id)).fetchall()
        if len(rows)!=2:raise ValueError('RESTORE_PREVIEW_RESULT_NOT_READY')
        outputs={}
        for row in rows:
            if (len(row)!=7 or not isinstance(row[0],UUID) or row[0].int==0
                    or row[1] not in ('derivative','derivative_manifest') or row[1] in outputs):
                raise ValueError('RESTORE_PREVIEW_RESULT_OUTPUTS')
            artifact,role,*physical=row
            obj=RestoreObject(*physical);_validate(obj)
            if not obj.key.startswith('tenants/'+str(target.tenant_id)+'/'):
                raise ValueError('RESTORE_PREVIEW_RESULT_SCOPE')
            outputs[role]=RestoreLegacyPreviewOutput(artifact,role,obj)
        if len({v.artifact_id for v in outputs.values()})!=2:
            raise ValueError('RESTORE_PREVIEW_RESULT_OUTPUTS')
        guard()
    guard();return RestoreLegacyPreviewResult(registration,outputs['derivative'],outputs['derivative_manifest'])
