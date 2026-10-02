"""Scoped immutable MaskRef/NPY inputs for the probability expression evaluator."""
from contextlib import contextmanager,ExitStack
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.algorithms.layer_masks import MaskPlan


@contextmanager
def resolve_mask_inputs(plan:MaskPlan,artifacts,target:WorkingImage,*,check_active=lambda:None):
    """Materialize declared refs, verify complete NPYs, then yield tile resolver.

    Geometry mismatch requires explicit mask.transform in the graph. This source
    never infers correspondence merely from equal image dimensions/canvas hashes.
    All file reservations/mappings close on normal completion or any error.
    """
    with resolve_mask_input_plans((plan,),artifacts,target,check_active=check_active) as read_tile:
        yield read_tile


@contextmanager
def resolve_mask_input_plans(plans:tuple[MaskPlan,...],artifacts,target:WorkingImage,*,max_refs:int=256,check_active=lambda:None):
    """Share mappings across source-bound plans; full render adds one protection ref."""
    check_active()
    if (type(max_refs) is not int or not 1<=max_refs<=257 or not isinstance(plans,tuple) or len(plans)>33
            or any(not isinstance(plan,MaskPlan) or not 1<=len(plan.nodes)<=256 for plan in plans)
            or not isinstance(target,WorkingImage)):
        raise ValueError('MASK_PLAN_INVALID')
    nodes=tuple(node for plan in plans for node in plan.nodes if node.operation=='ref')
    if len({(node.ref.artifact_id,node.ref.sha256) for node in nodes if node.ref is not None})>max_refs:
        raise ValueError('MASK_INPUT_REF_BUDGET')
    canvas_hash=canonical_hash(target.canvas)
    h,w=target.canvas.height,target.canvas.width
    cache={}
    with ExitStack() as stack:
        for node in nodes:
            check_active()
            if node.operation!='ref':continue
            if node.ref is None:raise ValueError('MASK_DAG_INVALID')
            key=(node.ref.artifact_id,node.ref.sha256)
            if key in cache:continue
            mask=artifacts.contract(node.ref,MaskRef)
            if (mask.canvas!=target.canvas or canonical_hash(mask.canvas)!=canvas_hash or
                    mask.transform_chain.hash!=target.transform_chain.hash):
                raise ValueError('UNSUPPORTED_TRANSFORM')
            local=stack.enter_context(artifacts.input(mask.artifact,suffix='.npy'))
            array=stack.enter_context(read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
            cache[key]=array
        is_open=True
        def read_tile(ref,x,y,width,height):
            if not is_open:raise ValueError('MASK_INPUTS_CLOSED')
            check_active()
            if (any(type(value) is not int for value in (x,y,width,height)) or
                    min(x,y)<0 or min(width,height)<1 or x+width>w or y+height>h):
                raise ValueError('MASK_TILE_INVALID')
            key=(ref.artifact_id,ref.sha256)
            if key not in cache:raise ValueError('MASK_INPUT_UNDECLARED')
            return cache[key][y:y+height,x:x+width]
        check_active()
        try:
            yield read_tile
            check_active()
        finally:
            is_open=False
            cache.clear()
