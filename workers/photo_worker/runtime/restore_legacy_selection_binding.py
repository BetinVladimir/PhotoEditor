"""Derive a fresh selection target from the physically adopted Apply output."""
from dataclasses import dataclass
from hashlib import sha256
import re
from uuid import UUID

import psycopg

from photo_worker.contracts.base import canonical_bytes
from .restore_export_preflight import RestoreExportSelection,one_item_selection_digest
from .restore_legacy_apply import RestoreLegacyApplyTarget
from .restore_legacy_catalogue import RestoreLegacyApplyBinding
from .restore_legacy_result import RestoreLegacyApplyResult
from .restore_selection import RestoreSelectionTarget
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacySelectionBinding:
    target: RestoreSelectionTarget
    expected: RestoreExportSelection
    payload_sha256: str


def bind_fixture_legacy_selection(connection,apply_target,binding,applied,*,
                                  preset_id,preset_revision,preset_sha256,selection_key,
                                  expected_database,assert_fixture_scope,check_active):
    """Server-created collection/item IDs come only from confirmed Apply lineage."""
    if (not isinstance(apply_target,RestoreLegacyApplyTarget)
            or not isinstance(binding,RestoreLegacyApplyBinding)
            or not isinstance(applied,RestoreLegacyApplyResult)
            or not isinstance(preset_id,UUID) or preset_id.int==0
            or type(preset_revision) is not int or preset_revision<1
            or not isinstance(preset_sha256,str)
            or re.fullmatch('[0-9a-f]{64}',preset_sha256) is None
            or not isinstance(selection_key,str) or not 1<=len(selection_key)<=128
            or any(not 33<=ord(c)<=126 for c in selection_key)
            or selection_key==apply_target.idempotency_key
            or not isinstance(expected_database,str) or not expected_database
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_SELECTION_BINDING_INSTALLATION')
    if (applied.registration.database!=expected_database
            or applied.registration.output_collection_id==apply_target.collection_id
            or applied.item_id.int==0 or applied.asset_version_id.int==0
            or applied.artifact_id.int==0):
        raise ValueError('RESTORE_LEGACY_SELECTION_BINDING_SOURCE')
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):
            raise ValueError('RESTORE_LEGACY_SELECTION_BINDING_AUTHORITY')
        row=connection.execute('''SELECT c.revision,i.payload,p.revision,p.config_hash,p.config
 FROM photo.collections c
 JOIN photo.collection_items i ON i.tenant_id=c.tenant_id AND i.collection_id=c.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 JOIN photo.export_presets p ON p.tenant_id=c.tenant_id AND p.id=%s AND p.archived_at IS NULL
 WHERE c.tenant_id=%s AND c.id=%s AND c.project_id=%s AND c.source_run_id=%s
  AND c.source_collection_id=%s AND c.deleted_at IS NULL AND c.state<>'archived'
  AND i.id=%s AND i.asset_version_id=%s AND i.created_by_run_id=c.source_run_id
  AND v.created_by_run_id=c.source_run_id AND v.parent_asset_version_id=%s
  AND a.id=%s AND a.sha256=%s AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>transaction_timestamp())
  AND (SELECT count(*) FROM photo.collection_items n WHERE n.tenant_id=c.tenant_id AND n.collection_id=c.id)=1
 LIMIT 2''',(preset_id,apply_target.tenant_id,applied.registration.output_collection_id,
            binding.project_id,applied.registration.run_id,apply_target.collection_id,
            applied.item_id,applied.asset_version_id,binding.asset_version_id,
            applied.artifact_id,applied.object.sha256)).fetchall()
        guard()
    if len(row)!=1 or len(row[0])!=5:
        raise ValueError('RESTORE_LEGACY_SELECTION_BINDING_NOT_READY')
    revision,payload,current_preset_revision,current_preset_sha,current_config=row[0]
    if (type(revision) is not int or not 1<=revision<=9007199254740991
            or not isinstance(payload,dict)
            or current_preset_revision!=preset_revision
            or current_preset_sha!=preset_sha256
            or not isinstance(current_config,dict)
            or sha256(canonical_bytes(current_config)).hexdigest()!=preset_sha256):
        raise ValueError('RESTORE_LEGACY_SELECTION_BINDING_CHANGED')
    digest=one_item_selection_digest(applied,payload)
    target=RestoreSelectionTarget(apply_target.origin,apply_target.tenant_id,
        applied.registration.output_collection_id,applied.item_id,revision,
        selection_key,apply_target.authorization)
    expected=RestoreExportSelection(digest,preset_revision,preset_sha256)
    guard()
    return RestoreLegacySelectionBinding(target,expected,sha256(canonical_bytes(payload)).hexdigest())
