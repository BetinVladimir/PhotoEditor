"""Validate and republish a cached typed DAG into a fresh attempt's outputs."""
from dataclasses import dataclass
from hashlib import sha256
from jsonschema import Draft202012Validator,FormatChecker
from photo_worker.contracts.base import canonical_bytes,parse_json,ArtifactRef as ContractRef
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from .adoption import (AdoptionInvalid,ArtifactRef,OUTPUT_BINDINGS,MAX_DEPTH,MAX_JSON_BYTES,
                       _load_registry,_schema_refs,SCHEMA_PATH,OPERATIONS_PATH)
from .render_result import StagedRenderResult
from .render_cache_bytes import VerifiedCacheFile
from .render_cache_lookup import RenderCacheEntry

MODELS={'ImageResult':ImageResult,'ImageReport':ImageReport,'WorkingImage':WorkingImage,'MaskRef':MaskRef}


@dataclass(frozen=True)
class CachePublicationNode:
    file: VerifiedCacheFile
    contract: str
    role: str
    document_jcs: bytes | None


def plan_render_cache_publication(candidate,files,declared_inputs):
    """Preserve exact current declared inputs, stage all other reachable outputs.

    Typed DAG, role/count policy and complete closure validation finish before
    any write. Subsequent canonical adoption still independently recomposes.
    """
    if (not isinstance(candidate,RenderCacheEntry) or not 1<=len(files)<=30001
            or not isinstance(declared_inputs,(set,frozenset))
            or any(not isinstance(file,VerifiedCacheFile) for file in files)):
        raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_INVALID')
    by_ref={file.ref:file for file in files}
    if len(by_ref)!=len(files) or candidate.root not in by_ref or candidate.root in declared_inputs:
        raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_INVALID')
    schemas=_load_registry(SCHEMA_PATH);defs=schemas['$defs']
    operation=next(op for op in _load_registry(OPERATIONS_PATH)['operations'] if op['id']=='render.compose' and op['version']=='1')
    policies=operation['role_policies'];active=set();seen={};order=[];counts={};reached=set()
    def visit(ref,contract,role,depth):
        if depth>MAX_DEPTH:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_DEPTH')
        if ref not in by_ref:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_CLOSURE')
        reached.add(ref)
        if ref in declared_inputs:return
        if ref in active:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_CYCLE')
        if ref in seen:
            if seen[ref]!=(contract,role):raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_TYPE')
            return
        file=by_ref[ref];policy=policies.get(role)
        if (policy is None or file.role!=role or file.media_type not in policy['media_types']
                or file.bytes>policy['max_bytes']):raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_ROLE')
        counts[role]=counts.get(role,0)+1
        if counts[role]>policy['max_count']:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_COUNT')
        seen[ref]=(contract,role);active.add(ref);raw=None
        if file.media_type=='application/json':
            if file.bytes>MAX_JSON_BYTES or contract not in MODELS:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_TYPE')
            with file.path.open('rb') as document:
                raw=document.read(file.bytes+1)
            if len(raw)!=file.bytes or sha256(raw).hexdigest()!=ref.sha256:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_BYTES')
            value=parse_json(raw,max_bytes=MAX_JSON_BYTES)
            validator=Draft202012Validator({**schemas,'$ref':'#/$defs/'+contract},format_checker=FormatChecker())
            if canonical_bytes(value)!=raw or not validator.is_valid(value):raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_SCHEMA')
            model=MODELS[contract].model_validate_json(raw)
            if contract=='ImageReport' and model.status not in ('pass','review'):
                raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_QUALITY')
            for path,child in _schema_refs(defs[contract],value,defs,validator):
                if child in declared_inputs:
                    visit(child,'declared_input','',depth+1);continue
                binding=OUTPUT_BINDINGS.get((contract,'/'+'/'.join(path)))
                if binding is None:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_BINDING')
                visit(child,binding.contract,binding.role,depth+1)
        active.remove(ref);order.append(CachePublicationNode(file,contract,role,raw))
    visit(candidate.root,'ImageResult','document',0)
    if reached!=set(by_ref):raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_CLOSURE')
    for role,policy in policies.items():
        if policy['required'] and counts.get(role,0)==0:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_COUNT')
    if len(order)>operation['max_output_count']:
        raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_COUNT')
    return tuple(order)


def republish_render_cache(candidate,files,declared_inputs,artifacts,*,check_active=lambda:None,on_staging_start=lambda:None):
    """Copy binary bytes exactly, remap only new output refs in typed documents.

    Caller holds verified-file ownership, live generation and qualification.
    Uploads use normal attempt-scoped TaskArtifacts; result commit and adoption
    remain the existing executor/coordinator responsibility.
    """
    plan=plan_render_cache_publication(candidate,files,declared_inputs)
    mapping={};outputs=[];written=0;review=False
    old_ids={file.ref.artifact_id for file in files};published_ids={}
    def rewrite(value):
        if isinstance(value,dict):
            if set(value)=={'artifact_id','sha256'}:
                ref=ArtifactRef.parse(value)
                if ref in mapping:return mapping[ref].model_dump(mode='json')
            return {key:rewrite(child) for key,child in value.items()}
        if isinstance(value,list):return [rewrite(child) for child in value]
        return value
    for node in plan:
        check_active();file=node.file
        if node.document_jcs is not None:
            value=rewrite(parse_json(node.document_jcs,max_bytes=MAX_JSON_BYTES))
            model=MODELS[node.contract].model_validate_json(canonical_bytes(value))
            on_staging_start()
            reference=artifacts.stage_contract(model,node.role)
            size=len(canonical_bytes(model));expected=sha256(canonical_bytes(model)).hexdigest()
            if node.contract=='ImageReport':review=review or model.status=='review'
        else:
            with artifacts.scratch.reserve(file.bytes) as owner:
                path=owner.path('render-cache-output','.npy');digest=sha256();count=0
                with file.path.open('rb') as source,artifacts.scratch.open_writer(path,owner) as target:
                    while True:
                        check_active();block=source.read(65536)
                        if not block:break
                        count+=len(block)
                        if count>file.bytes:raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_BYTES')
                        digest.update(block);target.write(block)
                if count!=file.bytes or digest.hexdigest()!=file.ref.sha256:
                    raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_BYTES')
                on_staging_start()
                reference=artifacts.stage(path,owner,node.role,file.media_type)
            size=file.bytes;expected=file.ref.sha256
        if (not isinstance(reference,ContractRef) or reference.artifact_id.int==0
                or reference.artifact_id in old_ids or reference.sha256!=expected):
            raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_UPLOAD')
        identity=(reference.sha256,file.media_type,node.role)
        if reference.artifact_id in published_ids and published_ids[reference.artifact_id]!=identity:
            raise AdoptionInvalid('RENDER_CACHE_PUBLICATION_UPLOAD')
        published_ids[reference.artifact_id]=identity
        mapping[file.ref]=reference;outputs.append(reference);written+=size;check_active()
    return StagedRenderResult(mapping[candidate.root],tuple(outputs),written,review)
