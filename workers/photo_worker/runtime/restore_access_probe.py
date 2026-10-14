"""Concrete bounded HTTPS JSON access denial with a live positive control."""
from dataclasses import dataclass, field
from datetime import datetime, timezone
from hashlib import sha256
import http.client
import ssl
import re
from urllib.parse import urlsplit
from photo_worker.contracts.base import parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreAccessTarget:
    origin: str
    denied_path: str = field(repr=False)
    control_path: str = field(repr=False)
    authorization: str = field(repr=False)
    expected_status: int
    expected_code: str
    control_sha256: str


@dataclass(frozen=True)
class RestoreAccessDenialEvidence:
    schema_version: str
    status: int
    code: str
    response_sha256: str
    observed_at: datetime


DENIALS = {(403,'FORBIDDEN'),(404,'NOT_FOUND'),(409,'DOWNLOAD_REVOKED'),
           (403,'LEASE_LOST'),(404,'ARTIFACT_MISSING'),(409,'PROFILE_CONSENT_REVOKED')}


@dataclass(frozen=True)
class RestoreAccessReadableEvidence:
    schema_version: str
    response_sha256: str
    observed_at: datetime


def observe_restore_access_readable(target, expected_sha256, *, tls_context, assert_fixture_scope, check_active):
    if not isinstance(expected_sha256,str) or re.fullmatch('[0-9a-f]{64}',expected_sha256) is None:
        raise ValueError('RESTORE_ACCESS_BASELINE_PIN')
    return _observe_restore_access(target,tls_context=tls_context,assert_fixture_scope=assert_fixture_scope,
        check_active=check_active,readable_sha256=expected_sha256)


def observe_restore_access_denied(target, *, tls_context, assert_fixture_scope, check_active):
    return _observe_restore_access(target,tls_context=tls_context,assert_fixture_scope=assert_fixture_scope,
        check_active=check_active)


def _observe_restore_access(target, *, tls_context, assert_fixture_scope, check_active, readable_sha256=None):
    """No redirects/retries/proxy/env credentials. 401/5xx never prove denial.

    Operator installs exact fixture/control routes and live credentials. Scope
    guard must bind both routes to this isolated recovery/fixture. Positive
    control is expected to return200 with the same principal before and after.
    Direct S3 XML responses need a separate probe and are never accepted here.
    """
    if (not isinstance(target,RestoreAccessTarget) or not isinstance(tls_context,ssl.SSLContext)
            or tls_context.verify_mode!=ssl.CERT_REQUIRED or not tls_context.check_hostname
            or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)
            or (target.expected_status,target.expected_code) not in DENIALS
            or not isinstance(target.control_sha256,str) or re.fullmatch('[0-9a-f]{64}',target.control_sha256) is None
            or not isinstance(target.authorization,str) or not 1<=len(target.authorization)<=8192
            or any(c in target.authorization for c in '\r\n')):
        raise ValueError('RESTORE_ACCESS_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_ACCESS_ORIGIN')
    for path in (target.denied_path,target.control_path):
        if (not isinstance(path,str) or not path.startswith('/') or path.startswith('//')
                or len(path)>8192 or any(ord(c)<33 or ord(c)>126 for c in path)
                or urlsplit(path).fragment):raise ValueError('RESTORE_ACCESS_PATH')
    if target.denied_path==target.control_path:raise ValueError('RESTORE_ACCESS_CONTROL')
    def guard():
        require_restore_fixture(check_active,assert_fixture_scope,target)
    def request(path):
        guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,
            context=tls_context,timeout=5)
        try:
            connection.request('GET',path,headers={'Authorization':target.authorization,
                'Accept':'application/json','Accept-Encoding':'identity','Cache-Control':'no-cache'})
            response=connection.getresponse();guard()
            raw=response.read(16385)
            if len(raw)>16384 or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_ACCESS_RESPONSE')
            guard();return response.status,response.getheader('Content-Type',''),raw
        except (OSError,http.client.HTTPException):
            raise ValueError('RESTORE_ACCESS_UNAVAILABLE') from None
        finally:connection.close()
    def control():
        status,media,raw=request(target.control_path)
        if (status!=200 or media.split(';')[0].strip().lower()!='application/json'
                or sha256(raw).hexdigest()!=target.control_sha256):
            raise ValueError('RESTORE_ACCESS_CONTROL')
        if not isinstance(parse_json(raw,max_bytes=16384),dict):raise ValueError('RESTORE_ACCESS_CONTROL')
    control()
    status,media,raw=request(target.denied_path)
    if readable_sha256 is not None:
        if (status!=200 or media.split(';')[0].strip().lower()!='application/json'
                or sha256(raw).hexdigest()!=readable_sha256
                or not isinstance(parse_json(raw,max_bytes=16384),dict)):
            raise ValueError('RESTORE_ACCESS_BASELINE')
        control();guard()
        return RestoreAccessReadableEvidence('restore-access-readable/v1',readable_sha256,datetime.now(timezone.utc))
    if status!=target.expected_status or media.split(';')[0].strip().lower()!='application/json':
        raise ValueError('RESTORE_ACCESS_DENIAL')
    value=parse_json(raw,max_bytes=16384)
    if not isinstance(value,dict) or value.get('code')!=target.expected_code:
        raise ValueError('RESTORE_ACCESS_DENIAL')
    control()
    guard()
    return RestoreAccessDenialEvidence('restore-access-denial/v1',status,target.expected_code,
                                      sha256(raw).hexdigest(),datetime.now(timezone.utc))
