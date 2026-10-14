"""Redeem owner intent and stream the exact final volume; never retain URLs/tokens."""
from dataclasses import dataclass
from datetime import datetime,timezone
from hashlib import sha256
from urllib.parse import urlsplit,parse_qs,unquote
import http.client
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_intent_lifecycle import RestoreRegisteredIntent
from .restore_export_volume import RestoreExportVolume
from .restore_preview_download import validate_preview_download_installation
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRedeemedVolume:
    artifact_id: str
    sha256: str
    bytes_verified: int
    api_response_sha256: str


def verify_fixture_intent_delivery(target,intent,volume,*,tls_context,assert_fixture_scope,check_active):
    api,objects=validate_preview_download_installation(target,tls_context=tls_context,
        assert_fixture_scope=assert_fixture_scope,check_active=check_active)
    if (not isinstance(intent,RestoreRegisteredIntent) or not isinstance(volume,RestoreExportVolume)
            or intent.registration.artifact_id!=volume.artifact_id
            or not 1<=volume.object.size<=10*1024**3):
        raise ValueError('RESTORE_REDEEM_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,target,intent.registration,volume)
    guard()
    if intent.ticket.expires_at<=datetime.now(timezone.utc):raise ValueError('RESTORE_REDEEM_EXPIRED')
    connection=http.client.HTTPSConnection(api.hostname,api.port or 443,context=tls_context,timeout=5)
    try:
        # Public redeem endpoint consumes only the bearer intent, no owner credential.
        connection.request('POST','/api/v1/downloads/redeem',body=canonical_bytes({'token':intent.ticket.token}),
            headers={'Content-Type':'application/json','Accept':'application/json','Accept-Encoding':'identity','X-Photo-Client':'native'})
        response=connection.getresponse();guard();raw=response.read(16385)
        if (response.status!=200 or len(raw)>16384 or response.getheader('Content-Encoding') not in (None,'identity')
                or response.getheader('Content-Type','').split(';')[0].strip().lower()!='application/json'):
            raise ValueError('RESTORE_REDEEM_REFUSED')
    except (OSError,http.client.HTTPException):raise ValueError('RESTORE_REDEEM_OUTCOME_UNKNOWN') from None
    finally:connection.close()
    value=parse_json(raw,max_bytes=16384);obj=volume.object
    if (not isinstance(value,dict) or set(value)!={'url','expires_at','filename','bytes','sha256'}
            or type(value['bytes']) is not int or value['bytes']!=obj.size or value['sha256']!=obj.sha256
            or not isinstance(value['filename'],str) or not value['filename'] or len(value['filename'])>1024
            or any(ord(c)<32 for c in value['filename']) or '/' in value['filename'] or '\\' in value['filename']
            or not isinstance(value['url'],str)):
        raise ValueError('RESTORE_REDEEM_LINK')
    try:
        expiry=datetime.fromisoformat(value['expires_at'].replace('Z','+00:00'));link=urlsplit(value['url'])
        valid=(expiry.tzinfo is not None and expiry>datetime.now(timezone.utc)
            and link.scheme=='https' and (link.hostname,link.port or 443)==(objects.hostname,objects.port or 443)
            and link.username is None and link.password is None and not link.fragment
            and all(33<=ord(c)<=126 for c in value['url'])
            and parse_qs(link.query).get('versionId')==[obj.version]
            and unquote(link.path)=='/'+obj.bucket+'/'+obj.key)
    except (ValueError,TypeError,AttributeError):valid=False
    if not valid:raise ValueError('RESTORE_REDEEM_LINK')
    guard();connection=http.client.HTTPSConnection(link.hostname,link.port or 443,context=tls_context,timeout=5)
    try:
        connection.request('GET',link.path+'?'+link.query,headers={'Accept-Encoding':'identity'})
        response=connection.getresponse();guard()
        if response.status!=200 or response.getheader('Content-Encoding') not in (None,'identity'):
            raise ValueError('RESTORE_REDEEM_OBJECT_REFUSED')
        digest=sha256();size=0
        while True:
            guard();block=response.read(min(65536,obj.size-size+1))
            if not block:break
            size+=len(block)
            if size>obj.size:raise ValueError('RESTORE_REDEEM_OBJECT_SIZE')
            digest.update(block)
        if size!=obj.size or digest.hexdigest()!=obj.sha256:raise ValueError('RESTORE_REDEEM_OBJECT_SHA')
    except (OSError,http.client.HTTPException):raise ValueError('RESTORE_REDEEM_OBJECT_UNAVAILABLE') from None
    finally:connection.close()
    guard();return RestoreRedeemedVolume(str(volume.artifact_id),obj.sha256,size,sha256(raw).hexdigest())
