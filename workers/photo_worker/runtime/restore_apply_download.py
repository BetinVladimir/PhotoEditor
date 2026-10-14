"""Download the exact adopted legacy Apply WorkingImage through native API."""
from dataclasses import dataclass
from datetime import datetime,timezone,timedelta
from hashlib import sha256
from urllib.parse import urlsplit,parse_qs
import http.client
from photo_worker.contracts.base import parse_json
from .restore_preview_download import RestorePreviewDownloadTarget,validate_preview_download_installation,signed_url_matches_object_path
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreApplyDownloadEvidence:
    artifact_id: str
    sha256: str
    bytes_verified: int
    api_response_sha256: str


def verify_fixture_apply_download(target,tenant_id,result,*,tls_context,assert_fixture_scope,check_active):
    api,objects=validate_preview_download_installation(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,tenant_id,result)
    def get(u,path,headers,limit):
        guard();connection=http.client.HTTPSConnection(u.hostname,u.port or 443,context=tls_context,timeout=5)
        try:
            connection.request('GET',path,headers=headers)
            response=connection.getresponse();guard()
            if response.status!=200 or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_APPLY_DOWNLOAD_REFUSED')
            chunks=[];size=0
            while True:
                guard();block=response.read(min(65536,limit-size+1))
                if not block:break
                size+=len(block)
                if size>limit:raise ValueError('RESTORE_APPLY_DOWNLOAD_SIZE')
                chunks.append(block)
            return response.getheader('Content-Type','').split(';')[0].strip().lower(),b''.join(chunks)
        except (OSError,http.client.HTTPException):raise ValueError('RESTORE_APPLY_DOWNLOAD_UNAVAILABLE') from None
        finally:connection.close()
    media,raw=get(api,f'/api/v1/tenants/{tenant_id}/runs/{result.registration.run_id}/outputs',
        {'Authorization':target.authorization,'Accept':'application/json','Accept-Encoding':'identity'},65536)
    if media!='application/json':raise ValueError('RESTORE_APPLY_DOWNLOAD_API')
    value=parse_json(raw,max_bytes=65536)
    if not isinstance(value,dict) or set(value)!={'items'} or not isinstance(value['items'],list) or not 1<=len(value['items'])<=256:
        raise ValueError('RESTORE_APPLY_DOWNLOAD_API')
    candidates=[item for item in value['items'] if isinstance(item,dict) and item.get('artifact_id')==str(result.artifact_id)]
    if len(candidates)!=1:raise ValueError('RESTORE_APPLY_DOWNLOAD_OUTPUT')
    expected={str(result.artifact_id):(result,'application/json',1048576)}
    links={}
    for item in candidates:
        if (not isinstance(item,dict) or set(item)!={'artifact_id','role','sha256','bytes','media_type','url','expires_at'}
                or not isinstance(item['artifact_id'],str) or item['artifact_id'] not in expected
                or item['artifact_id'] in links):
            raise ValueError('RESTORE_APPLY_DOWNLOAD_OUTPUT')
        output,expected_media,maximum=expected[item['artifact_id']];obj=output.object
        if (item['role']!='working_image' or item['sha256']!=obj.sha256
                or type(item['bytes']) is not int or item['bytes']!=obj.size or not 1<=obj.size<=maximum
                or item['media_type']!=expected_media or not isinstance(item['url'],str)):
            raise ValueError('RESTORE_APPLY_DOWNLOAD_OUTPUT')
        try:expiry=datetime.fromisoformat(item['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):raise ValueError('RESTORE_APPLY_DOWNLOAD_EXPIRY') from None
        now=datetime.now(timezone.utc)
        if (expiry.tzinfo is None or expiry<=now
                or expiry>now+timedelta(minutes=10,seconds=5)):
            raise ValueError('RESTORE_APPLY_DOWNLOAD_EXPIRY')
        link=urlsplit(item['url'])
        if (link.scheme!='https' or (link.hostname,link.port or 443)!=(objects.hostname,objects.port or 443)
                or link.username is not None or link.password is not None or link.fragment
                or not signed_url_matches_object_path(link.path,obj)
                or any(ord(c)<33 or ord(c)>126 for c in item['url'])
                or parse_qs(link.query).get('versionId')!=[obj.version]):
            raise ValueError('RESTORE_APPLY_DOWNLOAD_LINK')
        links[item['artifact_id']]=(link,output,expected_media,expiry)
    # Only the exact adopted WorkingImage is downloaded; other task outputs
    # are not a delivery claim for this probe.
    for link,output,expected_media,expiry in links.values():
        obj=output.object
        if datetime.now(timezone.utc)>=expiry:raise ValueError('RESTORE_APPLY_DOWNLOAD_EXPIRY')
        media,body=get(link,link.path+'?'+link.query,{'Accept':expected_media,'Accept-Encoding':'identity'},obj.size)
        if media!=expected_media or len(body)!=obj.size or sha256(body).hexdigest()!=obj.sha256:
            raise ValueError('RESTORE_APPLY_DOWNLOAD_SHA')
    guard();return RestoreApplyDownloadEvidence(str(result.artifact_id),result.object.sha256,
        result.object.size,sha256(raw).hexdigest())
