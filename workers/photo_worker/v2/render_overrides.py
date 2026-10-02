"""Preserve and source-validate full OverrideV1 documents for V2 composition."""
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.style import StyleV2
from .render_inputs import read_render_document

MODULES=('strength','global_','local','beauty','defects','geometry','extensions')


def validate_render_override_source(override,source):
    # OverrideV1 refers to original encoded-source SHA, not WorkingImage JSON.
    if override.input_sha256!=source.canvas.source_sha256:
        raise ValueError('RENDER_OVERRIDE_SOURCE_CHANGED')
    for binding in override.mask_refs:
        mask=binding.mask
        if mask.canvas!=source.canvas or mask.transform_chain.hash!=source.transform_chain.hash:
            raise ValueError('RENDER_OVERRIDE_MASK_CHANGED')


def load_render_overrides(artifacts,inputs,*,max_document_bytes:int,check_active=lambda:None)->tuple[OverrideV1,...]:
    """Retain all authored documents/decisions/landmarks, not just scalar edits.

    SQL must prove override origin and mask provenance. Pixel mappings and
    operation-specific execution are separate; this loads metadata only.
    """
    if type(max_document_bytes) is not int or not 1<=max_document_bytes<=134217728:
        raise ValueError('RENDER_OVERRIDE_BUDGET_INVALID')
    result=[];remaining=max_document_bytes;cache={}
    for ref in inputs.recipe.recipe.overrides:
        check_active();key=(ref.artifact_id,ref.sha256)
        if key not in cache:
            raw=read_render_document(artifacts,ref,check_active);remaining-=len(raw)
            if remaining<0:raise ValueError('RENDER_OVERRIDE_DOCUMENT_BUDGET')
            if canonical_bytes(parse_json(raw))!=raw:raise ValueError('RENDER_OVERRIDE_NONCANONICAL')
            override=OverrideV1.model_validate_json(raw)
            validate_render_override_source(override,inputs.source);cache[key]=override
        result.append(cache[key].model_copy(deep=True))
    check_active();return tuple(result)


def merge_render_override_modules(base:StyleV2,overrides:tuple[OverrideV1,...],source,*,check_active=lambda:None)->StyleV2:
    """Apply module replacement in authored order, as existing style.resolve.

    Later non-null module values win; absent modules retain the previous style.
    Returns a validated owned StyleV2. Mask/decision/landmark effects remain in
    the original OverrideV1 inputs for their dedicated composition stages.
    """
    result=base.model_copy(deep=True)
    for override in overrides:
        check_active();validate_render_override_source(override,source)
        patch={field:getattr(override.modules,field) for field in MODULES if getattr(override.modules,field) is not None}
        result=StyleV2.model_validate(result.model_copy(update=patch).model_dump(mode='json',by_alias=True))
    check_active();return result
