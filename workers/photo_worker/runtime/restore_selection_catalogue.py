"""Readonly saved Selection.Create receipt and adopted one-item snapshot binding."""
from dataclasses import dataclass
from datetime import datetime
from hashlib import sha256
from uuid import UUID
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_selection import RestoreSelectionTarget,RestoreSelectionTicket,selection_request_body
from .restore_export_preflight import one_item_selection_digest
from .restore_legacy_result import RestoreLegacyApplyResult
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestoreSelectionRegistration:
    snapshot_id: UUID
    digest: str
    saved_response_sha256: str


def observe_fixture_selection(connection,target,ticket,actor_id,applied,*,expected_database,check_active):
    if (not isinstance(target,RestoreSelectionTarget) or not isinstance(ticket,RestoreSelectionTicket)
            or not isinstance(actor_id,UUID) or actor_id.int==0 or not isinstance(applied,RestoreLegacyApplyResult)
            or target.collection_id!=applied.registration.output_collection_id or target.item_id!=applied.item_id
            or ticket.request_sha256!=sha256(selection_request_body(target)).hexdigest() or not callable(check_active)):
        raise ValueError('RESTORE_SELECTION_CATALOGUE_INSTALLATION')
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_SELECTION_CATALOGUE_AUTHORITY')
        row=connection.execute('''SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s
 AND route='Selection.Create' AND key=%s''',(target.tenant_id,actor_id,target.idempotency_key)).fetchone()
        if (not row or len(row)!=4 or row[0]!=ticket.request_sha256 or row[1]!=201 or row[3]!=ticket.snapshot_id
                or not isinstance(row[2],(bytes,bytearray,memoryview))):
            raise ValueError('RESTORE_SELECTION_CATALOGUE_RECEIPT')
        raw=bytes(row[2]);value=parse_json(raw,max_bytes=16384)
        if (not isinstance(value,dict) or set(value)!={'id','digest','item_count','expires_at'}
                or canonical_bytes(value)!=raw or sha256(raw).hexdigest()!=ticket.response_sha256
                or value['id']!=str(ticket.snapshot_id) or value['digest']!=ticket.digest
                or type(value['item_count']) is not int or value['item_count']!=1):
            raise ValueError('RESTORE_SELECTION_CATALOGUE_RECEIPT')
        try:expires=datetime.fromisoformat(value['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):raise ValueError('RESTORE_SELECTION_CATALOGUE_RECEIPT') from None
        if expires!=ticket.expires_at:raise ValueError('RESTORE_SELECTION_CATALOGUE_RECEIPT')
        snapshot=connection.execute('''SELECT i.payload,s.expires_at
 FROM photo.snapshots s JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id AND i.ordinal=0
 JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
 JOIN photo.collection_items ci ON ci.tenant_id=i.tenant_id AND ci.collection_id=c.id AND ci.id=i.item_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=ci.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE s.tenant_id=%s AND s.id=%s AND s.kind='selection' AND s.collection_id=%s
 AND s.principal_type='user' AND s.principal_id=%s AND s.source_revision=%s
 AND s.digest=%s AND s.item_count=1 AND s.expires_at>clock_timestamp()
 AND c.deleted_at IS NULL AND c.source_run_id=%s AND c.revision=s.source_revision
 AND i.item_id=%s AND i.asset_version_id=%s AND ci.asset_version_id=i.asset_version_id
 AND i.artifact_id=%s AND i.sha256=%s AND a.id=i.artifact_id AND a.sha256=i.sha256
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (SELECT count(*) FROM photo.snapshot_items n WHERE n.tenant_id=s.tenant_id AND n.snapshot_id=s.id)=1''',
            (target.tenant_id,ticket.snapshot_id,target.collection_id,actor_id,target.collection_revision,
             ticket.digest,applied.registration.run_id,applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)).fetchone()
        if (not snapshot or len(snapshot)!=2 or snapshot[1]!=ticket.expires_at
                or one_item_selection_digest(applied,snapshot[0])!=ticket.digest):
            raise ValueError('RESTORE_SELECTION_CATALOGUE_BINDING')
        require_restore_active(check_active)
    return RestoreSelectionRegistration(ticket.snapshot_id,ticket.digest,sha256(raw).hexdigest())
