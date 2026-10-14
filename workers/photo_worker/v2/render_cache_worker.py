"""Worker cache reuse through lease-gated SQL and the existing mTLS gateway."""
from dataclasses import replace
from photo_worker.contracts.base import ArtifactRef as ContractRef,canonical_bytes,parse_json
from .adoption import ArtifactEvidence,ArtifactRef,AdoptionInvalid
from .render_adoption_inputs import decode_render_adoption_inputs
from .render_qualification_pin import RenderQualificationPin
from .render_cache_identity import render_cache_context,render_cache_identity
from .render_cache_lookup import decode_render_cache_entry
from .render_cache_bytes import verified_render_cache_files
from .render_cache_republish import republish_render_cache
from .render_inputs import read_render_document


class GatewayRenderCacheReader:
    def __init__(self,candidate,rows,gateway,check_active):
        inventory=parse_json(candidate.inventory_jcs,max_bytes=16777216)
        if not rows or len(rows)!=len(inventory):raise AdoptionInvalid('RENDER_CACHE_MANIFEST_STALE')
        self.gateway=gateway;self.check_active=check_active;self.metadata={}
        for ordinal,(row,entry) in enumerate(zip(rows,inventory)):
            if len(row)!=9:raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            identifier,digest,size,media,role,_,_,version,index=row
            ref=ArtifactRef(identifier,digest)
            if (str(identifier)!=entry['artifact_id'] or digest!=entry['sha256'] or index!=ordinal
                    or type(size) is not int or size<1 or not isinstance(version,str) or version in ('','null')):
                raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            evidence=ArtifactEvidence(ref,media,size,role,'cache')
            if ref in self.metadata and self.metadata[ref]!=evidence:raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')
            self.metadata[ref]=evidence
        if candidate.root!=ArtifactRef(rows[0][0],rows[0][1]):raise AdoptionInvalid('RENDER_CACHE_MANIFEST_INVALID')

    def evidence(self,ref):
        self.check_active()
        if ref not in self.metadata:raise AdoptionInvalid('RENDER_CACHE_UNDECLARED_ARTIFACT')
        return self.metadata[ref]

    def iter_bytes(self,ref,max_bytes):
        evidence=self.evidence(ref)
        if evidence.bytes>max_bytes:raise AdoptionInvalid('RENDER_CACHE_BYTES_LIMIT')
        reference=ContractRef(artifact_id=ref.artifact_id,sha256=ref.sha256)
        with self.gateway.open_stream(reference) as stream:
            if stream.size!=evidence.bytes:raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
            while True:
                self.check_active();block=stream.read(65536)
                if not block:break
                yield block


class WorkerRenderCache:
    def __init__(self,database,*,max_bytes):
        if type(max_bytes) is not int or max_bytes<1:raise ValueError('Invalid cache scratch bound')
        self.database=database;self.max_bytes=max_bytes

    def try_publish(self,ctx,artifacts,qualification_pin):
        ctx.check_active();lease=ctx.lease;command=lease.command
        raw=read_render_document(artifacts,command.params,ctx.check_active)
        row=self.database.render_cache_context(lease,raw)
        if row is None:raise AdoptionInvalid('RENDER_CACHE_CONTEXT_STALE')
        if len(row)!=5:raise AdoptionInvalid('RENDER_CACHE_CONTEXT_INVALID')
        context=replace(decode_render_adoption_inputs(row[:4]),qualification_pin=RenderQualificationPin.from_value(row[4]))
        if (context.qualification_pin!=qualification_pin or context.execution_profile()!=lease.execution_profile
                or context.recipe!=ArtifactRef(command.params.artifact_id,command.params.sha256)):
            raise AdoptionInvalid('RENDER_CACHE_CONTEXT_INVALID')
        profile=context.execution_profile()
        key=render_cache_identity(command.tenant_id,context.recipe,profile,context.parent_roots,context.qualification_pin)
        body=canonical_bytes(render_cache_context(command.tenant_id,context.recipe,profile,context.parent_roots,context.qualification_pin)).decode()
        candidate=decode_render_cache_entry(self.database.lookup_render_cache(lease,raw,key,body))
        if candidate is None:
            ctx.check_active()
            if not self.database.admit_render_recipe(lease,raw):
                raise AdoptionInvalid('RENDER_CACHE_CONTEXT_STALE')
            return None
        from photo_worker.runtime.gateway import GatewayError
        staging_started=False
        def started():
            nonlocal staging_started
            staging_started=True
        try:
            rows=self.database.render_cache_artifacts(lease,raw,key,body)
            reader=GatewayRenderCacheReader(candidate,rows,artifacts.gateway,ctx.check_active)
            # Keep room for the largest fresh output copy/manifest before any IO.
            sizes={ref:evidence.bytes for ref,evidence in reader.metadata.items()}
            if sum(sizes.values())+max(sizes.values())>self.max_bytes:return None
            with verified_render_cache_files(candidate,reader,artifacts.scratch,max_bytes=self.max_bytes,
                                             check_active=ctx.check_active) as files:
                declared={file.ref for file in files if artifacts._allows(
                    ContractRef(artifact_id=file.ref.artifact_id,sha256=file.ref.sha256))}
                ctx.check_active()
                return republish_render_cache(candidate,files,declared,artifacts,check_active=ctx.check_active,on_staging_start=started)
        except (AdoptionInvalid,GatewayError) as error:
            if isinstance(error,GatewayError) and error.code not in ('ARTIFACT_MISSING','ARTIFACT_HASH_MISMATCH'):
                raise
            ctx.check_active()
            self.database.quarantine_render_cache(lease,raw,key,body,candidate.result_id)
            if staging_started or not self.database.admit_render_recipe(lease,raw):raise
            return None
