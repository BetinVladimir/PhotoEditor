"""Saved export admission and frozen job/run catalogue, not completed export."""
from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_export import RestoreExportTarget,RestoreExportTicket,export_request_body
from .restore_export_preflight import RestoreExportSelection
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreExportRegistration:
    database: str
    download_job_id: UUID
    run_id: UUID
    config_sha256: str
    saved_response_sha256: str


def observe_fixture_export_registration(connection,target,binding,ticket,selection,*,expected_database,check_active):
    if (not isinstance(target,RestoreExportTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(ticket,RestoreExportTicket) or not isinstance(selection,RestoreExportSelection)
            or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_CATALOGUE_INSTALLATION')
    fingerprint=sha256(export_request_body(target)).hexdigest()
    if fingerprint!=ticket.request_sha256:raise ValueError('RESTORE_EXPORT_CATALOGUE_REQUEST')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_CATALOGUE_AUTHORITY')
        saved=connection.execute('''SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s
 AND route='Download.Start' AND key=%s''',(target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
        if (not saved or len(saved)!=4 or saved[0]!=fingerprint or saved[1]!=202 or saved[3]!=ticket.download_job_id
                or not isinstance(saved[2],(bytes,bytearray,memoryview))):
            raise ValueError('RESTORE_EXPORT_CATALOGUE_RECEIPT')
        # HTTP re-encodes Accepted with writeJSON, while the stored response is
        # canonical. Bind exact typed semantics, not unequal wire-byte hashes.
        body=bytes(saved[2]);accepted={'download_job_id':str(ticket.download_job_id),'status':'queued'}
        if parse_json(body)!=accepted or canonical_bytes(accepted)!=body:
            raise ValueError('RESTORE_EXPORT_CATALOGUE_RECEIPT')
        row=connection.execute('''SELECT r.id,r.config_snapshot,r.config_hash,j.preset
 FROM photo.download_jobs j JOIN photo.runs r ON r.tenant_id=j.tenant_id AND r.id=j.run_id
 JOIN photo.snapshots s ON s.tenant_id=j.tenant_id AND s.id=j.snapshot_id
 JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
 WHERE j.tenant_id=%s AND j.id=%s AND j.principal_type='user' AND j.principal_id=%s
 AND j.snapshot_id=%s AND j.collection_id=c.id AND c.project_id=%s
 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=j.principal_id
 AND s.item_count=1 AND s.digest=%s AND r.kind='download' AND r.created_by=j.principal_id
 AND r.input_snapshot_id=s.id''', (target.tenant_id,ticket.download_job_id,binding.actor_id,
            target.selection_snapshot_id,binding.project_id,selection.snapshot_digest)).fetchone()
        if not row or len(row)!=4 or not isinstance(row[0],UUID) or row[0].int==0 or not isinstance(row[1],dict):
            raise ValueError('RESTORE_EXPORT_CATALOGUE_REGISTRATION')
        run,config,digest,preset=row
        if (not isinstance(preset,dict) or set(preset)!={'id','revision','hash','config'}
                or preset['id']!=str(target.preset_id) or preset['revision']!=selection.preset_revision
                or preset['hash']!=selection.preset_sha256 or not isinstance(preset['config'],dict)
                or sha256(canonical_bytes(preset['config'])).hexdigest()!=selection.preset_sha256):
            raise ValueError('RESTORE_EXPORT_CATALOGUE_PRESET')
        expected={'schema_version':1,'pipeline':'download','pipeline_version':3,
            'input_snapshot_id':str(target.selection_snapshot_id),'input_snapshot_digest':selection.snapshot_digest,
            'preset_id':str(target.preset_id),'preset_revision':selection.preset_revision,'preset_hash':selection.preset_sha256,
            'preset':preset['config'],'failure_policy':'continue_items','scope':'selected'}
        if config!=expected or sha256(canonical_bytes(config)).hexdigest()!=digest:
            raise ValueError('RESTORE_EXPORT_CATALOGUE_CONFIG')
        require_restore_active(check_active)
    return RestoreExportRegistration(expected_database,ticket.download_job_id,run,digest,sha256(body).hexdigest())
