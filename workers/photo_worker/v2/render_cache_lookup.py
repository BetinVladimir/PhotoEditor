"""Read-only candidate lookup; never substitutes for byte proof or publication."""
from dataclasses import dataclass
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid, ArtifactRef
from .render_adoption_inputs import _reference
from .render_cache_identity import render_cache_context, render_cache_identity


@dataclass(frozen=True)
class RenderCacheEntry:
    result_id: UUID
    root: ArtifactRef
    inventory_jcs: bytes


def lookup_render_cache(connection, tenant_id, task_id, attempt, fence, inputs):
    """Return a scoped immutable candidate; caller must verify pinned bytes again."""
    profile=inputs.execution_profile()
    body=canonical_bytes(render_cache_context(tenant_id,inputs.recipe,profile,
                                             inputs.parent_roots,inputs.qualification_pin)).decode()
    key=render_cache_identity(tenant_id,inputs.recipe,profile,inputs.parent_roots,inputs.qualification_pin)
    row=connection.execute('SELECT * FROM photo.lookup_v26_render_cache(%s,%s,%s,%s,%s,%s)',
                           (tenant_id,task_id,attempt,fence,key,body)).fetchone()
    return decode_render_cache_entry(row)


def decode_render_cache_entry(row):
    if row is None:return None
    if not isinstance(row,(tuple,list)) or len(row)!=3:
        raise AdoptionInvalid('RENDER_CACHE_ENTRY_INVALID')
    try:
        result=row[0] if isinstance(row[0],UUID) else UUID(row[0])
        root=row[1];inventory=row[2]
        if result.int==0 or not isinstance(root,dict) or set(root)!={'artifact_id','sha256'}:
            raise ValueError('root')
        ref=_reference(root['artifact_id'],root['sha256'])
        if not isinstance(inventory,list) or not 1<=len(inventory)<=30001:raise ValueError('inventory')
        for ordinal,entry in enumerate(inventory):
            if not isinstance(entry,dict) or set(entry)!={'artifact_id','sha256','purpose','ordinal'}:
                raise ValueError('entry')
            _reference(entry['artifact_id'],entry['sha256'])
            purpose=entry['purpose']
            if (type(entry['ordinal']) is not int or entry['ordinal']!=ordinal or not isinstance(purpose,str)
                    or len(purpose)!=64 or any(c not in '0123456789abcdef' for c in purpose)):
                raise ValueError('ordinal/purpose')
        if inventory[0]['artifact_id']!=str(ref.artifact_id) or inventory[0]['sha256']!=ref.sha256:
            raise ValueError('root mismatch')
        encoded=canonical_bytes(inventory)
        if len(encoded)>16777216:raise ValueError('size')
        return RenderCacheEntry(result,ref,encoded)
    except (TypeError,ValueError,AttributeError,OverflowError) as error:
        raise AdoptionInvalid('RENDER_CACHE_ENTRY_INVALID') from error
