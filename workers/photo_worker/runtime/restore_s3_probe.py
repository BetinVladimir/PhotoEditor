"""Exact-version signed S3 purge denial; expired or invalid signatures refuse."""
from dataclasses import dataclass, field
from datetime import datetime, timezone, timedelta
from hashlib import sha256
import http.client
import re
import ssl
from urllib.parse import urlsplit, parse_qsl, quote
import xml.etree.ElementTree as ET
from uuid import UUID
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreS3AccessTarget:
    origin: str
    bucket: str
    tenant_id: UUID
    key: str
    version: str
    control_key: str
    control_version: str
    control_sha256: str
    denied_url: str = field(repr=False)
    control_url: str = field(repr=False)


@dataclass(frozen=True)
class RestoreS3DenialEvidence:
    schema_version: str
    tenant_id: UUID
    bucket: str
    key: str
    version: str
    code: str
    response_sha256: str
    observed_at: datetime


@dataclass(frozen=True)
class RestoreS3ReadableEvidence:
    schema_version: str
    tenant_id: UUID
    bucket: str
    key: str
    version: str
    sha256: str
    size: int
    observed_at: datetime


def observe_signed_s3_readable(target,expected_sha256,expected_size,*,tls_context,assert_fixture_scope,check_active):
    if (not isinstance(expected_sha256,str) or re.fullmatch('[0-9a-f]{64}',expected_sha256) is None
            or type(expected_size) is not int or not 0<=expected_size<=1<<20):
        raise ValueError('RESTORE_S3_BASELINE_INSTALLATION')
    return _observe_signed_s3(target,tls_context=tls_context,assert_fixture_scope=assert_fixture_scope,
        check_active=check_active,baseline=(expected_sha256,expected_size))


def observe_purged_signed_s3(target, *, tls_context, assert_fixture_scope, check_active):
    return _observe_signed_s3(target,tls_context=tls_context,assert_fixture_scope=assert_fixture_scope,
        check_active=check_active)


def _observe_signed_s3(target, *, tls_context, assert_fixture_scope, check_active, baseline=None):
    if (not isinstance(target,RestoreS3AccessTarget) or not isinstance(target.tenant_id,UUID)
            or target.tenant_id.int==0 or not isinstance(tls_context,ssl.SSLContext)
            or tls_context.verify_mode!=ssl.CERT_REQUIRED or not tls_context.check_hostname
            or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not callable(assert_fixture_scope) or not callable(check_active)
            or any(not isinstance(v,str) for v in (target.origin,target.bucket,target.key,target.control_key,target.control_sha256))
            or re.fullmatch('[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]',target.bucket) is None
            or re.fullmatch('[0-9a-f]{64}',target.control_sha256) is None
            or any(not isinstance(v,str) or not v or v=='null' for v in (target.version,target.control_version))):
        raise ValueError('RESTORE_S3_INSTALLATION')
    origin=urlsplit(target.origin)
    if (origin.scheme!='https' or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ('','/') or origin.query or origin.fragment):
        raise ValueError('RESTORE_S3_ORIGIN')
    prefixes=tuple(part+'tenants/'+str(target.tenant_id)+'/' for part in ('','staging/','originals/'))
    if not any(target.key.startswith(prefix) for prefix in prefixes) or target.key==target.control_key:
        raise ValueError('RESTORE_S3_FIXTURE')
    required={'X-Amz-Algorithm','X-Amz-Credential','X-Amz-Date','X-Amz-Expires',
              'X-Amz-SignedHeaders','X-Amz-Signature','versionId'}
    def signed(url,key,version):
        if (not isinstance(url,str) or len(url)>16384 or any(ord(c)<33 or ord(c)>126 for c in url)
                or not isinstance(key,str) or len(key.encode())>1024
                or any(segment in ('','.','..') for segment in key.split('/'))):
            raise ValueError('RESTORE_S3_URL')
        value=urlsplit(url)
        if (value.scheme!=origin.scheme or value.netloc!=origin.netloc or value.username is not None
                or value.fragment or value.path!='/'+target.bucket+'/'+quote(key,safe='/')):
            raise ValueError('RESTORE_S3_URL')
        pairs=parse_qsl(value.query,keep_blank_values=True,strict_parsing=True)
        query=dict(pairs)
        if (len(query)!=len(pairs) or not required<=set(query)
                or set(query)-required-{'X-Amz-Security-Token'}
                or query['versionId']!=version or query['X-Amz-Algorithm']!='AWS4-HMAC-SHA256'
                or query['X-Amz-SignedHeaders']!='host'
                or re.fullmatch('[0-9a-f]{64}',query['X-Amz-Signature']) is None
                or re.fullmatch('[1-9][0-9]{0,5}',query['X-Amz-Expires']) is None):
            raise ValueError('RESTORE_S3_SIGNATURE_SCOPE')
        credential=query['X-Amz-Credential'].split('/')
        if (len(credential)!=5 or not all(credential) or credential[-2:]!=['s3','aws4_request']
                or credential[1]!=query['X-Amz-Date'][:8]):raise ValueError('RESTORE_S3_CREDENTIAL')
        date=datetime.strptime(query['X-Amz-Date'],'%Y%m%dT%H%M%SZ').replace(tzinfo=timezone.utc)
        expires=int(query['X-Amz-Expires'])
        if not 1<=expires<=604800:raise ValueError('RESTORE_S3_EXPIRY')
        return value.path+'?'+value.query,query,date,date+timedelta(seconds=expires)
    denied=signed(target.denied_url,target.key,target.version)
    control=signed(target.control_url,target.control_key,target.control_version)
    if (denied[1]['X-Amz-Credential']!=control[1]['X-Amz-Credential']
            or denied[1].get('X-Amz-Security-Token')!=control[1].get('X-Amz-Security-Token')):
        raise ValueError('RESTORE_S3_CONTROL_PRINCIPAL')
    def guard():
        require_restore_fixture(check_active,assert_fixture_scope,target)
        now=datetime.now(timezone.utc)
        # Keep both links unexpired throughout the three bounded requests.
        if any(date>now or expiry<=now+timedelta(seconds=30) for _,_,date,expiry in (denied,control)):
            raise ValueError('RESTORE_S3_EXPIRED')
    def request(link,maximum):
        guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,context=tls_context,timeout=5)
        try:
            connection.request('GET',link[0],headers={'Accept-Encoding':'identity'})
            response=connection.getresponse();guard();raw=response.read(maximum+1)
            if len(raw)>maximum or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_S3_RESPONSE')
            guard();return response.status,response.getheader('Content-Type',''),response.getheader('x-amz-version-id'),raw
        except (OSError,http.client.HTTPException):raise ValueError('RESTORE_S3_UNAVAILABLE') from None
        finally:connection.close()
    def positive():
        status,_,version,raw=request(control,1<<20)
        if status!=200 or version!=target.control_version or sha256(raw).hexdigest()!=target.control_sha256:
            raise ValueError('RESTORE_S3_CONTROL')
    positive()
    if baseline is not None:
        expected_sha,expected_size=baseline
        status,_,version,raw=request(denied,1<<20)
        if status!=200 or version!=target.version or len(raw)!=expected_size or sha256(raw).hexdigest()!=expected_sha:
            raise ValueError('RESTORE_S3_BASELINE')
        positive();guard()
        return RestoreS3ReadableEvidence('restore-s3-readable/v1',target.tenant_id,target.bucket,
            target.key,target.version,expected_sha,expected_size,datetime.now(timezone.utc))
    status,media,_,raw=request(denied,16384)
    if status!=404 or media.split(';')[0].strip().lower() not in ('application/xml','text/xml'):
        raise ValueError('RESTORE_S3_DENIAL')
    if b'<!DOCTYPE' in raw.upper() or b'<!ENTITY' in raw.upper():raise ValueError('RESTORE_S3_XML')
    try:document=ET.fromstring(raw)
    except ET.ParseError:raise ValueError('RESTORE_S3_XML') from None
    codes=document.findall('Code')
    if document.tag!='Error' or len(codes)!=1 or codes[0].text not in ('NoSuchVersion','NoSuchKey'):
        raise ValueError('RESTORE_S3_DENIAL')
    positive();guard()
    return RestoreS3DenialEvidence('restore-s3-denial/v1',target.tenant_id,target.bucket,
        target.key,target.version,codes[0].text,sha256(raw).hexdigest(),datetime.now(timezone.utc))
