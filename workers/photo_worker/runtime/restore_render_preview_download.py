"""Native review-preview API and exact signed-version byte verification."""
from dataclasses import dataclass
from datetime import datetime,timezone,timedelta
from hashlib import sha256
import http.client
from urllib.parse import urlsplit,unquote,parse_qsl
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,parse_json
from .restore_inventory import RestoreObject,_validate
from .restore_preview_download import RestorePreviewDownloadTarget,validate_preview_download_installation
from .restore_render_preview_bytes import verify_render_preview_bytes
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderPreviewDownload:
    verified: object
    api_sha256: str
    manifest_bytes: int
    image_bytes: int


def download_fixture_render_preview(target,tenant_id,review_id,side,variant,*,
                                    source,source_width,source_height,source_alpha,
                                    manifest_ref,manifest_object,image_ref,image_object,
                                    srgb_profile,tls_context,assert_fixture_scope,check_active):
    """Both physical versions must come from an independent scoped catalogue."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(review_id,UUID) or review_id.int==0
            or side not in ('before','after') or variant not in ('preview1600','preview2560')
            or not isinstance(source,ArtifactRef)
            or not isinstance(manifest_ref,ArtifactRef) or not isinstance(image_ref,ArtifactRef)
            or not isinstance(manifest_object,RestoreObject)
            or not isinstance(image_object,RestoreObject)
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_INSTALLATION')
    api,objects=validate_preview_download_installation(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    for ref,obj,maximum in ((manifest_ref,manifest_object,1048576),
                            (image_ref,image_object,52*1048576)):
        _validate(obj)
        if (ref.artifact_id.int==0 or ref.sha256!=obj.sha256
                or not 2<=obj.size<=maximum):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_OBJECT')
    if len({source.artifact_id,manifest_ref.artifact_id,image_ref.artifact_id})!=3:
        raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_ALIAS')
    def guard():require_restore_fixture(check_active,assert_fixture_scope)
    def get(origin,path,headers,limit):
        guard();connection=http.client.HTTPSConnection(origin.hostname,origin.port or 443,
                                                       context=tls_context,timeout=5)
        try:
            connection.request('GET',path,headers=headers)
            response=connection.getresponse();guard()
            if response.status!=200 or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_REFUSED')
            parts=[];received=0
            while True:
                guard();part=response.read(min(65536,limit-received+1))
                if not part:break
                received+=len(part)
                if received>limit:raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_LIMIT')
                parts.append(part)
            return response.getheader('Content-Type','').split(';')[0].strip().lower(),b''.join(parts)
        except (OSError,http.client.HTTPException):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_UNAVAILABLE') from None
        finally:connection.close()
    api_path=f'/api/v1/tenants/{tenant_id}/reviews/{review_id}/previews/{side}/{variant}'
    api_media,api_raw=get(api,api_path,{'Authorization':target.authorization,
        'Accept':'application/json','Accept-Encoding':'identity'},65536)
    if api_media!='application/json':raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_API')
    value=parse_json(api_raw,max_bytes=65536)
    if not isinstance(value,dict):raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_API')
    prepared=[]
    for name,ref,obj,media in (
            ('manifest',manifest_ref,manifest_object,'application/json'),
            ('image',image_ref,image_object,'image/webp' if source_alpha else 'image/jpeg')):
        item=value.get(name)
        if (not isinstance(item,dict) or set(item)!={'artifact','bytes','media_type','url','expires_at'}
                or item['artifact']!={'artifact_id':str(ref.artifact_id),'sha256':ref.sha256}
                or type(item['bytes']) is not int or item['bytes']!=obj.size
                or item['media_type']!=media or not isinstance(item['url'],str)):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_LINK')
        try:expiry=datetime.fromisoformat(item['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_EXPIRY') from None
        now=datetime.now(timezone.utc)
        if expiry.tzinfo is None or not now<expiry<=now+timedelta(minutes=6):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_EXPIRY')
        link=urlsplit(item['url'])
        if (link.scheme!='https' or (link.hostname,link.port or 443)!=(objects.hostname,objects.port or 443)
                or link.username is not None or link.password is not None or link.fragment
                or unquote(link.path)!='/'+obj.bucket+'/'+obj.key
                or any(ord(c)<33 or ord(c)>126 for c in item['url'])):
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_LINK')
        query=parse_qsl(link.query,keep_blank_values=True)
        if [v for k,v in query if k=='versionId']!=[obj.version]:
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_VERSION')
        prepared.append((link,obj,media))
    raw_parts=[]
    for link,obj,media in prepared:
        actual,raw=get(objects,link.path+'?'+link.query,
            {'Accept':media,'Accept-Encoding':'identity'},obj.size)
        if actual!=media or len(raw)!=obj.size or sha256(raw).hexdigest()!=obj.sha256:
            raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_SHA')
        raw_parts.append(raw)
    verified=verify_render_preview_bytes(api_raw,*raw_parts,review_id=review_id,side=side,
        source=source,source_width=source_width,source_height=source_height,
        source_alpha=source_alpha,variant=variant,srgb_profile=srgb_profile,
        check_active=guard)
    if verified.manifest!=manifest_ref or verified.image!=image_ref:
        raise ValueError('RESTORE_RENDER_PREVIEW_DOWNLOAD_CHANGED')
    guard()
    return RestoreRenderPreviewDownload(verified,sha256(api_raw).hexdigest(),
                                        len(raw_parts[0]),len(raw_parts[1]))
