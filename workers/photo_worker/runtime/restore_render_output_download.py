"""Download the reviewed render presentation from the completed child run."""
from dataclasses import dataclass
from datetime import datetime,timezone,timedelta
from hashlib import sha256
import http.client
from urllib.parse import urlsplit,unquote,parse_qsl
from uuid import UUID

from photo_worker.contracts.base import parse_json
from .restore_batch_adoption import RestoreBatchAdoptionEvidence
from .restore_render_preview_catalogue import (
    RestoreRenderPreviewObjects,observe_fixture_after_preview_objects)
from .restore_render_review import RestoreRenderReviewEvidence
from .restore_render_terminal import RestoreRenderTerminalEvidence,observe_fixture_render_terminal
from .restore_preview_download import validate_preview_download_installation
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderOutputDownload:
    child_run_id: UUID
    derivative_sha256: str
    derivative_bytes: int
    manifest_sha256: str
    manifest_bytes: int
    api_sha256: str


def download_fixture_render_output(connection,target,tenant_id,actor_id,adoption,review,
                                   terminal,preview,*,tls_context,assert_installed_fixture,
                                   check_active):
    """Require exact accepted SQL receipt, two public links and physical bytes."""
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0
            or not isinstance(actor_id,UUID) or actor_id.int==0
            or not isinstance(adoption,RestoreBatchAdoptionEvidence)
            or not isinstance(review,RestoreRenderReviewEvidence)
            or not isinstance(terminal,RestoreRenderTerminalEvidence)
            or not isinstance(preview,RestoreRenderPreviewObjects)
            or review.review_id!=terminal.review_id
            or review.candidate!=preview.source
            or preview.review_id!=review.review_id
            or terminal.child_run_id!=adoption.child_run_id
            or terminal.database!=adoption.database or preview.database!=adoption.database
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_OUTPUT_INSTALLATION')
    api,objects=validate_preview_download_installation(target,tls_context=tls_context,
        assert_fixture_scope=assert_installed_fixture,check_active=check_active)
    def guard():require_restore_fixture(check_active,assert_installed_fixture)
    def current():
        return observe_fixture_after_preview_objects(connection,tenant_id,review.review_id,
            review.task_id,review.candidate,preview.variant,preview.profile_hash,
            (preview.manifest_object.bucket,preview.image_object.bucket),
            expected_database=adoption.database,accepted_revision=review.revision,
            assert_fixture_scope=guard,check_active=guard)
    def completed():
        return observe_fixture_render_terminal(connection,tenant_id,actor_id,adoption,review,
            assert_fixture_scope=guard,check_active=guard)
    guard()
    if current()!=preview or completed()!=terminal:
        raise ValueError('RESTORE_RENDER_OUTPUT_CHANGED')
    def get(origin,path,headers,limit):
        guard();client=http.client.HTTPSConnection(origin.hostname,origin.port or 443,
                                                   context=tls_context,timeout=5)
        try:
            client.request('GET',path,headers=headers)
            response=client.getresponse();guard()
            if response.status!=200 or response.getheader('Content-Encoding') not in (None,'identity'):
                raise ValueError('RESTORE_RENDER_OUTPUT_REFUSED')
            chunks=[];received=0
            while True:
                guard();block=response.read(min(65536,limit-received+1))
                if not block:break
                received+=len(block)
                if received>limit:raise ValueError('RESTORE_RENDER_OUTPUT_SIZE')
                chunks.append(block)
            return response.getheader('Content-Type','').split(';')[0].strip().lower(),b''.join(chunks)
        except (OSError,http.client.HTTPException):
            raise ValueError('RESTORE_RENDER_OUTPUT_UNAVAILABLE') from None
        finally:client.close()
    media,raw=get(api,f'/api/v1/tenants/{tenant_id}/runs/{adoption.child_run_id}/outputs',
        {'Authorization':target.authorization,'Accept':'application/json','Accept-Encoding':'identity'},65536)
    if media!='application/json':raise ValueError('RESTORE_RENDER_OUTPUT_API')
    value=parse_json(raw,max_bytes=65536)
    if not isinstance(value,dict) or set(value)!={'items'} or not isinstance(value['items'],list):
        raise ValueError('RESTORE_RENDER_OUTPUT_API')
    expected={str(preview.manifest.artifact_id):('derivative_manifest',preview.manifest,preview.manifest_object,'application/json'),
              str(preview.image.artifact_id):('derivative',preview.image,preview.image_object,preview.image_media_type)}
    if len(expected)!=2 or len(value['items'])!=2:raise ValueError('RESTORE_RENDER_OUTPUT_SET')
    links={}
    for item in value['items']:
        if (not isinstance(item,dict) or set(item)!={'artifact_id','role','sha256','bytes','media_type','url','expires_at'}
                or item.get('artifact_id') not in expected or item['artifact_id'] in links):
            raise ValueError('RESTORE_RENDER_OUTPUT_ITEM')
        role,ref,obj,expected_media=expected[item['artifact_id']]
        if (item['role']!=role or item['sha256']!=ref.sha256 or item['sha256']!=obj.sha256
                or type(item['bytes']) is not int or item['bytes']!=obj.size
                or item['media_type']!=expected_media or not isinstance(item['url'],str)):
            raise ValueError('RESTORE_RENDER_OUTPUT_ITEM')
        try:expiry=datetime.fromisoformat(item['expires_at'].replace('Z','+00:00'))
        except (ValueError,TypeError,AttributeError):
            raise ValueError('RESTORE_RENDER_OUTPUT_EXPIRY') from None
        now=datetime.now(timezone.utc)
        if expiry.tzinfo is None or not now<expiry<=now+timedelta(minutes=11):
            raise ValueError('RESTORE_RENDER_OUTPUT_EXPIRY')
        link=urlsplit(item['url'])
        if (link.scheme!='https' or (link.hostname,link.port or 443)!=(objects.hostname,objects.port or 443)
                or link.username is not None or link.password is not None or link.fragment
                or unquote(link.path)!='/'+obj.bucket+'/'+obj.key
                or [v for k,v in parse_qsl(link.query,keep_blank_values=True) if k=='versionId']!=[obj.version]
                or any(ord(c)<33 or ord(c)>126 for c in item['url'])):
            raise ValueError('RESTORE_RENDER_OUTPUT_LINK')
        links[item['artifact_id']]=(link,obj,expected_media)
    if set(links)!=set(expected):raise ValueError('RESTORE_RENDER_OUTPUT_SET')
    for link,obj,expected_media in links.values():
        actual,body=get(objects,link.path+'?'+link.query,
            {'Accept':expected_media,'Accept-Encoding':'identity'},obj.size)
        if actual!=expected_media or len(body)!=obj.size or sha256(body).hexdigest()!=obj.sha256:
            raise ValueError('RESTORE_RENDER_OUTPUT_SHA')
    if current()!=preview or completed()!=terminal:
        raise ValueError('RESTORE_RENDER_OUTPUT_CHANGED')
    guard()
    return RestoreRenderOutputDownload(adoption.child_run_id,preview.image.sha256,
        preview.image_object.size,preview.manifest.sha256,preview.manifest_object.size,
        sha256(raw).hexdigest())
