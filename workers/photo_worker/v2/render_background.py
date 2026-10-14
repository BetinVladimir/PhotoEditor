"""Same-canvas native background blur with retained measured depth evidence."""
from contextlib import contextmanager
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.background import BlurParams
from photo_worker.operations.background.prepare_blur import prepare_blur
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels


@dataclass(frozen=True)
class RenderBlurStage:
    pixels: AdjustmentPixels
    subject_confidence: float
    subject_depth: float
    strength: float
    maximum_radius: int
    review_required: bool


@contextmanager
def prepare_blur_stage(artifacts, source, current, params, subject, depth, *,
                       check_active=lambda: None):
    """Caller verifies params/mask/depth refs and qualifies complete resources.

    Operates on preceding same-canvas pixels; does not publish an intermediate
    image or waive final quality/provenance checks. Alpha/generated ownership
    is retained without conversion or quantization.
    """
    check_active()
    if not isinstance(params, BlurParams):
        raise ValueError('RENDER_BLUR_PARAMS_INVALID')
    params = BlurParams.model_validate_json(canonical_bytes(params))
    if (source.dtype != 'float32' or source.color.profile_id != 'work_rec2020_gamma24_v1'
            or params.subject.canvas != source.canvas
            or params.subject.transform_chain != source.transform_chain
            or params.depth.canvas != source.canvas
            or params.depth.transform_chain != source.transform_chain
            or params.depth.invalid_mask is not None
            or params.subject.confidence < .75
            or current.rgb.shape != (source.canvas.height, source.canvas.width, 3)):
        raise ValueError('RENDER_BLUR_SOURCE_INVALID')
    with prepare_blur(current.rgb, subject, depth, artifacts.scratch,
            strength=params.strength, tile=params.tile, check_active=check_active) as files:
        with read_array(files.rgb, current.rgb.shape, 'float32', check_active,
                unit_interval=True) as rgb:
            check_active()
            yield RenderBlurStage(AdjustmentPixels(rgb, current.alpha, current.generated),
                params.subject.confidence, files.subject_depth, params.strength,
                max(files.radii), params.subject.confidence < .9)
            check_active()


def render_blur_params(prepared_style, image_ref, subject, depth, *, tile_size=512):
    """Materialize authored blur settings without inventing a depth/mask ref.

    Global color strength is not applied to the explicit background strength;
    this matches the existing Apply background operation contract.
    """
    background = prepared_style.effective.extensions.background
    if background is None or background.mode != 'blur':
        raise ValueError('RENDER_BLUR_STYLE_INVALID')
    return BlurParams(image=image_ref, subject=subject, depth=depth,
        strength=background.blur_strength, tile=tile_size)


@dataclass(frozen=True)
class RenderComposeStage:
    pixels: AdjustmentPixels
    mode: str
    subject_confidence: float
    review_required: bool


@contextmanager
def prepare_background_stage(artifacts,source,current,params,subject,*,background_rgb=None,
                             check_active=lambda:None):
    """Verified same-canvas compose preserving float alpha through Render."""
    from photo_worker.contracts.background import ComposeParams
    from photo_worker.operations.background.prepare import prepare_background
    check_active()
    if not isinstance(params,ComposeParams):
        raise ValueError('RENDER_BACKGROUND_PARAMS_INVALID')
    params=ComposeParams.model_validate_json(canonical_bytes(params))
    if (source.dtype!='float32' or source.color.profile_id!='work_rec2020_gamma24_v1'
            or params.subject.canvas!=source.canvas
            or params.subject.transform_chain!=source.transform_chain
            or params.subject.confidence<.75
            or current.rgb.shape!=(source.canvas.height,source.canvas.width,3)):
        raise ValueError('RENDER_BACKGROUND_SOURCE_INVALID')
    with prepare_background(current.rgb,subject,current.alpha,background_rgb,params,
            artifacts.scratch,alpha_mode='float32',check_active=check_active) as files:
        with read_array(files.rgb,current.rgb.shape,'float32',check_active,unit_interval=True) as rgb:
            if files.alpha is None:
                yield RenderComposeStage(AdjustmentPixels(rgb,None,current.generated),params.mode,
                    params.subject.confidence,params.subject.confidence<.9)
            else:
                with read_array(files.alpha,current.rgb.shape[:2],'float32',check_active,
                        unit_interval=True) as alpha:
                    yield RenderComposeStage(AdjustmentPixels(rgb,alpha,current.generated),params.mode,
                        params.subject.confidence,params.subject.confidence<.9)
            check_active()


def render_compose_params(prepared_style,image_ref,subject,*,tile_size=512):
    """Bind non-asset authored background without inventing asset authority."""
    from photo_worker.contracts.background import ComposeParams
    background=prepared_style.effective.extensions.background
    if background is None or background.mode not in ('transparent','solid'):
        raise ValueError('RENDER_BACKGROUND_STYLE_INVALID')
    return ComposeParams(image=image_ref,subject=subject,mode=background.mode,
        solid_rgb=background.solid_rgb if background.mode=='solid' else None,tile=tile_size)


def render_frozen_compose_params(prepared_style,frozen,subject,*,tile_size=512):
    """Materialize authored compose inputs from the complete immutable recipe.

    Live SQL acceptance/closure authority and normalized pixels remain upstream.
    Revalidate an owned recipe so post-validation model mutations cannot replace
    the base style, ICC or source; match the effective override module exactly.
    """
    from .render_recipe import FrozenRenderRecipe,decode_frozen_render_recipe
    from photo_worker.contracts.background import ComposeParams
    if not isinstance(frozen,FrozenRenderRecipe):
        raise ValueError('RENDER_BACKGROUND_RECIPE_INVALID')
    frozen=decode_frozen_render_recipe(canonical_bytes(frozen))
    background=prepared_style.effective.extensions.background
    if background is None:
        raise ValueError('RENDER_BACKGROUND_STYLE_INVALID')
    if background.mode!='asset':
        return render_compose_params(prepared_style,frozen.recipe.source.source,subject,tile_size=tile_size)
    pin=frozen.background_asset
    if pin is None or pin.portable_asset_id!=background.asset_id:
        raise ValueError('RENDER_BACKGROUND_PIN_CHANGED')
    return ComposeParams(image=frozen.recipe.source.source,subject=subject,mode='asset',
        background=pin.source,tile=tile_size)


@contextmanager
def load_background_asset(artifacts,source,background_ref,*,check_active=lambda:None):
    """Load verified opaque working background; portable-ID resolution is upstream.

    TaskArtifacts independently enforces declared immutable artifact inputs.
    Keep this context alive through native composition and final publication.
    """
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.base import ArtifactRef
    check_active()
    if not isinstance(background_ref,ArtifactRef):
        raise ValueError('RENDER_BACKGROUND_ASSET_PIN_INVALID')
    image=artifacts.contract(background_ref,WorkingImage)
    if (image.dtype!='float32' or image.color!=source.color or image.alpha is not None
            or not 1<=image.canvas.width*image.canvas.height<=200000000):
        raise ValueError('RENDER_BACKGROUND_ASSET_INVALID')
    # Own metadata so callbacks cannot change the shape/ref after validation.
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    with artifacts.input(image.pixels,suffix='.npy') as local:
        with read_array(local.path,(image.canvas.height,image.canvas.width,3),'float32',
                check_active,unit_interval=True) as rgb:
            check_active()
            yield image,rgb
            check_active()
