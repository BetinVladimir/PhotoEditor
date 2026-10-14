"""Ordered native color/beauty/background path with retained stage outcomes."""
from contextlib import contextmanager
from dataclasses import replace
import math
from photo_worker.contracts.style import StyleV2
from .render_portrait import prepare_portrait_base, portrait_provider_peak, PORTRAIT_MODULES
from .render_background import prepare_blur_stage,prepare_background_stage
from .render_effects import RenderEffectChain
from .render_modules import required_render_modules
from .render_protection import preserve_render_protection

PORTRAIT_BACKGROUND_MODULES=PORTRAIT_MODULES|frozenset({'background'})


def _portrait_style(style):
    if not required_render_modules(style.effective)<=PORTRAIT_BACKGROUND_MODULES:
        raise ValueError('RENDER_BACKGROUND_COMPLETE_PIPELINE_REQUIRED')
    background=style.effective.extensions.background
    if background is None or background.mode not in ('keep','blur','transparent','solid','asset'):
        raise ValueError('RENDER_BACKGROUND_MODE_PIPELINE_REQUIRED')
    data=style.effective.model_dump(mode='python',by_alias=True)
    data['extensions']['background']=None
    return replace(style,effective=StyleV2.model_validate(data))


def _bound_background(inputs,style,normalized):
    """Validate frozen identity against effective module and owned normalized pixels.

    Caller keeps normalize_frozen_background's context alive and admits decoder
    resources before entering it; this stage admits retained composition memory.
    """
    import numpy as np
    from .render_background_decode import NormalizedBackground
    from .render_background_binding import FrozenBackgroundAsset
    pin=getattr(inputs.recipe,'background_asset',None)
    if not isinstance(pin,FrozenBackgroundAsset) or not isinstance(normalized,NormalizedBackground):
        raise ValueError('RENDER_BACKGROUND_PIN_REQUIRED')
    image=normalized.image
    if (style.effective.extensions.background.asset_id!=pin.portable_asset_id
            or pin.style!=inputs.recipe.recipe.base_style
            or pin.work_icc_sha!=inputs.source.color.icc_sha256
            or image.source_sha256!=pin.source.sha256
            or image.profile_sha256!=pin.work_icc_sha
            or image.profile_id!='work_rec2020_gamma24_v1' or image.alpha_path is not None
            or type(image.width) is not int or type(image.height) is not int
            or min(image.width,image.height)<1 or image.width*image.height>200_000_000
            or not isinstance(normalized.rgb,np.ndarray) or normalized.rgb.dtype!=np.float32
            or normalized.rgb.shape!=(image.height,image.width,3) or normalized.rgb.flags.writeable):
        raise ValueError('RENDER_BACKGROUND_PIN_CHANGED')
    return normalized


def portrait_background_peak(inputs,style,faces,*,normalized_background=None,tile_size=512,max_roi_pixels=4194304):
    portrait=_portrait_style(style)
    peak=portrait_provider_peak(inputs.source,portrait,faces,
        has_protection=inputs.protection is not None,tile_size=tile_size,max_roi_pixels=max_roi_pixels)
    if style.effective.extensions.background.mode=='blur':
        h,w=inputs.source.canvas.height,inputs.source.canvas.width
        radius=math.ceil(.02*max(h,w)*style.effective.extensions.background.blur_strength)
        halo=math.ceil(4*max(radius/3,.5))+2 if radius else 0
        pixels=h*w;support=min(h,tile_size+2*halo)*min(w,tile_size+2*halo)
        peak['ram_bytes']+=32*pixels+256*support+128*1024*1024
        peak['scratch_bytes']+=12*pixels+8192
    elif style.effective.extensions.background.mode in ('transparent','solid','asset'):
        h,w=inputs.source.canvas.height,inputs.source.canvas.width
        pixels=h*w;tile=min(h,tile_size)*min(w,tile_size)
        peak['ram_bytes']+=32*pixels+256*tile+128*1024*1024
        peak['scratch_bytes']+=16*pixels+16384
    if style.effective.extensions.background.mode=='asset':
        normalized=_bound_background(inputs,style,normalized_background)
        background_pixels=normalized.image.width*normalized.image.height
        peak['ram_bytes']+=12*background_pixels+128*1024*1024
        peak['scratch_bytes']+=12*background_pixels+1048576
    elif normalized_background is not None:
        raise ValueError('RENDER_BACKGROUND_UNEXPECTED_INPUTS')
    if style.effective.extensions.background.mode!='keep' and inputs.protection is not None:
        peak['ram_bytes']+=16*pixels+16384
        peak['scratch_bytes']+=16*pixels+16384
    return peak


@contextmanager
def prepare_portrait_background_base(artifacts,inputs,style,original,masks,*,faces,
        person_reliable,execution_profile,blur_params=None,subject=None,depth=None,compose_params=None,
        normalized_background=None,
        permanent=None,features=None,coverage_counts=None,person_mask=None,region_masks=None,
        tile_size=512,max_roi_pixels=4194304,check_active=lambda:None):
    """Caller verifies mask/depth/face authority and qualifies complete runtime.

    Admit retained intermediates before allocating native output. Unsupported
    authored effects/modes are never omitted. Keep every owner through yield.
    """
    check_active()
    portrait=_portrait_style(style)
    background=style.effective.extensions.background
    normalized=(_bound_background(inputs,style,normalized_background) if background.mode=='asset' else None)
    if background.mode!='asset' and normalized_background is not None:
        raise ValueError('RENDER_BACKGROUND_UNEXPECTED_INPUTS')
    if background.mode=='blur':
        from photo_worker.contracts.background import BlurParams
        if (not isinstance(blur_params,BlurParams) or blur_params.strength!=background.blur_strength
                or blur_params.image!=inputs.recipe.recipe.source.source
                or blur_params.tile!=tile_size or subject is None or depth is None):
            raise ValueError('RENDER_BLUR_AUTHORED_PARAMS_CHANGED')
        if compose_params is not None:
            raise ValueError('RENDER_BACKGROUND_UNEXPECTED_INPUTS')
    elif background.mode in ('transparent','solid','asset'):
        from photo_worker.contracts.background import ComposeParams
        if (not isinstance(compose_params,ComposeParams) or compose_params.mode!=background.mode
                or compose_params.image!=inputs.recipe.recipe.source.source
                or compose_params.tile!=tile_size or subject is None
                or compose_params.solid_rgb!=(background.solid_rgb if background.mode=='solid' else None)
                or compose_params.shadow_opacity!=0 or blur_params is not None or depth is not None
                or (background.mode=='asset' and (compose_params.background!=inputs.recipe.background_asset.source
                    or compose_params.asset_scale!=1 or compose_params.position_x!=.5
                    or compose_params.position_y!=.5))):
            raise ValueError('RENDER_BACKGROUND_AUTHORED_PARAMS_CHANGED')
    elif any(value is not None for value in (blur_params,subject,depth,compose_params)):
        raise ValueError('RENDER_BACKGROUND_UNEXPECTED_INPUTS')
    from .render_prepare import admit_render_inputs
    peak=portrait_background_peak(inputs,style,faces,normalized_background=normalized,tile_size=tile_size,max_roi_pixels=max_roi_pixels)
    admit_render_inputs(inputs,execution_profile,peak,tile_size=tile_size)
    with prepare_portrait_base(artifacts,inputs,portrait,original,masks,faces=faces,
            person_reliable=person_reliable,execution_profile=execution_profile,permanent=permanent,
            features=features,coverage_counts=coverage_counts,person_mask=person_mask,
            region_masks=region_masks,tile_size=tile_size,max_roi_pixels=max_roi_pixels,
            check_active=check_active) as base:
        if background.mode=='keep':
            yield base
        else:
            stage=(prepare_blur_stage(artifacts,inputs.source,base.pixels,blur_params,subject,depth,
                    check_active=check_active) if background.mode=='blur' else
                prepare_background_stage(artifacts,inputs.source,base.pixels,compose_params,subject,
                    background_rgb=None if normalized is None else normalized.rgb,check_active=check_active))
            with stage as blurred:
                with preserve_render_protection(artifacts,original,blurred.pixels,
                        masks.protection if inputs.protection is not None else None,
                        tile_size=tile_size,check_active=check_active) as protected:
                    yield RenderEffectChain(protected,(base,blurred))
        check_active()


@contextmanager
def prepare_frozen_asset_background_base(artifacts,inputs,style,original,masks,*,
        registry,subject_ref,subject,retained_resources,faces,person_reliable,execution_profile,
        permanent=None,features=None,coverage_counts=None,person_mask=None,region_masks=None,
        tile_size=512,max_roi_pixels=4194304,check_active=lambda:None):
    """Normalize the pinned import and retain its owner through ordered rendering.

    Caller supplies verified subject pixels/ref and complete already-retained
    resource costs, plus qualified native runtime. No caller-authored compose
    params or normalized metadata can replace the frozen background selection.
    """
    from .render_background import render_frozen_compose_params
    from .render_background_decode import normalize_frozen_background
    from .render_recipe import decode_frozen_render_recipe
    from .render_inputs import FrozenRenderInputs
    from photo_worker.contracts.base import canonical_bytes
    check_active()
    if not isinstance(inputs,FrozenRenderInputs):
        raise ValueError('RENDER_BACKGROUND_INPUTS_INVALID')
    from copy import deepcopy
    style=deepcopy(style);execution_profile=deepcopy(execution_profile)
    retained_resources=deepcopy(retained_resources)
    _portrait_style(style)
    frozen=decode_frozen_render_recipe(canonical_bytes(inputs.recipe))
    if style.effective.extensions.background.mode!='asset':
        raise ValueError('RENDER_BACKGROUND_STYLE_INVALID')
    params=render_frozen_compose_params(style,frozen,subject_ref,tile_size=tile_size)
    # Own metadata while input/decode callbacks run; pixel owners remain caller's.
    from photo_worker.contracts.images import WorkingImage
    owned=replace(inputs,recipe=frozen,source=WorkingImage.model_validate_json(canonical_bytes(inputs.source)))
    from photo_worker.contracts.base import canonical_hash
    if (canonical_hash(owned.source.canvas)!=frozen.recipe.source.canvas_hash
            or owned.source.dtype!='float32'
            or owned.source.color.icc_sha256!=frozen.background_asset.work_icc_sha
            or params.subject.confidence<.75
            or params.subject.canvas!=owned.source.canvas
            or params.subject.transform_chain!=owned.source.transform_chain):
        raise ValueError('RENDER_BACKGROUND_SOURCE_INVALID')
    with normalize_frozen_background(artifacts,frozen.background_asset,registry,
            execution_profile=execution_profile,retained_resources=retained_resources,
            tile_size=tile_size,check_active=check_active) as normalized:
        with prepare_portrait_background_base(artifacts,owned,style,original,masks,
                faces=faces,person_reliable=person_reliable,execution_profile=execution_profile,
                compose_params=params,subject=subject,normalized_background=normalized,
                permanent=permanent,features=features,coverage_counts=coverage_counts,
                person_mask=person_mask,region_masks=region_masks,tile_size=tile_size,
                max_roi_pixels=max_roi_pixels,check_active=check_active) as rendered:
            yield rendered
            check_active()
