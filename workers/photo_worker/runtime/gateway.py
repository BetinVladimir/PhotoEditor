"""mTLS object-gateway transport. No bucket names, object keys or redirects."""
from __future__ import annotations
import hashlib
import http.client
import os
from pathlib import Path
from dataclasses import dataclass
import re
import ssl
import stat
from typing import Callable
from urllib.parse import urlsplit
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,parse_json
from .database import Lease

_ROLE=re.compile(r'^[a-z][a-z0-9_-]{0,63}$')
_TOKEN=re.compile(r'^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$')


class GatewayError(RuntimeError):
    def __init__(self,code:str,retryable:bool=False):
        super().__init__(code)
        self.code,self.retryable=code,retryable


def client_tls(ca:Path,certificate:Path,private_key:Path)->ssl.SSLContext:
    for path in (ca,certificate,private_key):
        if path.is_symlink() or not path.is_file():
            raise ValueError('TLS credential must be a regular file')
    if private_key.stat().st_mode&0o077:
        raise ValueError('TLS private key must have mode 0600 or stricter')
    context=ssl.create_default_context(ssl.Purpose.SERVER_AUTH,cafile=str(ca))
    context.minimum_version=ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(str(certificate),str(private_key))
    return context


@dataclass(frozen=True)
class ArtifactMetadata:
    bytes: int
    media_type: str
    version_sha256: str


def artifact_metadata(headers, size):
    media = headers.get_all('X-Photo-Media-Type') or []
    versions = headers.get_all('X-Photo-Object-Version-SHA256') or []
    if not media and not versions:
        return None  # Older gateways can stream, but cannot qualify metadata.
    if (len(media) != 1 or len(versions) != 1
            or not re.fullmatch(r'[a-z0-9!#$&^_.+-]+/[a-z0-9!#$&^_.+-]+', media[0])
            or not re.fullmatch(r'[0-9a-f]{64}', versions[0])):
        raise GatewayError('GATEWAY_RESPONSE_INVALID')
    return ArtifactMetadata(size, media[0], versions[0])


class ArtifactStream:
    def __init__(self,response:http.client.HTTPResponse,connection:http.client.HTTPSConnection,
                 expected:int,check_active:Callable[[],None],metadata:ArtifactMetadata|None=None):
        self._response,self._connection=response,connection
        self.metadata=metadata
        self._remaining=expected
        self._size=expected
        self._check_active=check_active
        self.closed=False

    @property
    def size(self)->int:
        return self._size

    def read(self,size:int=-1)->bytes:
        if self.closed:raise ValueError('Artifact stream is closed')
        if type(size) is not int or size<0:
            raise ValueError('Artifact reads must have a bounded size')
        self._check_active()
        try:
            data=self._response.read(min(size,self._remaining))
        except (OSError,http.client.HTTPException) as exc:
            raise GatewayError('DEPENDENCY_UNAVAILABLE',True) from exc
        self._check_active()
        if not data and self._remaining and size:
            raise GatewayError('ARTIFACT_SIZE_MISMATCH')
        self._remaining-=len(data)
        return data

    def close(self):
        if not self.closed:
            self.closed=True
            self._response.close()
            self._connection.close()

    def __enter__(self):return self
    def __exit__(self,*exc):self.close()


class Gateway:
    def __init__(self,origin:str,lease:Lease,tls:ssl.SSLContext,token:Callable[[],str],
                 check_active:Callable[[],None],*,max_download_bytes:int,max_upload_bytes:int,allowed_output_roles:frozenset[str],
                 timeout_seconds:float=10):
        u=urlsplit(origin)
        if u.scheme!='https' or not u.hostname or u.username is not None or u.password is not None or u.path or u.query or u.fragment:
            raise ValueError('Gateway requires an exact HTTPS origin')
        if tls.verify_mode!=ssl.CERT_REQUIRED or not tls.check_hostname:
            raise ValueError('Gateway requires verified server TLS')
        if any(type(v) is not int or not 0<=v<=2**53-1 for v in (max_download_bytes,max_upload_bytes)) or not 0<timeout_seconds<=30:
            raise ValueError('Invalid gateway limits')
        if not isinstance(allowed_output_roles,frozenset) or any(not _ROLE.fullmatch(role) for role in allowed_output_roles):
            raise ValueError('Invalid output role allowlist')
        self._host,self._port=u.hostname,u.port or 443
        self._tls,self._token,self._check=tls,token,check_active
        self._lease,self._max=lease,max_download_bytes
        self._max_output=max_upload_bytes
        self._roles,self._timeout=allowed_output_roles,timeout_seconds

    def _connection(self):
        self._check()
        return http.client.HTTPSConnection(self._host,self._port,context=self._tls,timeout=self._timeout)

    def _headers(self):
        self._check()
        token=self._token()
        if not isinstance(token,str) or len(token)>8192 or not _TOKEN.fullmatch(token):
            raise GatewayError('INVALID_TASK_CAPABILITY')
        return {'Authorization':'Bearer '+token,'X-Photo-Attempt':str(self._lease.command.attempt),
                'X-Photo-Fence':str(self._lease.fence),'X-Photo-Worker':str(self._lease.worker_id),
                'Accept-Encoding':'identity'}

    @staticmethod
    def _status(response):
        if response.status in (401,403,409,410):raise GatewayError('LEASE_LOST')
        if response.status==404:raise GatewayError('ARTIFACT_MISSING')
        if response.status==413:raise GatewayError('DISK_QUOTA')
        if response.status in (429,500,502,503,504):raise GatewayError('DEPENDENCY_UNAVAILABLE',True)
        if not 200<=response.status<300:raise GatewayError('GATEWAY_RESPONSE_INVALID')

    def open_stream(self,ref:ArtifactRef)->ArtifactStream:
        connection=self._connection()
        try:
            headers=self._headers()
            headers['Accept']='application/octet-stream'
            connection.request('GET',f'/internal/v1/tasks/{self._lease.command.task_id}/objects/{ref.artifact_id}',headers=headers)
            response=connection.getresponse()
            self._status(response)
            if response.status!=200 or response.getheader('Content-Encoding','identity')!='identity' or response.getheader('Transfer-Encoding') is not None:
                raise GatewayError('GATEWAY_RESPONSE_INVALID')
            lengths=response.headers.get_all('Content-Length') or []
            if len(lengths)!=1 or not lengths[0].isascii() or not lengths[0].isdigit():
                raise GatewayError('GATEWAY_RESPONSE_INVALID')
            size=int(lengths[0])
            if size>self._max:raise GatewayError('DISK_QUOTA')
            if response.getheader('X-Photo-SHA256')!=ref.sha256 or response.getheader('X-Photo-Artifact-ID')!=str(ref.artifact_id):
                raise GatewayError('ARTIFACT_HASH_MISMATCH')
            self._check()
            return ArtifactStream(response,connection,size,self._check,artifact_metadata(response.headers,size))
        except ssl.SSLError as exc:
            connection.close()
            raise GatewayError('CONFIG_INVALID') from exc
        except (OSError,http.client.HTTPException) as exc:
            connection.close()
            raise GatewayError('DEPENDENCY_UNAVAILABLE',True) from exc
        except BaseException:
            connection.close()
            raise

    def metadata(self, ref: ArtifactRef) -> ArtifactMetadata:
        # GET headers run the same lease/input/object authority as byte reads.
        # Close immediately; do not download or materialize the body.
        with self.open_stream(ref) as stream:
            if stream.metadata is None:
                raise GatewayError('GATEWAY_METADATA_UNAVAILABLE')
            return stream.metadata

    def upload(self,path:Path,role:str,media_type:str,*,expected_identity=None)->ArtifactRef:
        if role not in self._roles:raise ValueError('Output role is not allowed')
        if not re.fullmatch(r'[a-z0-9.+-]+/[a-z0-9.+-]+',media_type):raise ValueError('Invalid output media type')
        self._check()
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as source:
            before=os.fstat(source.fileno())
            if not stat.S_ISREG(before.st_mode):raise ValueError('Output must be a regular file')
            if expected_identity is not None:
                if (not isinstance(expected_identity,tuple) or len(expected_identity)!=2
                        or any(type(value) is not int or value<0 for value in expected_identity)
                        or before.st_nlink!=1
                        or (before.st_dev,before.st_ino)!=expected_identity):
                    raise GatewayError('ARTIFACT_CHANGED')
            if before.st_size>self._max_output:raise GatewayError('DISK_QUOTA')
            digest=hashlib.sha256()
            for block in iter(lambda:source.read(1024*1024),b''):
                self._check();digest.update(block)
            sha=digest.hexdigest()
            source.seek(0)
            connection=self._connection()
            try:
                headers=self._headers()
                headers.update({'Content-Type':media_type,'Content-Length':str(before.st_size),
                                'X-Photo-SHA256':sha,'Idempotency-Key':str(uuid4())})
                connection.putrequest('PUT',f'/internal/v1/tasks/{self._lease.command.task_id}/outputs/{role}',skip_accept_encoding=True)
                for key,value in headers.items():connection.putheader(key,value)
                connection.endheaders()
                sent=0;actual=hashlib.sha256()
                for block in iter(lambda:source.read(1024*1024),b''):
                    self._check();sent+=len(block)
                    if sent>before.st_size:raise GatewayError('ARTIFACT_CHANGED')
                    connection.send(block);actual.update(block)
                if sent!=before.st_size or actual.hexdigest()!=sha:raise GatewayError('ARTIFACT_CHANGED')
                self._check()
                response=connection.getresponse()
                self._status(response)
                if response.status not in (200,201) or response.getheader('Content-Type','').split(';')[0]!='application/json':
                    raise GatewayError('GATEWAY_RESPONSE_INVALID')
                lengths=response.headers.get_all('Content-Length') or []
                transfer=response.getheader('Transfer-Encoding')
                if (response.getheader('Content-Encoding','identity')!='identity' or
                    len(lengths)>1 or
                    (lengths and (not lengths[0].isascii() or not lengths[0].isdigit())) or
                    (lengths and transfer is not None) or
                    (transfer is not None and transfer.lower()!='chunked')):
                    raise GatewayError('GATEWAY_RESPONSE_INVALID')
                if response.length is not None and response.length>65536:
                    raise GatewayError('GATEWAY_RESPONSE_INVALID')
                body=response.read(65537)
                # HTTPResponse.read(amount) may return a short body without
                # IncompleteRead. A remaining declared length means a lost
                # response, not malformed operation input or a safe PUT retry.
                if response.length is not None and response.length>0:
                    raise GatewayError('DEPENDENCY_UNAVAILABLE',True)
                try:
                    parse_json(body,max_bytes=65536)
                    ref=ArtifactRef.model_validate_json(body)
                except ValueError as exc:
                    raise GatewayError('GATEWAY_RESPONSE_INVALID') from exc
                if ref.sha256!=sha:raise GatewayError('ARTIFACT_HASH_MISMATCH')
                if expected_identity is not None:
                    try:
                        current=os.stat(path,follow_symlinks=False)
                    except FileNotFoundError as exc:
                        raise GatewayError('ARTIFACT_CHANGED') from exc
                    if (not stat.S_ISREG(current.st_mode) or current.st_nlink!=1
                            or (current.st_dev,current.st_ino)!=expected_identity):
                        raise GatewayError('ARTIFACT_CHANGED')
                self._check()
                return ref
            except ssl.SSLError as exc:
                raise GatewayError('CONFIG_INVALID') from exc
            except (OSError,http.client.HTTPException) as exc:
                # Unknown PUT outcome is not retried automatically. An accepted
                # orphan is invisible until the fenced SQL task commit.
                raise GatewayError('DEPENDENCY_UNAVAILABLE',True) from exc
            finally:
                connection.close()
