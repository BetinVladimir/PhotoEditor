"""Read-only exact preview registration evidence, never readiness evidence."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_legacy_preview import RestoreLegacyPreviewTarget,RestoreLegacyPreviewTicket,legacy_preview_request_body
from .restore_authority import require_restore_fixture
from .restore_legacy_catalogue import RestoreLegacyApplyBinding


@dataclass(frozen=True)
class RestoreLegacyPreviewRegistration:
    database: str
    run_id: UUID
    snapshot_id: UUID
    task_id: UUID
    config_sha256: str
    params_sha256: str


def observe_fixture_legacy_preview_registration(connection,target,binding,ticket,*,expected_database,
                                                assert_fixture_scope,check_active):
    if (not isinstance(target,RestoreLegacyPreviewTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(ticket,RestoreLegacyPreviewTicket) or target.asset_version_id!=binding.asset_version_id
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_PREVIEW_CATALOGUE_INSTALLATION')
    request=legacy_preview_request_body(target);fingerprint=sha256(request).hexdigest()
    if fingerprint!=ticket.request_sha256:raise ValueError('RESTORE_PREVIEW_CATALOGUE_REQUEST')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_PREVIEW_CATALOGUE_AUTHORITY')
        saved=connection.execute('''SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s
 AND route='Preview.Request' AND key=%s''',(target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if (not saved or len(saved)!=4 or saved[0]!=fingerprint or saved[1]!=202 or saved[3]!=ticket.run_id
                or not isinstance(saved[2],(bytes,bytearray,memoryview))
                or sha256(bytes(saved[2])).hexdigest()!=ticket.response_sha256):
            raise ValueError('RESTORE_PREVIEW_CATALOGUE_RECEIPT')
        row=connection.execute('''SELECT r.input_snapshot_id,r.config_snapshot,r.config_hash,
 t.task_id,t.params,t.params_sha256,t.declared_inputs
 FROM photo.runs r JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.interactive_tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.asset_versions v ON v.tenant_id=r.tenant_id AND v.id=%s
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id
 WHERE r.tenant_id=%s AND r.id=%s AND r.kind='preview' AND r.created_by=%s
 AND s.kind='input' AND s.principal_type='user' AND s.principal_id=r.created_by AND s.item_count=1
 AND s.artifact_id=%s AND s.digest=%s
 AND t.kind='preview' AND t.operation='editor.preview' AND t.ordinal=0 AND t.backend='cpu'
 AND t.runtime_lock_id IS NULL AND t.source_artifact_id=s.artifact_id
 AND a.project_id=%s AND a.deleted_at IS NULL AND ar.id=s.artifact_id
 AND ar.sha256=%s AND v.content_hash=ar.sha256 AND ar.state='ready' AND ar.object_version IS NOT NULL
 AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
 AND (SELECT count(*) FROM photo.interactive_tasks n WHERE n.tenant_id=r.tenant_id AND n.run_id=r.id)=1''',
            (target.asset_version_id,target.tenant_id,ticket.run_id,binding.actor_id,binding.source_artifact_id,
             sha256(canonical_bytes({'artifact_id':str(binding.source_artifact_id),'sha256':binding.source_sha256})).hexdigest(),
             binding.project_id,binding.source_sha256)).fetchone()
        if (not row or len(row)!=7 or any(not isinstance(row[i],UUID) or row[i].int==0 for i in (0,3))
                or not isinstance(row[1],dict) or not isinstance(row[4],dict)):
            raise ValueError('RESTORE_PREVIEW_CATALOGUE_REGISTRATION')
        snapshot,config,config_sha,task,params,params_sha,inputs=row
        expected_config={'schema_version':1,'explicit_generation':False,'asset_version_id':str(target.asset_version_id),
            'draft_revision':target.draft_revision,'style_version_id':str(target.style_version_id) if target.style_version_id else None,
            'scale':target.scale,'roi':list(target.roi) if target.roi else None}
        if config!=expected_config or sha256(canonical_bytes(config)).hexdigest()!=config_sha:
            raise ValueError('RESTORE_PREVIEW_CATALOGUE_CONFIG')
        modules=parse_json(target.params_raw)
        if 'schema_version' in modules:modules=modules['modules']
        source={'artifact_id':str(binding.source_artifact_id),'sha256':binding.source_sha256}
        style=None
        if target.style_version_id is not None:
            style_row=connection.execute('''SELECT v.export_artifact_id,v.config_hash FROM photo.style_versions v
 JOIN photo.styles s ON s.tenant_id=v.tenant_id AND s.id=v.style_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash
 WHERE v.tenant_id=%s AND v.id=%s AND s.project_id=%s AND s.archived_at IS NULL
 AND v.approval_status='accepted' AND v.config_json->>'mode'='fixed'
 AND a.state='ready' AND a.object_version IS NOT NULL''',
                (target.tenant_id,target.style_version_id,binding.project_id)).fetchone()
            if not style_row or len(style_row)!=2:raise ValueError('RESTORE_PREVIEW_CATALOGUE_STYLE')
            style={'artifact_id':str(style_row[0]),'sha256':style_row[1]}
        expected_params={'image':source,'style':style,'modules':modules,'scale':target.scale,'roi':expected_config['roi']}
        if (params!=expected_params or sha256(canonical_bytes(params)).hexdigest()!=params_sha
                or inputs!=([source,style] if style else [source])):
            raise ValueError('RESTORE_PREVIEW_CATALOGUE_PARAMS')
        guard()
    guard();return RestoreLegacyPreviewRegistration(expected_database,ticket.run_id,snapshot,task,config_sha,params_sha)
