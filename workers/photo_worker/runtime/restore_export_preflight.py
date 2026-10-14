"""Readonly one-item export selection/preset authority before native admission."""
from dataclasses import dataclass
from hashlib import sha256
import re
import psycopg
from photo_worker.contracts.base import canonical_bytes
from .restore_export import RestoreExportTarget
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_legacy_result import RestoreLegacyApplyResult
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreExportSelection:
    snapshot_digest: str
    preset_revision: int
    preset_sha256: str



def one_item_selection_digest(applied,payload):
    """Exact collection.selectionEntry wire shape, including nullable fields."""
    if (not isinstance(applied,RestoreLegacyApplyResult) or not isinstance(payload,dict)
            or set(payload)!={'position','captured_at','camera_hash'}
            or type(payload['position']) is not int or abs(payload['position'])>9007199254740991
            or any(value is not None and (not isinstance(value,str) or len(value.encode())>1048576)
                for value in (payload['captured_at'],payload['camera_hash']))):
        raise ValueError('RESTORE_EXPORT_SELECTION_PAYLOAD')
    entry={'item_id':str(applied.item_id),'asset_version_id':str(applied.asset_version_id),
        'artifact_id':str(applied.artifact_id),'sha256':applied.object.sha256,**payload}
    return sha256(canonical_bytes([entry])).hexdigest()


def verify_fixture_export_preflight(connection,target,binding,applied,expected,*,expected_database,check_active,require_fresh_key=True):
    if (type(require_fresh_key) is not bool or not isinstance(target,RestoreExportTarget) or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(applied,RestoreLegacyApplyResult) or not isinstance(expected,RestoreExportSelection)
            or type(expected.preset_revision) is not int or expected.preset_revision<1
            or any(not isinstance(v,str) or re.fullmatch('[0-9a-f]{64}',v) is None
                for v in (expected.snapshot_digest,expected.preset_sha256)) or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_PREFLIGHT_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_EXPORT_PREFLIGHT_AUTHORITY')
        if require_fresh_key:
            exists=connection.execute('''SELECT EXISTS(SELECT FROM photo.idempotency_records
     WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s AND route='Download.Start' AND key=%s)''',
                (target.tenant_id,binding.actor_id,target.idempotency_key)).fetchone()
            if exists!=(False,):raise ValueError('RESTORE_EXPORT_ALREADY_REQUESTED')
        row=connection.execute('''SELECT p.revision,p.config_hash,p.config,i.payload
 FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id AND i.ordinal=0
 JOIN photo.collection_items ci ON ci.tenant_id=i.tenant_id AND ci.id=i.item_id AND ci.collection_id=c.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=ci.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 JOIN photo.export_presets p ON p.tenant_id=s.tenant_id AND p.id=%s AND p.archived_at IS NULL
 WHERE s.tenant_id=%s AND s.id=%s AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=%s
 AND s.item_count=1 AND s.digest=%s AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp())
 AND c.id=%s AND c.project_id=%s AND c.deleted_at IS NULL AND c.source_run_id=%s
 AND c.revision=s.source_revision
 AND i.item_id=%s AND i.asset_version_id=%s AND ci.asset_version_id=i.asset_version_id
 AND i.artifact_id=%s AND i.sha256=%s AND a.id=i.artifact_id AND a.sha256=i.sha256
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (SELECT count(*) FROM photo.snapshot_items n WHERE n.tenant_id=s.tenant_id AND n.snapshot_id=s.id)=1''',
            (target.preset_id,target.tenant_id,target.selection_snapshot_id,binding.actor_id,expected.snapshot_digest,
             applied.registration.output_collection_id,binding.project_id,applied.registration.run_id,
             applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)).fetchone()
        if (not row or len(row)!=4 or row[0]!=expected.preset_revision or row[1]!=expected.preset_sha256
                or not isinstance(row[2],dict) or sha256(canonical_bytes(row[2])).hexdigest()!=expected.preset_sha256):
            raise ValueError('RESTORE_EXPORT_SELECTION_OR_PRESET')
        if one_item_selection_digest(applied,row[3])!=expected.snapshot_digest:
            raise ValueError('RESTORE_EXPORT_SELECTION_DIGEST')
        require_restore_active(check_active)
    return expected
