"""Shared bounded mask mappings for complete same-canvas layer composition."""
from contextlib import contextmanager
from dataclasses import dataclass
from photo_worker.algorithms.layer_masks import compile_mask,evaluate_mask_tile
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.layers.mask_inputs import resolve_mask_input_plans


@dataclass(frozen=True)
class RenderMaskTiles:
    layer:object
    protection:object


@contextmanager
def resolve_render_masks(artifacts,inputs,*,max_mask_bytes:int,max_tile_bytes:int,check_active=lambda:None):
    """Resource admission precedes IO; unique MaskRefs share one NPY mapping.

    Active layer masks and optional protection are evaluated on the verified
    current source canvas. Geometry changes require an explicit transformed
    source/mask stage; this resolver never infers coordinate correspondence.
    """
    if any(type(v) is not int or not 1<=v<=2**53-1 for v in (max_mask_bytes,max_tile_bytes)):
        raise ValueError('RENDER_MASK_BUDGET_INVALID')
    check_active();canvas_hash=canonical_hash(inputs.source.canvas);plans={}
    for layer in inputs.layers.layers if inputs.layers is not None else ():
        if layer.enabled and layer.opacity>0:plans[layer.id]=compile_mask(layer.mask,canvas_hash)
    protected=None
    if inputs.recipe.recipe.protected_mask is not None:
        if inputs.protection is None or not inputs.protection.protected:raise ValueError('RENDER_PROTECTION_MISSING')
        protected=compile_mask({'op':'ref','mask':inputs.recipe.recipe.protected_mask.model_dump(mode='json'),'canvas_hash':canvas_hash},canvas_hash)
    inventory=tuple(plans.values())+((protected,) if protected is not None else ())
    refs={(n.ref.artifact_id,n.ref.sha256) for p in inventory for n in p.nodes if n.ref is not None}
    required=len(refs)*(inputs.source.canvas.width*inputs.source.canvas.height*4+8192)
    if required>max_mask_bytes:raise ValueError('RENDER_MASK_INPUT_BUDGET')
    with resolve_mask_input_plans(inventory,artifacts,inputs.source,max_refs=257,check_active=check_active) as read:
        def evaluate(plan,x,y,width,height):
            return evaluate_mask_tile(plan,read,x=x,y=y,width=width,height=height,max_tile_bytes=max_tile_bytes,check_active=check_active)
        def layer_tile(id,x,y,width,height):
            if id not in plans:raise ValueError('RENDER_MASK_LAYER_UNAVAILABLE')
            return evaluate(plans[id],x,y,width,height)
        def protected_tile(x,y,width,height):
            if protected is None:return None
            return evaluate(protected,x,y,width,height)
        yield RenderMaskTiles(layer_tile,protected_tile)
