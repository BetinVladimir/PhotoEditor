"""Native one-item selection admission for an adopted Apply collection."""
from dataclasses import dataclass,field
from datetime import datetime,timezone
from hashlib import sha256
from uuid import UUID
import re
import http.client
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_export import RestoreExportTarget,validate_fixture_export
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreSelectionTarget:
    origin: str
    tenant_id: UUID
    collection_id: UUID
    item_id: UUID
    collection_revision: int
    idempotency_key: str=field(repr=False)
    authorization: str=field(repr=False)


@dataclass(frozen=True)
class RestoreSelectionTicket:
    snapshot_id: UUID
    digest: str
    expires_at: datetime
    request_sha256: str
    response_sha256: str


def selection_request_body(target):
    return canonical_bytes({'collection_revision':str(target.collection_revision),'scope':'selected','item_ids':[str(target.item_id)]})


def validate_fixture_selection(target,*,tls_context,assert_fixture_scope,check_active):
    if (not isinstance(target,RestoreSelectionTarget) or not isinstance(target.item_id,UUID) or target.item_id.int==0
            or type(target.collection_revision) is not int or not 1<=target.collection_revision<=9007199254740991):
        raise ValueError('RESTORE_SELECTION_INSTALLATION')
    # Shared pure origin/TLS/key/auth validation; these IDs are validation-only.
    origin,_=validate_fixture_export(RestoreExportTarget(target.origin,target.tenant_id,target.collection_id,
        target.item_id,target.idempotency_key,target.authorization),tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    return origin,selection_request_body(target)


def request_fixture_selection(target,*,tls_context,assert_fixture_scope,check_active):
    """One POST; caller must prove fresh Selection.Create key and saved SQL receipt."""
    origin,raw=validate_fixture_selection(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target)
    guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('POST',f'/api/v1/tenants/{target.tenant_id}/collections/{target.collection_id}/selection-snapshots',
            body=raw,headers={'Authorization':target.authorization,'Idempotency-Key':target.idempotency_key,
                'X-Photo-Client':'native','Content-Type':'application/json','Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();body=response.read(16385)
        if (response.status!=201 or len(body)>16384 or response.getheader('Idempotency-Replayed') is not None
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_SELECTION_REFUSED')
        value=parse_json(body,max_bytes=16384)
        if (not isinstance(value,dict) or set(value)!={'id','digest','item_count','expires_at'}
                or type(value['item_count']) is not int or value['item_count']!=1
                or not isinstance(value['digest'],str) or re.fullmatch('[0-9a-f]{64}',value['digest']) is None):
            raise ValueError('RESTORE_SELECTION_TICKET')
        try:
            snapshot=UUID(value['id']);expires=datetime.fromisoformat(value['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):raise ValueError('RESTORE_SELECTION_TICKET') from None
        if snapshot.int==0 or expires.tzinfo is None or expires<=datetime.now(timezone.utc):
            raise ValueError('RESTORE_SELECTION_TICKET')
        guard();return RestoreSelectionTicket(snapshot,value['digest'],expires,sha256(raw).hexdigest(),sha256(body).hexdigest())
    except (OSError,http.client.HTTPException):raise ValueError('RESTORE_SELECTION_OUTCOME_UNKNOWN') from None
    finally:connection.close()
