"""Owner intent receipt/MAC binding before redeem; secret material never returned."""
from dataclasses import dataclass
from hashlib import sha256
import hmac
import base64
from datetime import datetime
from uuid import UUID
import psycopg
from .restore_authority import require_restore_active
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_download_intent import RestoreDownloadIntentTarget,RestoreDownloadIntentTicket,download_intent_request_body
from .restore_export_volume import RestoreExportVolume


def _mac(key,purpose,value):
    digest=hmac.new(key,digestmod='sha256')
    for field in (purpose,value):
        raw=field.encode();digest.update(len(raw).to_bytes(8,'big'));digest.update(raw)
    return digest.digest()


@dataclass(frozen=True)
class RestoreIntentRegistration:
    intent_id: UUID
    artifact_id: UUID
    saved_response_sha256: str


def observe_fixture_owner_intent(connection,target,ticket,actor_id,volume,*,expected_database,pepper,check_active,consumed=False):
    if (not isinstance(target,RestoreDownloadIntentTarget) or not isinstance(ticket,RestoreDownloadIntentTicket)
            or type(pepper) is not bytes or len(pepper)<32 or not isinstance(actor_id,UUID) or actor_id.int==0
            or type(consumed) is not bool or not isinstance(volume,RestoreExportVolume) or not callable(check_active)
            or ticket.request_sha256!=sha256(download_intent_request_body(target)).hexdigest()):
        raise ValueError('RESTORE_INTENT_CATALOGUE_INSTALLATION')
    expected_request=sha256(canonical_bytes({'download_job_id':str(target.download_job_id),'volume':target.volume})).hexdigest()
    require_restore_active(check_active)
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='5s'");connection.execute('SET LOCAL row_security=off')
        authority=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if authority!=(expected_database,True):raise ValueError('RESTORE_INTENT_CATALOGUE_AUTHORITY')
        saved=connection.execute('''SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=%s AND principal_type='user' AND principal_id=%s
 AND route='Download.Intent' AND key=%s''',(target.tenant_id,actor_id,target.idempotency_key)).fetchone()
        if (not saved or len(saved)!=4 or saved[0]!=expected_request or saved[1]!=201
                or not isinstance(saved[2],(bytes,bytearray,memoryview)) or not isinstance(saved[3],UUID)):
            raise ValueError('RESTORE_INTENT_CATALOGUE_RECEIPT')
        raw=bytes(saved[2]);document=parse_json(raw)
        if not isinstance(document,dict) or set(document)!={'intent_id','expires_at'} or canonical_bytes(document)!=raw:
            raise ValueError('RESTORE_INTENT_CATALOGUE_RECEIPT')
        try:
            intent=UUID(document['intent_id']);expires=datetime.fromisoformat(document['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):
            raise ValueError('RESTORE_INTENT_CATALOGUE_RECEIPT') from None
        token=base64.urlsafe_b64encode(_mac(pepper,'portal-download-bearer',str(intent))).rstrip(b'=').decode()
        if intent.int==0 or intent!=saved[3] or expires!=ticket.expires_at or not hmac.compare_digest(token,ticket.token):
            raise ValueError('RESTORE_INTENT_CATALOGUE_TOKEN')
        row=connection.execute('''SELECT i.artifact_id,i.token_hash,i.expires_at
 FROM photo.download_intents i JOIN photo.download_volumes v ON v.tenant_id=i.tenant_id AND v.artifact_id=i.artifact_id
 JOIN photo.download_jobs j ON j.tenant_id=v.tenant_id AND j.id=v.download_job_id
 WHERE i.tenant_id=%s AND i.id=%s AND i.principal_type='user' AND i.principal_id=%s
 AND j.id=%s AND j.principal_type='user' AND j.principal_id=i.principal_id
 AND j.status='ready' AND NOT j.cancel_requested AND j.expires_at>clock_timestamp()
 AND v.volume_no=%s AND v.status='ready' AND i.expires_at>clock_timestamp()
 AND i.intent_kind='volume' AND (i.consumed_at IS NOT NULL)=%s''',
            (target.tenant_id,intent,actor_id,target.download_job_id,target.volume,consumed)).fetchone()
        if (not row or len(row)!=3 or row[0]!=volume.artifact_id or row[2]!=ticket.expires_at
                or not isinstance(row[1],(bytes,bytearray,memoryview))
                or not hmac.compare_digest(bytes(row[1]),_mac(pepper,'portal-download-intent',ticket.token))):
            raise ValueError('RESTORE_INTENT_CATALOGUE_BINDING')
        require_restore_active(check_active)
    return RestoreIntentRegistration(intent,volume.artifact_id,sha256(raw).hexdigest())
