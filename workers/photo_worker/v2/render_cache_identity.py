"""Tenant-scoped content identity for qualified independent Render replay."""
from dataclasses import asdict
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from .adoption import AdoptionInvalid,ArtifactRef
from .render_qualification_pin import RenderQualificationPin


def render_cache_context(tenant_id,recipe,profile,parent_roots,pin):
    """No reuse authorization: callers must still recheck scope and byte proof.

    The task-bound transport manifest reference is excluded. All semantic
    profile, closure entry hash/count and contract pins remain included.
    Recipe content SHA avoids depending on its publication artifact UUID.
    """
    if (not isinstance(tenant_id,UUID) or tenant_id.int==0 or not isinstance(recipe,ArtifactRef)
            or not isinstance(profile,dict) or not isinstance(parent_roots,tuple) or len(parent_roots)>33
            or not isinstance(pin,RenderQualificationPin)):
        raise AdoptionInvalid('RENDER_CACHE_IDENTITY_INVALID')
    if (profile.get('renderer_version')!='v2.2'
            or (profile.get('backend'),profile.get('model_lock_sha'),profile.get('render_runtime_sha'))
            != (pin.backend,pin.model_lock_sha,pin.runtime_lock_sha)):
        raise AdoptionInvalid('RENDER_CACHE_IDENTITY_INVALID')
    refs=[];seen=set()
    for ref in (recipe,*parent_roots):
        if (not isinstance(ref,ArtifactRef) or not isinstance(ref.artifact_id,UUID) or ref.artifact_id.int==0 or not isinstance(ref.sha256,str)
                or len(ref.sha256)!=64 or any(c not in '0123456789abcdef' for c in ref.sha256)):
            raise AdoptionInvalid('RENDER_CACHE_IDENTITY_INVALID')
    for ref in parent_roots:
        if ref.artifact_id in seen:raise AdoptionInvalid('RENDER_CACHE_IDENTITY_INVALID')
        seen.add(ref.artifact_id);refs.append({'artifact_id':str(ref.artifact_id),'sha256':ref.sha256})
    value=dict(schema_version=2,tenant_id=str(tenant_id),operation='render.compose',version='1',
        params_sha256=recipe.sha256,ordered_parent_roots=refs,execution_profile={k:v for k,v in profile.items() if k!='v2_input_manifest'},qualification=asdict(pin))
    try:
        if len(canonical_bytes(value))>1048576:raise ValueError('too large')
        return value
    except (TypeError,ValueError,OverflowError) as error:
        raise AdoptionInvalid('RENDER_CACHE_IDENTITY_INVALID') from error


def render_cache_identity(tenant_id,recipe,profile,parent_roots,pin):
    return canonical_hash(render_cache_context(tenant_id,recipe,profile,parent_roots,pin))
