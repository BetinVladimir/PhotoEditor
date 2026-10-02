"""Per-attempt artifact lifecycle used by atomic operation adapters."""
from __future__ import annotations
from dataclasses import dataclass
from pathlib import Path
import ssl
from typing import TypeVar
from contextlib import contextmanager
from photo_worker.contracts.base import ArtifactRef,Contract,canonical_bytes
from .credentials import TaskCredentials
from .executor import ExecutionContext
from .gateway import Gateway
from .materialize import materialize_gateway
from .parameters import read_contract,read_style
from .scratch import ScratchSpace,Reservation,ScratchError
from photo_worker.v2.input_closure import V2InputIndex

T=TypeVar('T',bound=Contract)

@dataclass(frozen=True)
class ArtifactConfig:
    root:Path
    origin:str
    tls:ssl.SSLContext
    max_scratch_bytes:int
    max_download_bytes:int
    max_upload_bytes:int


class TaskArtifacts:
    def __init__(self,ctx:ExecutionContext,gateway:Gateway,scratch:ScratchSpace,
                 v2_index:V2InputIndex|None=None):
        self.ctx,self.gateway,self.scratch=ctx,gateway,scratch
        self._closed=False
        self._v2_index=v2_index
        self._allowed=frozenset((r.artifact_id,r.sha256) for r in [*ctx.lease.command.inputs,ctx.lease.command.params])

    @classmethod
    def create(cls,ctx:ExecutionContext,config:ArtifactConfig,roles:frozenset[str]):
        ctx.check_active()
        for value in (config.max_scratch_bytes,config.max_download_bytes,config.max_upload_bytes):
            if type(value) is not int or not 0<=value<=2**53-1:raise ValueError('Invalid local artifact limit')
        estimate=ctx.lease.execution_profile.get('resource_estimate')
        if not isinstance(estimate,dict):raise ValueError('Missing admitted resource estimate')
        scratch_bytes,output_bytes=estimate.get('scratch_bytes'),estimate.get('output_bytes')
        if any(type(value) is not int or not 0<=value<=2**53-1 for value in (scratch_bytes,output_bytes)):
            raise ValueError('Invalid admitted artifact budget')
        if scratch_bytes>config.max_scratch_bytes:raise ValueError('Admitted scratch exceeds worker configuration')
        credentials=TaskCredentials(config.origin,ctx.lease,config.tls,ctx.check_active)
        gateway=Gateway(config.origin,ctx.lease,config.tls,credentials,ctx.check_active,
            max_download_bytes=config.max_download_bytes,max_upload_bytes=min(output_bytes,config.max_upload_bytes),allowed_output_roles=roles)
        scratch=ScratchSpace(config.root,ctx.lease.command.tenant_id,ctx.lease.command.task_id,ctx.lease.command.attempt,scratch_bytes)
        try:
            index=None
            if ctx.lease.execution_profile.get('renderer_version')=='v2.2':
                index=V2InputIndex.build(task_id=ctx.lease.command.task_id,
                    inputs=ctx.lease.command.inputs,params=ctx.lease.command.params,
                    execution_profile=ctx.lease.execution_profile,scratch=scratch,
                    open_stream=gateway.open_stream,check_active=ctx.check_active)
            return cls(ctx,gateway,scratch,index)
        except BaseException:
            scratch.cleanup()
            raise

    def _check(self):
        if self._closed:raise ScratchError('Artifact session is closed')
        self.ctx.check_active()

    def _allows(self,ref:ArtifactRef)->bool:
        return (self._v2_index.allows(ref) if self._v2_index is not None
                else (ref.artifact_id,ref.sha256) in self._allowed)

    def contract(self,ref:ArtifactRef,model:type[T])->T:
        self._check()
        if not self._allows(ref):
            raise ValueError('Artifact is not a declared task input')
        return read_contract(ref,model,self.gateway.open_stream,self.ctx.check_active,
                             max_bytes=8*1024*1024 if self._v2_index is not None else 1048576)

    def declared_contract_refs(self, contract: str, maximum: int = 1) -> tuple[ArtifactRef, ...]:
        self._check()
        if self._v2_index is None:
            raise ValueError('Typed input discovery requires V2 closure')
        return self._v2_index.refs_for_contract(contract, maximum)

    def params(self,model:type[T])->T:
        self._check()
        ref=self.ctx.lease.command.params
        if not self._allows(ref):raise ValueError('Params artifact is not a declared task input')
        return read_contract(ref,model,self.gateway.open_stream,self.ctx.check_active)

    def style(self,ref:ArtifactRef):
        self._check()
        if not self._allows(ref):
            raise ValueError('Style is not a declared task input')
        estimate=self.ctx.lease.execution_profile.get('resource_estimate')
        ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
        # Full strict JSON/JCS validation has simultaneous object and byte
        # representations. Style operations must explicitly admit this budget.
        if type(ram) is not int or ram<3*1024**3:raise ValueError('Style parsing requires admitted RAM')
        return read_style(ref,self.gateway.open_stream,self.ctx.check_active)

    def input(self,ref:ArtifactRef,*,suffix:str='.bin'):
        self._check()
        if not self._allows(ref):
            raise ValueError('Artifact is not a declared task input')
        return materialize_gateway(ref,self.scratch,self.gateway.open_stream,self.ctx.check_active,suffix=suffix)

    @contextmanager
    def stream(self,ref:ArtifactRef):
        """Open one declared input without materializing it on scratch disk."""
        self._check()
        if not self._allows(ref):
            raise ValueError('Artifact is not a declared task input')
        with self.gateway.open_stream(ref) as source:
            yield source
        self._check()

    def stage(self,path:Path,reservation:Reservation,role:str,media_type:str)->ArtifactRef:
        self._check()
        if reservation.owner is not self.scratch:raise ScratchError('Output reservation belongs to another attempt')
        reservation.validate_file(path)
        result=self.gateway.upload(path,role,media_type)
        self._check()
        return result

    def stage_contract(self,value:Contract,role:str)->ArtifactRef:
        self._check()
        if not isinstance(value,Contract):raise TypeError('Output manifest requires a strict contract')
        payload=canonical_bytes(value)
        limit=8*1024*1024 if self._v2_index is not None else 1048576
        if len(payload)>limit:raise ValueError('Output manifest exceeds typed document limit')
        with self.scratch.reserve(len(payload)) as reservation:
            path=reservation.path('manifest','.json')
            with self.scratch.open_writer(path,reservation) as output:output.write(payload)
            return self.stage(path,reservation,role,'application/json')

    def close(self):
        if not self._closed:
            try:
                if self._v2_index is not None:self._v2_index.close()
            finally:
                try:self.scratch.cleanup()
                finally:self._closed=True

    def __enter__(self):self._check();return self
    def __exit__(self,*exc):self.close()
