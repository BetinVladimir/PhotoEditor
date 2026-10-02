"""Conservative same-canvas composition costs plus qualified provider peaks."""


def render_resources(width,height,*,mask_refs,mask_nodes,provider_peak,tile_size=512,alpha_mode='require_float32'):
    """Provider peak includes base/proposal/model/geometry costs, never inferred.

    Original/base/preceding/next intermediates and final publication copies may
    coexist. Count mapped pages as RAM conservatively; shared masks count once.
    """
    if (any(type(v) is not int or v<1 for v in (width,height)) or width*height>200000000
            or type(mask_refs) is not int or not 0<=mask_refs<=257
            or type(mask_nodes) is not int or not 0<=mask_nodes<=512
            or type(tile_size) is not int or tile_size not in (256,512,1024)
            or alpha_mode not in ('require_float32','normalize_uint16')
            or not isinstance(provider_peak,dict) or set(provider_peak)!={'ram_bytes','scratch_bytes','vram_bytes','output_bytes'}
            or any(type(v) is not int or not 0<=v<=2**53-1 for v in provider_peak.values())):
        raise ValueError('RENDER_RESOURCES_INVALID')
    pixels=width*height;tile=min(width,tile_size)*min(height,tile_size)
    mapped=(112+4*mask_refs)*pixels+8192*(16+mask_refs)
    if alpha_mode=='normalize_uint16':mapped+=4*pixels+8192
    tiles=max(256,4*(mask_nodes+3))*tile
    required=dict(pixel_count=pixels,ram_bytes=mapped+tiles+256*1024*1024,
        scratch_bytes=mapped+64*1024*1024,output_bytes=24*pixels+32*1024*1024,vram_bytes=0)
    for key,value in provider_peak.items():required[key]+=value
    if any(value>2**53-1 for value in required.values()):raise ValueError('RENDER_RESOURCES_OVERFLOW')
    return required


def admit_render(profile,width,height,*,mask_refs,mask_nodes,provider_peak,tile_size=512,alpha_mode='require_float32'):
    """Validate sealed execution estimate; qualification is an independent gate."""
    from photo_worker.runtime.executor import OperationFailure
    required=render_resources(width,height,mask_refs=mask_refs,mask_nodes=mask_nodes,
        provider_peak=provider_peak,tile_size=tile_size,alpha_mode=alpha_mode)
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if (not isinstance(estimate,dict) or any(type(estimate.get(key)) is not int
            or not 0<=estimate[key]<=2**53-1 for key in required)
            or estimate['pixel_count']!=required['pixel_count']):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
                     ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<required[key]:raise OperationFailure(code)
    return required


def validate_render_execution_profile(profile, recipe, *, backend, runtime_sha,
                                      tile_size=512, alpha_mode='require_float32'):
    """Check admitted profile pins before providers, documents or pixel IO.

    Runtime/provider qualification and resource admission remain separate checks.
    Backend/runtime arguments come from worker configuration and approved pins.
    """
    import re
    from .render_recipe import FrozenRenderRecipe
    from photo_worker.runtime.executor import OperationFailure
    if (not isinstance(profile, dict) or not isinstance(recipe, FrozenRenderRecipe)
            or backend not in ('cpu', 'cuda', 'mps')
            or not isinstance(runtime_sha, str) or re.fullmatch('[0-9a-f]{64}', runtime_sha) is None
            or type(tile_size) is not int or tile_size not in (256, 512, 1024)
            or alpha_mode not in ('require_float32', 'normalize_uint16')
            or profile.get('backend') != backend or profile.get('renderer_version') != 'v2.2'
            or profile.get('render_runtime_sha') != runtime_sha
            or profile.get('model_lock_sha') != recipe.scalars.model_lock_sha
            or profile.get('icc_lock_sha') != recipe.scalars.icc_lock_sha
            or type(profile.get('tile_size')) is not int or profile['tile_size'] != tile_size
            or profile.get('alpha_mode') != alpha_mode):
        raise OperationFailure('CONFIG_INVALID')
