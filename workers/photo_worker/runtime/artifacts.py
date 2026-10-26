"""Per-attempt artifact lifecycle used by atomic operation adapters."""
from __future__ import annotations
from dataclasses import dataclass
from pathlib import Path
import ssl
from typing import TypeVar
from contextlib import contextmanager
from photo_worker.contracts.archive import ArchiveParams,ArchiveManifest,ARCHIVE_DOCUMENT_MAX_BYTES
from photo_worker.contracts.base import ArtifactRef,Contract,canonical_bytes
from .credentials import TaskCredentials
from .executor import ExecutionContext
from .gateway import Gateway,GatewayError
from .materialize import materialize_gateway
from .parameters import read_contract,read_style,read_contract_bytes
from .scratch import ScratchSpace,Reservation,ScratchError
from photo_worker.v2.input_closure import V2InputIndex

def _archive_document_limit(model):
    return ARCHIVE_DOCUMENT_MAX_BYTES if model is ArchiveParams or model is ArchiveManifest else 1048576

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
                 v2_index:V2InputIndex|None=None,*,deferred_v2_index:bool=False):
        self.ctx,self.gateway,self.scratch=ctx,gateway,scratch
        self._closed=False
        self._v2_index=v2_index
        self._deferred_v2_index=deferred_v2_index
        self._allowed=frozenset((r.artifact_id,r.sha256) for r in [*ctx.lease.command.inputs,ctx.lease.command.params])

    @classmethod
    def create(cls,ctx:ExecutionContext,config:ArtifactConfig,roles:frozenset[str],*,
               defer_v2_index:bool=False):
        ctx.check_active()
        if type(defer_v2_index) is not bool:raise ValueError('Invalid input-index policy')
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
            session=cls(ctx,gateway,scratch,deferred_v2_index=defer_v2_index)
            if (not defer_v2_index and
                    ctx.lease.execution_profile.get('renderer_version')=='v2.2'):
                session.load_v2_index()
            return session
        except BaseException:
            scratch.cleanup()
            raise

    def _check(self,*,params_only:bool=False):
        if self._closed:raise ScratchError('Artifact session is closed')
        self.ctx.check_active()
        if self._deferred_v2_index and not params_only:
            raise ValueError('V2 input index is not ready')

    def load_v2_index(self) -> None:
        """Load the full V2 closure after a task-specific preflight gate."""
        self._check(params_only=True)
        if self._v2_index is not None:return
        if self.ctx.lease.execution_profile.get('renderer_version')!='v2.2':
            raise ValueError('Typed input index requires V2 renderer')
        self._v2_index=V2InputIndex.build(
            task_id=self.ctx.lease.command.task_id,
            inputs=self.ctx.lease.command.inputs,params=self.ctx.lease.command.params,
            execution_profile=self.ctx.lease.execution_profile,scratch=self.scratch,
            open_stream=self.gateway.open_stream,check_active=self.ctx.check_active)
        self._deferred_v2_index=False
        self._check()

    def _allows(self,ref:ArtifactRef)->bool:
        return (self._v2_index.allows(ref) if self._v2_index is not None
                else (ref.artifact_id,ref.sha256) in self._allowed)

    def require_declared_input(self, ref: ArtifactRef) -> None:
        """Check exact input membership without downloading or opening a file."""
        self._check()
        if not self._allows(ref):
            raise ValueError('Artifact is not a declared task input')

    def require_declared_contract(self, ref: ArtifactRef, contract: str) -> None:
        """Require an exact typed entry in the verified V2 input closure."""
        self._check()
        if self._v2_index is None:
            raise ValueError('Typed input discovery requires V2 closure')
        self._v2_index.require_entry(ref, contract)

    def contract(self,ref:ArtifactRef,model:type[T])->T:
        self._check()
        if not self._allows(ref):
            raise ValueError('Artifact is not a declared task input')
        return read_contract(ref,model,self.gateway.open_stream,self.ctx.check_active,
                             max_bytes=8*1024*1024 if self._v2_index is not None or model.__name__ in {"SceneGroups","CullRecommendations","SeriesCorrections"} else _archive_document_limit(model))

    def contract_bytes(self,ref:ArtifactRef,model:type[T])->bytes:
        self._check()
        if not self._allows(ref):raise ValueError('Artifact is not a declared task input')
        return read_contract_bytes(ref,model,self.gateway.open_stream,self.ctx.check_active,
            max_bytes=8*1024*1024 if self._v2_index is not None or model.__name__ in {"SceneGroups","CullRecommendations","SeriesCorrections"} else _archive_document_limit(model))

    def declared_contract_refs(self, contract: str, maximum: int = 1) -> tuple[ArtifactRef, ...]:
        self._check()
        if self._v2_index is None:
            raise ValueError('Typed input discovery requires V2 closure')
        return self._v2_index.refs_for_contract(contract, maximum)

    def declared_input_refs(self, maximum: int = 512) -> tuple[ArtifactRef, ...]:
        """Full verified V2 allowlist, including Params and transport metadata."""
        self._check()
        if self._v2_index is None:
            raise ValueError('Full input discovery requires V2 closure')
        return self._v2_index.allowed_refs(maximum)

    def declared_input_entries(self, maximum: int = 30000):
        """Typed verified closure entries; never infer object metadata from labels."""
        self._check()
        if self._v2_index is None:
            raise ValueError('Typed input discovery requires V2 closure')
        result = self._v2_index.declared_entries(maximum)
        self._check()
        return result

    def input_metadata(self, ref: ArtifactRef):
        """Fresh gateway metadata for one exact declared immutable input."""
        owned = ref.model_copy(deep=True)
        self.require_declared_input(owned)
        result = self.gateway.metadata(owned)
        self._check()
        return result

    def params(self,model:type[T])->T:
        self._check(params_only=True)
        ref=self.ctx.lease.command.params
        if not self._allows(ref):raise ValueError('Params artifact is not a declared task input')
        return read_contract(ref,model,self.gateway.open_stream,self.ctx.check_active,max_bytes=_archive_document_limit(model))

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

    def stage(self,path:Path,reservation:Reservation,role:str,media_type:str,*,
              expected_identity=None)->ArtifactRef:
        self._check()
        if reservation.owner is not self.scratch:raise ScratchError('Output reservation belongs to another attempt')
        observed_identity=reservation.validate_file(path)
        if expected_identity is not None and observed_identity!=expected_identity:
            raise GatewayError('ARTIFACT_CHANGED')
        result=self.gateway.upload(path,role,media_type,expected_identity=observed_identity)
        self._check()
        return result

    def stage_contract(self,value:Contract,role:str)->ArtifactRef:
        self._check()
        if not isinstance(value,Contract):raise TypeError('Output manifest requires a strict contract')
        payload=canonical_bytes(value)
        limit=8*1024*1024 if self._v2_index is not None or type(value).__name__ in {"SceneGroups","CullRecommendations","SeriesCorrections"} else _archive_document_limit(type(value))
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

    def __enter__(self):self._check(params_only=True);return self
    def __exit__(self,*exc):self.close()
