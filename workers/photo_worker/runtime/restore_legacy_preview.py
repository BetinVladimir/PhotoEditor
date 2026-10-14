"""Native legacy preview admission for an isolated restore fixture."""
from dataclasses import dataclass,field
from hashlib import sha256
from uuid import UUID
from urllib.parse import urlsplit
import ssl
import http.client
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreLegacyPreviewTarget:
    origin: str
    tenant_id: UUID
    asset_version_id: UUID
    draft_revision: int
    params_raw: bytes=field(repr=False)
    idempotency_key: str=field(repr=False)
    authorization: str=field(repr=False)
    style_version_id: UUID|None=None
    roi: tuple[int,int,int,int]|None=None
    scale: str='proxy'


@dataclass(frozen=True)
class RestoreLegacyPreviewTicket:
    run_id: UUID
    request_sha256: str
    response_sha256: str


def legacy_preview_request_body(target):
    if (type(target.params_raw) is not bytes or not 2<=len(target.params_raw)<=512*1024
            or type(target.draft_revision) is not int or not 0<=target.draft_revision<9007199254740991
            or target.scale not in ('proxy','full')
            or target.style_version_id is not None and
                (not isinstance(target.style_version_id,UUID) or target.style_version_id.int==0)
            or target.roi is not None and (type(target.roi) is not tuple or len(target.roi)!=4
                or any(type(v) is not int or not 0<=v<=2147483647 for v in target.roi)
                or target.roi[2]<1 or target.roi[3]<1)):
        raise ValueError('RESTORE_LEGACY_PREVIEW_PARAMS')
    params=parse_json(target.params_raw,max_bytes=512*1024)
    if not isinstance(params,dict) or not params or canonical_bytes(params)!=target.params_raw:
        raise ValueError('RESTORE_LEGACY_PREVIEW_PARAMS')
    modules=params
    if 'schema_version' in params:
        if type(params['schema_version']) is not int or params['schema_version']!=1:
            raise ValueError('RESTORE_LEGACY_PREVIEW_PARAMS')
        modules=params.get('modules')
    if (not isinstance(modules,dict) or not modules or
            not set(modules)<= {'strength','global','local','beauty','defects','geometry','extensions'}):
        raise ValueError('RESTORE_LEGACY_PREVIEW_PARAMS')
    body={'asset_version_id':str(target.asset_version_id),'draft_revision':target.draft_revision,
        'params':params,'scale':target.scale,'generation':False}
    if target.style_version_id is not None:body['style_version_id']=str(target.style_version_id)
    if target.roi is not None:body['roi']=list(target.roi)
    return canonical_bytes(body)


def validate_fixture_legacy_preview(target,*,tls_context,assert_fixture_scope,check_active):
    """Pure installation validation; no connection or fixture mutation."""
    if (not isinstance(target,RestoreLegacyPreviewTarget)
            or any(not isinstance(v,UUID) or v.int==0 for v in
                (target.tenant_id,target.asset_version_id))
            or not isinstance(target.idempotency_key,str) or not 1<=len(target.idempotency_key)<=128
            or any(not 33<=ord(c)<=126 for c in target.idempotency_key)
            or not isinstance(target.authorization,str) or not target.authorization.startswith('Bearer ')
            or not 8<=len(target.authorization)<=8192 or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context,ssl.SSLContext) or tls_context.verify_mode!=ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_PREVIEW_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None or origin.password is not None
            or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_LEGACY_PREVIEW_ORIGIN')
    raw=legacy_preview_request_body(target)
    # Resolve port before any mutation; urlsplit otherwise defers validation.
    if origin.port is not None and not 1<=origin.port<=65535:
        raise ValueError('RESTORE_LEGACY_PREVIEW_ORIGIN')
    return origin,raw


def request_fixture_legacy_preview(target,*,tls_context,assert_fixture_scope,check_active):
    """No retries/replays/redirects; ticket alone is never ready preview proof."""
    origin,raw=validate_fixture_legacy_preview(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target)
    guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('POST',f'/api/v1/tenants/{target.tenant_id}/previews',
            body=raw,headers={'Authorization':target.authorization,'Idempotency-Key':target.idempotency_key,
                'X-Photo-Client':'native','Content-Type':'application/json','Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();body=response.read(16385)
        if (response.status!=202 or len(body)>16384 or response.getheader('Idempotency-Replayed') is not None
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_LEGACY_PREVIEW_REFUSED')
        value=parse_json(body,max_bytes=16384)
        if not isinstance(value,dict) or set(value)!={'run_id','status'} or value['status']!='queued':
            raise ValueError('RESTORE_LEGACY_PREVIEW_TICKET')
        run=UUID(value['run_id'])
        if run.int==0:
            raise ValueError('RESTORE_LEGACY_PREVIEW_TICKET')
        guard();return RestoreLegacyPreviewTicket(run,sha256(raw).hexdigest(),sha256(body).hexdigest())
    except (OSError,http.client.HTTPException):
        raise ValueError('RESTORE_LEGACY_PREVIEW_OUTCOME_UNKNOWN') from None
    finally:connection.close()
