"""Join admitted source, masks, qualified proposals and final change evidence."""
from contextlib import contextmanager
from dataclasses import dataclass
from .render_reports import RenderReportEvidence,collect_render_parent_reports
from photo_worker.operations.optical.contracts import ImageResult
from .render_source import load_render_source_pixels
from .render_masks import resolve_render_masks
from .render_composition import RenderComposition,compose_render_pixels
from .render_effects import RenderEffectEvidence,unpack_render_base


@dataclass(frozen=True)
class PreparedRender:
    composition:RenderComposition
    alpha_requires_new_reference:bool
    source_reports:RenderReportEvidence
    effect_evidence:RenderEffectEvidence|None=None


@contextmanager
def prepare_render_pixels(artifacts,inputs,layers,render_base,render_proposal,*,
                          max_input_bytes:int,max_mask_bytes:int,max_tile_bytes:int,
                          alpha_mode='require_float32',tile_size:int=512,base_with_masks=False,source_result=None,execution_profile=None,provider_peak=None,check_active=lambda:None):
    """Retain all input/proposal/output owners until caller finishes staging.

    Caller supplies admitted metadata, qualified typed layers and a base-style
    context factory (inputs, original pixels). This is the same-canvas phase;
    geometry, base-style/operator qualification and graph adoption are upstream.
    Never silently omit or substitute a frozen authored layer.
    """
    check_active()
    authored=tuple(inputs.layers.layers) if inputs.layers is not None else ()
    if not isinstance(layers,tuple) or tuple(item.layer for item in layers)!=authored:
        raise ValueError('RENDER_PREPARED_LAYER_INVENTORY_CHANGED')
    if not callable(render_base):raise ValueError('RENDER_BASE_PROVIDER_INVALID')
    geometry={'optical.lens','optical.perspective','optical.panorama','optical.focus_stack','image.upscale'}
    if any(layer.enabled and layer.opacity>0 and layer.operation in geometry for layer in authored):
        raise ValueError('RENDER_GEOMETRY_STAGE_REQUIRED')
    if execution_profile is not None:
        admit_render_inputs(inputs,execution_profile,provider_peak,tile_size=tile_size,alpha_mode=alpha_mode)
    reports=RenderReportEvidence((),False)
    if source_result is not None:
        binding=inputs.recipe.recipe.source
        if not isinstance(source_result,ImageResult) or source_result.working_image!=binding.source:
            raise ValueError('RENDER_SOURCE_PARENT_CHANGED')
        reports=collect_render_parent_reports(artifacts,(source_result,),binding,check_active=check_active)
    with load_render_source_pixels(artifacts,inputs.source,max_input_bytes=max_input_bytes,
            alpha_mode=alpha_mode,source_result=source_result,source_ref=inputs.recipe.recipe.source.source if source_result is not None else None,
            check_active=check_active) as source:
        with resolve_render_masks(artifacts,inputs,max_mask_bytes=max_mask_bytes,
                max_tile_bytes=max_tile_bytes,check_active=check_active) as masks:
            base_context=(render_base(inputs,source.pixels,masks) if base_with_masks
                else render_base(inputs,source.pixels))
            with base_context as base:
                base,effect_evidence=unpack_render_base(base)
                with compose_render_pixels(source.pixels,base,layers,masks,artifacts.scratch,render_proposal,
                        color=inputs.source.color,max_tile_bytes=max_tile_bytes,tile_size=tile_size,
                        check_active=check_active) as composition:
                    if composition.pixels.effects:
                        from .render_effects import RenderCombinedEffects,effect_review_required
                        effect_evidence=RenderCombinedEffects(effect_evidence,composition.pixels.effects)
                        effect_review_required(effect_evidence)
                    yield PreparedRender(composition,source.alpha_requires_new_reference,reports,effect_evidence)
                    check_active()

def admit_render_inputs(inputs, execution_profile, provider_peak, *, tile_size=512, alpha_mode="require_float32"):
    """Check sealed full-composition resource bounds before document/pixel IO."""
    from photo_worker.algorithms.layer_masks import compile_mask
    from photo_worker.contracts.base import canonical_hash
    from .render_resources import admit_render
    authored=tuple(inputs.layers.layers) if inputs.layers is not None else ()
    canvas_hash=canonical_hash(inputs.source.canvas)
    plans=[compile_mask(layer.mask,canvas_hash) for layer in authored if layer.enabled and layer.opacity>0]
    protection=inputs.recipe.recipe.protected_mask
    if protection is not None:
        plans.append(compile_mask({'op':'ref','mask':protection.model_dump(mode='json'),'canvas_hash':canvas_hash},canvas_hash))
    refs={(node.ref.artifact_id,node.ref.sha256) for plan in plans for node in plan.nodes if node.ref is not None}
    admit_render(execution_profile,inputs.source.canvas.width,inputs.source.canvas.height,
        mask_refs=len(refs),mask_nodes=sum(len(plan.nodes) for plan in plans),provider_peak=provider_peak,
        tile_size=tile_size,alpha_mode=alpha_mode)

