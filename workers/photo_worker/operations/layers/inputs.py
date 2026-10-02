"""Join verified parent/mask inputs to attempt-owned layer preparation."""
from contextlib import contextmanager,ExitStack
from photo_worker.algorithms.layer_masks import compile_mask,evaluate_mask_tile
from photo_worker.runtime.arrays import read_array,read_alpha_array
from .alpha import layer_alpha
from .parents import resolve_layer_parents
from .mask_inputs import resolve_mask_inputs
from .prepare import prepare_layer


@contextmanager
def prepare_layer_inputs(params,artifacts,*,max_tile_bytes:int,tile_size:int=512,check_active=lambda:None,execution_profile=None):
    """Yield verified parents and closed output files while inputs remain owned.

    Legacy alpha conversion requires an explicit, resource-admitted profile.
    Null source_result still requires SQL original-snapshot admission.
    This helper does not publish artifacts or bypass task resource admission.
    """
    alpha_mode=(execution_profile or {}).get("layer_alpha_mode","require_float32")
    if alpha_mode not in ("require_float32","normalize_uint16"):raise ValueError("LAYER_ALPHA_MODE_INVALID")
    plan=compile_mask(params.mask,params.base.canvas_hash)
    refs={(node.ref.artifact_id,node.ref.sha256) for node in plan.nodes if node.operation=='ref'}
    if params.protected_mask is not None:refs.add((params.protected_mask.artifact_id,params.protected_mask.sha256))
    def preflight(source,proposal):
        if execution_profile is not None:
            from .resources import admit_layer,layer_tile_bytes
            admit_layer(execution_profile,source.canvas.width,source.canvas.height,
                mask_refs=len(refs),mask_nodes=len(plan.nodes),tile_size=tile_size)
            if max_tile_bytes<layer_tile_bytes(source.canvas.width,source.canvas.height,mask_nodes=len(plan.nodes),tile_size=tile_size):
                raise ValueError('LAYER_TILE_BUDGET_INVALID')
    with ExitStack() as inputs:
        parents=inputs.enter_context(resolve_layer_parents(params,artifacts,check_active=check_active,before_lineage=preflight))
        source=parents.source;h,w=source.canvas.height,source.canvas.width
        mask_reader=inputs.enter_context(resolve_mask_inputs(plan,artifacts,source,check_active=check_active))
        def mask(x,y,width,height):
            return evaluate_mask_tile(plan,mask_reader,x=x,y=y,width=width,height=height,
                max_tile_bytes=max_tile_bytes,check_active=check_active)
        protected=None
        if params.protected_mask is not None:
            expression={'op':'ref','mask':params.protected_mask.model_dump(mode='json'),'canvas_hash':params.base.canvas_hash}
            protection_plan=compile_mask(expression,params.base.canvas_hash)
            protection_reader=inputs.enter_context(resolve_mask_inputs(protection_plan,artifacts,source,check_active=check_active))
            def protected(x,y,width,height):return protection_reader(params.protected_mask,x,y,width,height)
        arrays={}
        def array(reference,shape,unit=False):
            if reference is None:return None
            key=(reference.artifact_id,reference.sha256,shape,unit)
            if key not in arrays:
                local=inputs.enter_context(artifacts.input(reference,suffix='.npy'))
                arrays[key]=inputs.enter_context(read_alpha_array(local.path,shape,check_active) if unit and alpha_mode=='normalize_uint16'
                    else read_array(local.path,shape,'float32',check_active,unit_interval=unit))
            return arrays[key]
        rgb=array(source.pixels,(h,w,3));proposal=array(parents.proposal.pixels,(h,w,3))
        alpha=inputs.enter_context(layer_alpha(array(source.alpha,(h,w),True),(h,w),artifacts.scratch,mode=alpha_mode,check_active=check_active))
        proposal_alpha=inputs.enter_context(layer_alpha(array(parents.proposal.alpha,(h,w),True),(h,w),artifacts.scratch,mode=alpha_mode,check_active=check_active))
        with prepare_layer(rgb,proposal,mask,artifacts.scratch,color=source.color,opacity=params.opacity,blend=params.blend,
                source_alpha=alpha.values,proposal_alpha=proposal_alpha.values,source_alpha_converted=alpha.requires_new_reference,read_protected=protected,
                read_source_generated=parents.read_source_generated,read_proposal_generated=parents.read_proposal_generated,
                source_generated_ref=parents.source_result.generated_mask if parents.source_result is not None else None,
                proposal_generated_ref=parents.proposal_result.generated_mask,
                max_tile_bytes=max_tile_bytes,tile_size=tile_size,check_active=check_active) as files:
            yield parents,files
