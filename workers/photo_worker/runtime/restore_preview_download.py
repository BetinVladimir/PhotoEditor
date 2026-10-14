"""Native outputs API and pinned signed-object download; no credentials in evidence."""
from dataclasses import dataclass,field
from datetime import datetime,timezone,timedelta
from hashlib import sha256
from urllib.parse import urlsplit,parse_qs,unquote
import http.client
import ssl
from photo_worker.contracts.base import parse_json
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestorePreviewDownloadTarget:
    api_origin: str
    object_origin: str
    authorization: str=field(repr=False)


@dataclass(frozen=True)
class RestorePreviewDownloadEvidence:
    artifact_id: str
    sha256: str
    bytes_verified: int
    api_response_sha256: str
    manifest_sha256: str
    manifest_bytes_verified: int


def validate_preview_download_installation(target,*,tls_context,assert_fixture_scope,check_active):
    if (not isinstance(target,RestorePreviewDownloadTarget) or not callable(assert_fixture_scope)
            or not callable(check_active) or not isinstance(tls_context,ssl.SSLContext)
            or tls_context.verify_mode!=ssl.CERT_REQUIRED or not tls_context.check_hostname
            or tls_context.minimum_version<ssl.TLSVersion.TLSv1_2
            or not isinstance(target.authorization,str) or not target.authorization.startswith('Bearer ')
            or not 8<=len(target.authorization)<=8192 or any(c in target.authorization for c in '\r\n')):
        raise ValueError('RESTORE_PREVIEW_DOWNLOAD_INSTALLATION')
    def origin(value):
        u=urlsplit(value)
        if (u.scheme!='https' or not u.hostname or u.username is not None or u.password is not None
                or u.path not in ('','/') or u.query or u.fragment):
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_ORIGIN')
        return u
    api=origin(target.api_origin);objects=origin(target.object_origin)
    for value in (api,objects):
        if value.port is not None and not 1<=value.port<=65535:
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_ORIGIN')
    return api,objects


def signed_url_matches_object_path(path,obj):
    """Accept path-style or bucket-hosted URLs for exactly the catalogue key."""
    try:
        decoded=unquote(path,encoding='utf-8',errors='strict')
        return (isinstance(obj.bucket,str) and obj.bucket!=''
                and isinstance(obj.key,str) and obj.key!=''
                and decoded in ('/'+obj.bucket+'/'+obj.key,'/'+obj.key))
    except (UnicodeError,AttributeError):
        return False


def verify_fixture_preview_download(target,tenant_id,result,*,tls_context,assert_fixture_scope,check_active):
    api,objects=validate_preview_download_installation(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,tenant_id,result)
    def get(u,path,headers,limit):
        guard();connection=http.client.HTTPSConnection(u.hostname,u.port or 443,context=tls_context,timeout=5)
        try:
            connection.request('GET',path,headers=headers)
            response=connection.getresponse();guard()
            if response.status!=200 or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_PREVIEW_DOWNLOAD_REFUSED')
            chunks=[];size=0
            while True:
                guard();block=response.read(min(65536,limit-size+1))
                if not block:break
                size+=len(block)
                if size>limit:raise ValueError('RESTORE_PREVIEW_DOWNLOAD_SIZE')
                chunks.append(block)
            return response.getheader('Content-Type','').split(';')[0].strip().lower(),b''.join(chunks)
        except (OSError,http.client.HTTPException):raise ValueError('RESTORE_PREVIEW_DOWNLOAD_UNAVAILABLE') from None
        finally:connection.close()
    media,raw=get(api,f'/api/v1/tenants/{tenant_id}/runs/{result.registration.run_id}/outputs',
        {'Authorization':target.authorization,'Accept':'application/json','Accept-Encoding':'identity'},65536)
    if media!='application/json':raise ValueError('RESTORE_PREVIEW_DOWNLOAD_API')
    value=parse_json(raw,max_bytes=65536)
    if not isinstance(value,dict) or set(value)!={'items'} or not isinstance(value['items'],list) or len(value['items'])!=2:
        raise ValueError('RESTORE_PREVIEW_DOWNLOAD_API')
    expected={str(result.derivative.artifact_id):(result.derivative,'image/jpeg',67108864),
              str(result.manifest.artifact_id):(result.manifest,'application/json',1048576)}
    if len(expected)!=2:raise ValueError('RESTORE_PREVIEW_DOWNLOAD_OUTPUT')
    links={}
    for item in value['items']:
        if (not isinstance(item,dict) or set(item)!={'artifact_id','role','sha256','bytes','media_type','url','expires_at'}
                or not isinstance(item['artifact_id'],str) or item['artifact_id'] not in expected
                or item['artifact_id'] in links):
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_OUTPUT')
        output,expected_media,maximum=expected[item['artifact_id']];obj=output.object
        if (item['role']!=output.role or item['sha256']!=obj.sha256
                or type(item['bytes']) is not int or item['bytes']!=obj.size or not 1<=obj.size<=maximum
                or item['media_type']!=expected_media or not isinstance(item['url'],str)):
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_OUTPUT')
        try:expiry=datetime.fromisoformat(item['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):raise ValueError('RESTORE_PREVIEW_DOWNLOAD_EXPIRY') from None
        now=datetime.now(timezone.utc)
        if (expiry.tzinfo is None or expiry<=now
                or expiry>now+timedelta(minutes=10,seconds=5)):
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_EXPIRY')
        link=urlsplit(item['url'])
        if (link.scheme!='https' or (link.hostname,link.port or 443)!=(objects.hostname,objects.port or 443)
                or link.username is not None or link.password is not None or link.fragment
                or not signed_url_matches_object_path(link.path,obj)
                or any(ord(c)<33 or ord(c)>126 for c in item['url'])
                or parse_qs(link.query).get('versionId')!=[obj.version]):
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_LINK')
        links[item['artifact_id']]=(link,output,expected_media,expiry)
    # Validate both links before the first object request; never partially accept
    # a correct JPEG alongside a substituted/foreign manifest.
    for link,output,expected_media,expiry in links.values():
        obj=output.object
        if datetime.now(timezone.utc)>=expiry:raise ValueError('RESTORE_PREVIEW_DOWNLOAD_EXPIRY')
        media,body=get(link,link.path+'?'+link.query,{'Accept':expected_media,'Accept-Encoding':'identity'},obj.size)
        if media!=expected_media or len(body)!=obj.size or sha256(body).hexdigest()!=obj.sha256:
            raise ValueError('RESTORE_PREVIEW_DOWNLOAD_SHA')
    guard();return RestorePreviewDownloadEvidence(str(result.derivative.artifact_id),result.derivative.object.sha256,
        result.derivative.object.size,sha256(raw).hexdigest(),result.manifest.object.sha256,result.manifest.object.size)
