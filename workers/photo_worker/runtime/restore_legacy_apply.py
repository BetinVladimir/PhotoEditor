"""One-item native legacy Apply admission for an isolated restore fixture."""
from dataclasses import dataclass,field
from hashlib import sha256
from uuid import UUID
from urllib.parse import urlsplit
import ssl
import http.client
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyApplyTarget:
    origin: str
    tenant_id: UUID
    collection_id: UUID
    item_id: UUID
    style_version_id: UUID
    source_revision: int
    idempotency_key: str=field(repr=False)
    authorization: str=field(repr=False)


@dataclass(frozen=True)
class RestoreLegacyApplyTicket:
    run_id: UUID
    output_collection_id: UUID
    request_sha256: str
    response_sha256: str


def legacy_apply_request_body(target):
    return canonical_bytes({'source_revision':target.source_revision,'style_version_id':str(target.style_version_id),
        'scope':'selected','item_ids':[str(target.item_id)]})


def validate_fixture_legacy_apply(target,*,tls_context,assert_fixture_scope,check_active):
    """Pure installation and native request validation, without IO."""
    if (not isinstance(target,RestoreLegacyApplyTarget)
            or any(not isinstance(v,UUID) or v.int==0 for v in
                (target.tenant_id,target.collection_id,target.item_id,target.style_version_id))
            or type(target.source_revision) is not int or not 1<=target.source_revision<9007199254740991
            or not isinstance(target.idempotency_key,str) or not 1<=len(target.idempotency_key)<=128
            or any(not 33<=ord(c)<=126 for c in target.idempotency_key)
            or not isinstance(target.authorization,str) or not target.authorization.startswith('Bearer ')
            or not 8<=len(target.authorization)<=8192 or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context,ssl.SSLContext) or tls_context.verify_mode!=ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_APPLY_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None or origin.password is not None
            or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_LEGACY_APPLY_ORIGIN')
    raw=legacy_apply_request_body(target)
    if origin.port is not None and not 1<=origin.port<=65535:
        raise ValueError('RESTORE_LEGACY_APPLY_ORIGIN')
    return origin,raw


def request_fixture_legacy_apply(target,*,tls_context,assert_fixture_scope,check_active):
    """No retries/replays/redirects; ticket alone is never committed Apply proof."""
    origin,raw=validate_fixture_legacy_apply(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target)
    guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('POST',f'/api/v1/tenants/{target.tenant_id}/collections/{target.collection_id}/apply-profile',
            body=raw,headers={'Authorization':target.authorization,'Idempotency-Key':target.idempotency_key,
                'X-Photo-Client':'native','Content-Type':'application/json','Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();body=response.read(16385)
        if (response.status!=202 or len(body)>16384 or response.getheader('Idempotency-Replayed') is not None
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_LEGACY_APPLY_REFUSED')
        value=parse_json(body,max_bytes=16384)
        if not isinstance(value,dict) or set(value)!={'run_id','output_collection_id','status'} or value['status']!='queued':
            raise ValueError('RESTORE_LEGACY_APPLY_TICKET')
        run=UUID(value['run_id']);output=UUID(value['output_collection_id'])
        if run.int==0 or output.int==0 or output==target.collection_id:
            raise ValueError('RESTORE_LEGACY_APPLY_TICKET')
        guard();return RestoreLegacyApplyTicket(run,output,sha256(raw).hexdigest(),sha256(body).hexdigest())
    except (OSError,http.client.HTTPException):
        raise ValueError('RESTORE_LEGACY_APPLY_OUTCOME_UNKNOWN') from None
    finally:connection.close()
