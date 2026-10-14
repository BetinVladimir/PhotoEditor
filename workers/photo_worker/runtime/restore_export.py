"""Native legacy download-job admission for an isolated restore fixture."""
from dataclasses import dataclass,field
from hashlib import sha256
from uuid import UUID
from urllib.parse import urlsplit
import ssl
import http.client
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreExportTarget:
    origin: str
    tenant_id: UUID
    selection_snapshot_id: UUID
    preset_id: UUID
    idempotency_key: str=field(repr=False)
    authorization: str=field(repr=False)


@dataclass(frozen=True)
class RestoreExportTicket:
    download_job_id: UUID
    request_sha256: str
    response_sha256: str


def export_request_body(target):
    return canonical_bytes({'selection_snapshot_id':str(target.selection_snapshot_id),'preset_id':str(target.preset_id)})


def validate_fixture_export(target,*,tls_context,assert_fixture_scope,check_active):
    """Pure installation and native request validation, without IO."""
    if (not isinstance(target,RestoreExportTarget)
            or any(not isinstance(v,UUID) or v.int==0 for v in
                (target.tenant_id,target.selection_snapshot_id,target.preset_id))
            or not isinstance(target.idempotency_key,str) or not 1<=len(target.idempotency_key)<=128
            or any(not 33<=ord(c)<=126 for c in target.idempotency_key)
            or not isinstance(target.authorization,str) or not target.authorization.startswith('Bearer ')
            or not 8<=len(target.authorization)<=8192 or any(c in target.authorization for c in '\r\n')
            or not isinstance(tls_context,ssl.SSLContext) or tls_context.verify_mode!=ssl.CERT_REQUIRED
            or not tls_context.check_hostname or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_EXPORT_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None or origin.password is not None
            or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_EXPORT_ORIGIN')
    raw=export_request_body(target)
    if origin.port is not None and not 1<=origin.port<=65535:
        raise ValueError('RESTORE_EXPORT_ORIGIN')
    return origin,raw


def request_fixture_export(target,*,tls_context,assert_fixture_scope,check_active):
    """No retries/replays/redirects; ticket alone is never completed export proof. Backend omits replay header; caller must preflight saved keys in SQL."""
    origin,raw=validate_fixture_export(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target)
    guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('POST',f'/api/v1/tenants/{target.tenant_id}/download-jobs',
            body=raw,headers={'Authorization':target.authorization,'Idempotency-Key':target.idempotency_key,
                'X-Photo-Client':'native','Content-Type':'application/json','Accept':'application/json','Accept-Encoding':'identity'})
        response=connection.getresponse();guard();body=response.read(16385)
        if (response.status!=202 or len(body)>16384 or response.getheader('Idempotency-Replayed') is not None
                or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_EXPORT_REFUSED')
        value=parse_json(body,max_bytes=16384)
        if not isinstance(value,dict) or set(value)!={'download_job_id','status'} or value['status']!='queued':
            raise ValueError('RESTORE_EXPORT_TICKET')
        job=UUID(value['download_job_id'])
        if job.int==0:
            raise ValueError('RESTORE_EXPORT_TICKET')
        guard();return RestoreExportTicket(job,sha256(raw).hexdigest(),sha256(body).hexdigest())
    except (OSError,http.client.HTTPException):
        raise ValueError('RESTORE_EXPORT_OUTCOME_UNKNOWN') from None
    finally:connection.close()
