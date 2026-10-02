"""Short-lived task credentials from the mTLS gateway, without storage keys."""
from __future__ import annotations
from datetime import datetime,timezone
import http.client
import json
import re
import ssl
from threading import Lock
import time
from typing import Callable
from urllib.parse import urlsplit
from .database import Lease
from .gateway import Gateway,GatewayError
from photo_worker.contracts.base import parse_json

_TOKEN=re.compile(r'^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$')

class TaskCredentials:
    """Callable token provider. Renew before expiry; never reuse on failure.

    The server rechecks the DB lease and binds its token to this TLS certificate.
    Local UTC clock must be synchronized. Monotonic time bounds cache lifetime
    even if the wall clock moves backwards after receipt.
    """
    def __init__(self,origin:str,lease:Lease,tls:ssl.SSLContext,check_active:Callable[[],None],*,timeout_seconds:float=10):
        u=urlsplit(origin)
        if u.scheme!='https' or not u.hostname or u.username is not None or u.password is not None or u.path or u.query or u.fragment:
            raise ValueError('Credentials require an exact HTTPS origin')
        if tls.verify_mode!=ssl.CERT_REQUIRED or not tls.check_hostname:
            raise ValueError('Credentials require verified server TLS')
        if not 0<timeout_seconds<=30:raise ValueError('Invalid credentials timeout')
        self._host,self._port=u.hostname,u.port or 443
        self._lease,self._tls,self._check=lease,tls,check_active
        self._timeout=timeout_seconds
        self._lock=Lock()
        self._token=''
        self._until=0.0
        self._expiry=datetime.min.replace(tzinfo=timezone.utc)

    def invalidate(self):
        with self._lock:
            self._token='';self._until=0.0

    def __call__(self)->str:
        self._check()
        with self._lock:
            self._check()
            if self._token and time.monotonic()<self._until and datetime.now(timezone.utc)<self._expiry:
                return self._token
            self._token='';self._until=0.0
            connection=http.client.HTTPSConnection(self._host,self._port,context=self._tls,timeout=self._timeout)
            started=time.monotonic()
            try:
                payload=json.dumps({'attempt':self._lease.command.attempt,'fence':str(self._lease.fence)},separators=(',',':'))
                connection.request('POST',f'/internal/v1/tasks/{self._lease.command.task_id}/credentials',body=payload,
                    headers={'Content-Type':'application/json','X-Photo-Worker':str(self._lease.worker_id),'Accept-Encoding':'identity'})
                response=connection.getresponse()
                Gateway._status(response)
                if response.status!=200 or response.getheader('Content-Type','').split(';')[0]!='application/json' or response.getheader('Content-Encoding','identity')!='identity':
                    raise GatewayError('GATEWAY_RESPONSE_INVALID')
                body=response.read(16385)
                try:
                    data=parse_json(body,max_bytes=16384)
                    if not isinstance(data,dict) or set(data)!={'token','token_type','expires_at','identity'}:raise ValueError()
                    expected={'tenant_id':str(self._lease.command.tenant_id),'run_id':str(self._lease.command.run_id),
                        'task_id':str(self._lease.command.task_id),'worker_id':str(self._lease.worker_id),
                        'attempt':self._lease.command.attempt,'fence':str(self._lease.fence)}
                    if not isinstance(data['identity'],dict) or data['identity']!=expected or type(data['identity'].get('attempt')) is not int:raise ValueError()
                    token=data['token']
                    if data['token_type']!='Bearer' or not isinstance(token,str) or len(token)>8192 or not _TOKEN.fullmatch(token):raise ValueError()
                    expiry=datetime.fromisoformat(data['expires_at'].replace('Z','+00:00'))
                    if expiry.tzinfo is None:raise ValueError()
                    remaining=(expiry-datetime.now(timezone.utc)).total_seconds()
                    if not 0<remaining<=60 or expiry>self._lease.deadline:raise ValueError()
                except (ValueError,TypeError,AttributeError,KeyError) as exc:
                    raise GatewayError('GATEWAY_RESPONSE_INVALID') from exc
                self._check()
                self._token=token
                self._expiry=expiry
                # Subtract both renewal margin and request duration conservatively.
                self._until=started+max(0.0,remaining-5)
                return token
            except ssl.SSLError as exc:
                raise GatewayError('CONFIG_INVALID') from exc
            except (OSError,http.client.HTTPException) as exc:
                raise GatewayError('DEPENDENCY_UNAVAILABLE',True) from exc
            finally:
                connection.close()
