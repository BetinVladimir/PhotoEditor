"""Own and hash every cached closure byte before reuse preparation."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes,parse_json
from .adoption import AdoptionInvalid,ArtifactRef,MAX_JSON_BYTES
from .render_cache_lookup import RenderCacheEntry


@dataclass(frozen=True)
class VerifiedCacheFile:
    ref: ArtifactRef
    path: Path
    media_type: str
    role: str | None
    bytes: int


@contextmanager
def verified_render_cache_files(candidate,reader,scratch,*,max_bytes,check_active=lambda:None):
    """Reader must use the generation-scoped SQL manifest and pinned versions.

    All files are quota-owned until context exit; no output is staged here.
    Hash verification is necessary, not typed Render or quality acceptance.
    """
    if (not isinstance(candidate,RenderCacheEntry) or type(max_bytes) is not int or max_bytes<1
            or not isinstance(candidate.inventory_jcs,bytes) or len(candidate.inventory_jcs)>16777216):
        raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
    inventory=parse_json(candidate.inventory_jcs,max_bytes=16777216)
    if (not isinstance(inventory,list) or not 1<=len(inventory)<=30001
            or canonical_bytes(inventory)!=candidate.inventory_jcs):
        raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
    refs=[];seen=set();metadata=[];total=0
    for ordinal,item in enumerate(inventory):
        if (not isinstance(item,dict) or set(item)!={'artifact_id','sha256','purpose','ordinal'}
                or type(item['ordinal']) is not int or item['ordinal']!=ordinal):
            raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
        check_active();ref=ArtifactRef.parse({k:item[k] for k in ('artifact_id','sha256')})
        if ref in seen:continue
        evidence=reader.evidence(ref)
        if (evidence.ref!=ref or evidence.source!='cache' or type(evidence.bytes) is not int
                or evidence.bytes<1 or evidence.media_type not in ('application/json','application/x-npy')):
            raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
        total+=evidence.bytes
        if total>max_bytes:raise AdoptionInvalid('RENDER_CACHE_BYTES_LIMIT')
        if evidence.media_type=='application/json' and evidence.bytes>MAX_JSON_BYTES:
            raise AdoptionInvalid('RENDER_CACHE_BYTES_LIMIT')
        refs.append(ref);seen.add(ref);metadata.append(evidence)
    if not refs or refs[0]!=candidate.root:raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
    with scratch.reserve(total) as owner:
        files=[]
        for evidence in metadata:
            check_active();suffix='.json' if evidence.media_type=='application/json' else '.npy'
            path=owner.path('render-cache',suffix);count=0;digest=sha256()
            iterator=iter(reader.iter_bytes(evidence.ref,evidence.bytes))
            try:
                with scratch.open_writer(path,owner) as target:
                    for block in iterator:
                        check_active()
                        if not isinstance(block,bytes) or not 1<=len(block)<=65536 or count+len(block)>evidence.bytes:
                            raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
                        count+=len(block);digest.update(block);target.write(block)
                if count!=evidence.bytes or digest.hexdigest()!=evidence.ref.sha256:
                    raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
                owner.validate_file(path)
                if suffix=='.json':
                    with path.open('rb') as document:
                        raw=document.read(evidence.bytes+1)
                    if len(raw)!=evidence.bytes or sha256(raw).hexdigest()!=evidence.ref.sha256:
                        raise AdoptionInvalid('RENDER_CACHE_BYTES_INVALID')
                    if canonical_bytes(parse_json(raw,max_bytes=MAX_JSON_BYTES))!=raw:
                        raise AdoptionInvalid('RENDER_CACHE_DOCUMENT_NONCANONICAL')
                files.append(VerifiedCacheFile(evidence.ref,path,evidence.media_type,evidence.role,evidence.bytes))
            finally:
                close=getattr(iterator,'close',None)
                if close is not None:close()
        check_active();yield tuple(files);check_active()
