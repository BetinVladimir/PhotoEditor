"""Exact legacy Apply admission catalogue; not completed output evidence."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_legacy_apply import RestoreLegacyApplyTarget,RestoreLegacyApplyTicket,legacy_apply_request_body
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyApplyBinding:
    actor_id: UUID
    project_id: UUID
    asset_version_id: UUID
    source_artifact_id: UUID
    source_sha256: str


@dataclass(frozen=True)
class RestoreLegacyApplyRegistration:
    database: str
    run_id: UUID
    output_collection_id: UUID
    snapshot_id: UUID
    config_sha256: str


def observe_fixture_legacy_apply_registration(connection,target,binding,ticket,*,expected_database,
                                               assert_fixture_scope,check_active):
    if (not isinstance(target,RestoreLegacyApplyTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(ticket,RestoreLegacyApplyTicket)
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_CATALOGUE_INSTALLATION')
    request=legacy_apply_request_body(target)
    if sha256(request).hexdigest()!=ticket.request_sha256:raise ValueError('RESTORE_LEGACY_CATALOGUE_REQUEST')
    fingerprint=sha256(canonical_bytes({'source_collection_id':str(target.collection_id),'request':parse_json(request)})).hexdigest()
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,binding,ticket)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_LEGACY_CATALOGUE_AUTHORITY')
        saved=connection.execute('''SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s
 AND route='Run.Apply' AND key=%s''',(target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if (not saved or len(saved)!=4 or saved[0]!=fingerprint or saved[1]!=202 or saved[3]!=ticket.run_id
                or not isinstance(saved[2],(bytes,bytearray,memoryview))
                or sha256(bytes(saved[2])).hexdigest()!=ticket.response_sha256):
            raise ValueError('RESTORE_LEGACY_CATALOGUE_RECEIPT')
        row=connection.execute('''SELECT r.input_snapshot_id,r.config_snapshot,r.config_hash
 FROM photo.runs r JOIN photo.collections c ON c.tenant_id=r.tenant_id AND c.id=r.output_collection_id
 JOIN photo.apply_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id AND i.ordinal=0
 WHERE r.tenant_id=%s AND r.id=%s AND r.kind='apply' AND r.created_by=%s
 AND r.output_collection_id=%s AND r.style_version_id=%s
 AND c.project_id=%s AND c.source_collection_id=%s AND c.source_revision=%s AND c.source_run_id=r.id
 AND c.applied_style_version_id=r.style_version_id AND c.deleted_at IS NULL
 AND b.source_collection_id=c.source_collection_id AND b.style_version_id=r.style_version_id AND b.item_count=1
 AND s.kind='selection' AND s.collection_id=c.source_collection_id AND s.source_revision=c.source_revision
 AND s.principal_type='user' AND s.principal_id=r.created_by AND s.item_count=1
 AND i.item_id=%s AND i.asset_version_id=%s AND i.artifact_id=%s AND i.sha256=%s
 AND (SELECT count(*) FROM photo.snapshot_items n WHERE n.tenant_id=s.tenant_id AND n.snapshot_id=s.id)=1''',
            (target.tenant_id,ticket.run_id,binding.actor_id,ticket.output_collection_id,target.style_version_id,
             binding.project_id,target.collection_id,target.source_revision,target.item_id,binding.asset_version_id,
             binding.source_artifact_id,binding.source_sha256)).fetchone()
        if not row or len(row)!=3 or not isinstance(row[0],UUID) or row[0].int==0 or not isinstance(row[1],dict):
            raise ValueError('RESTORE_LEGACY_CATALOGUE_REGISTRATION')
        snapshot,config,digest=row
        if sha256(canonical_bytes(config)).hexdigest()!=digest:
            raise ValueError('RESTORE_LEGACY_CATALOGUE_CONFIG')
        expected={'pipeline':'apply','renderer_version':'photo_renderer_v2','source_collection_id':str(target.collection_id),
            'source_revision':target.source_revision,'input_snapshot_id':str(snapshot),
            'style_version_id':str(target.style_version_id),'scope':'selected','identity_style':False,'strength':1}
        if any(k not in config or config[k]!=v for k,v in expected.items()):
            raise ValueError('RESTORE_LEGACY_CATALOGUE_CONFIG_BINDING')
        guard()
    guard();return RestoreLegacyApplyRegistration(expected_database,ticket.run_id,ticket.output_collection_id,snapshot,digest)
