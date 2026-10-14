"""Job-scoped Unix client for Go-owned replacement adoption transactions."""
from copy import deepcopy
from http.client import HTTPConnection, HTTPException
from pathlib import Path
import re
import socket
import stat
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, parse_json
from .adoption import AdoptionInvalid, prepare_v2_adoption
from .perspective_lines_adoption_commit import perspective_lines_commit_statement
from .perspective_lines_adoption_evidence import validate_perspective_lines_adoption_evidence
from .perspective_replacement_detection_replay import prepare_replacement_detection_replay


class _UnixConnection(HTTPConnection):
    def __init__(self,path,timeout):
        super().__init__('localhost',timeout=timeout)
        self.path=path

    def connect(self):
        if not stat.S_ISSOCK(Path(self.path).lstat().st_mode):
            raise ValueError('REPLACEMENT_ADOPTION_SOCKET_INVALID')
        self.sock=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        try:
            self.sock.settimeout(self.timeout)
            self.sock.connect(self.path)
        except BaseException:
            self.sock.close()
            self.sock=None
            raise


class ReplacementDetectionAdoptionBridge:
    def __init__(self,path,token,tenant_id,run_id,task_id,*,timeout=20,check_active):
        if (not isinstance(path,Path) or not path.is_absolute() or len(str(path).encode())>100
                or not isinstance(token,str) or re.fullmatch('[0-9a-f]{64}',token) is None
                or type(timeout) not in (int,float) or not 0<timeout<=25 or not callable(check_active)
                or any(not isinstance(value,UUID) or value.int==0 for value in (tenant_id,run_id,task_id))):
            raise ValueError('REPLACEMENT_ADOPTION_BRIDGE_CONFIGURATION')
        self._path,self._token,self._timeout=str(path),token,timeout
        self.tenant_id,self.run_id,self.task_id=tenant_id,run_id,task_id
        self.check_active=check_active

    def _request(self,route,context=None,**payload):
        self.check_active()
        packet={'schema_version':1,'tenant_id':str(self.tenant_id),'run_id':str(self.run_id),
            'task_id':str(self.task_id),'attempt':0,'fence':0,'result_id':str(UUID(int=0))}
        if context is not None:
            if (type(context.attempt) is not int or not 1<=context.attempt<=6
                    or type(context.fence) is not int or not 1<=context.fence<=2**53-1
                    or not isinstance(context.result_id,UUID) or context.result_id.int==0):
                raise AdoptionInvalid('REPLACEMENT_ADOPTION_CONTEXT_INVALID')
            packet.update(attempt=context.attempt,fence=context.fence,result_id=str(context.result_id))
        packet.update(payload)
        raw=canonical_bytes(packet)
        if len(raw)>12*1024*1024:
            raise AdoptionInvalid('REPLACEMENT_ADOPTION_REQUEST_TOO_LARGE')
        connection=_UnixConnection(self._path,self._timeout)
        try:
            connection.request('POST',route,body=raw,headers={'Authorization':'Bearer '+self._token,'Content-Type':'application/json'})
            response=connection.getresponse()
            limit=300000 if route=='/evidence' else 4096
            body=response.read(limit+1)
            self.check_active()
            if response.status!=200 or response.getheader('Content-Type')!='application/json' or len(body)>limit:
                raise AdoptionInvalid('REPLACEMENT_ADOPTION_BRIDGE_REFUSED')
            value=parse_json(body,limit)
            field={'/evidence':'evidence','/commit':'committed','/adopted':'adopted'}[route]
            if (not isinstance(value,dict) or set(value)!={'schema_version',field}
                    or type(value['schema_version']) is not int or value['schema_version']!=1
                    or canonical_bytes(value)!=body):
                raise AdoptionInvalid('REPLACEMENT_ADOPTION_BRIDGE_RESPONSE_INVALID')
            return value[field]
        finally:
            connection.close()

    def was_adopted(self):
        value=self._request('/adopted')
        if type(value) is not bool:
            raise AdoptionInvalid('REPLACEMENT_ADOPTION_BRIDGE_RESPONSE_INVALID')
        return value

    def evidence(self,context,params_raw):
        value=validate_perspective_lines_adoption_evidence(self._request('/evidence',context),self.task_id,context,params_raw)
        if value['run_id']!=str(self.run_id):
            raise AdoptionInvalid('REPLACEMENT_ADOPTION_BRIDGE_RESPONSE_INVALID')
        return value

    def commit(self,context,inventory,digest,prepared):
        # Existing validator binds replay evidence/Params/root to attempt/fence.
        perspective_lines_commit_statement(self.tenant_id,self.task_id,context,inventory,digest,prepared)
        value=self._request('/commit',context,inventory=inventory,digest=digest,
            evidence=parse_json(prepared.evidence_jcs,262144),root=parse_json(prepared.result_bytes,1048576))
        if value is not True:
            raise AdoptionInvalid('REPLACEMENT_ADOPTION_COMMIT_UNCONFIRMED')

    def replay_and_adopt(self,reader,context,params_ref,descriptor_jcs,*,installation):
        if self.was_adopted():
            return True
        ctx,ref=deepcopy(context),deepcopy(params_ref)
        params_raw=reader.read_json(ref,65536)
        prepared=prepare_replacement_detection_replay(reader,self.task_id,ctx,ref,descriptor_jcs,
            installation=installation,load_current_evidence=lambda:self.evidence(ctx,params_raw),check_active=self.check_active)
        inventory=prepare_v2_adoption(task_id=self.task_id,operation='optical.perspective_lines',version='1',
            root=ctx.root,reader=reader,params_ref=ref,check_active=self.check_active)
        if canonical_bytes(self.evidence(ctx,params_raw))!=prepared.evidence_jcs:
            raise AdoptionInvalid('REPLACEMENT_ADOPTION_EVIDENCE_CHANGED')
        try:
            self.commit(ctx,list(inventory.inventory),inventory.digest,prepared)
        except (OSError,HTTPException,AdoptionInvalid):
            # Read-only recovery after an ambiguous response. Never resend commit.
            self.check_active()
            if not self.was_adopted():
                raise
            return True
        self.check_active()
        return False
