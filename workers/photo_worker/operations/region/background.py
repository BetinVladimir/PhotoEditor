"""Native background stages retain Apply mask/alpha/depth invariants."""
from photo_worker.contracts.base import canonical_bytes
from contextlib import contextmanager,ExitStack
from photo_worker.contracts.background import ComposeParams,BlurParams
from photo_worker.operations.background.prepare import prepare_background
from photo_worker.operations.background.prepare_blur import prepare_blur
from photo_worker.runtime.arrays import read_array
from .geometry_native import GeometryPixels,_alpha16


def background_entry(effective,verified):
    requested=effective.extensions.background
    mode='keep' if requested is None else requested.mode
    entries=[entry for entry in verified if entry[0].operation in ('background.compose','background.blur')]
    if mode=='generate':
        if entries:raise ValueError('REGION_BACKGROUND_STAGE_COVERAGE_MISMATCH')
        return None
    if len(entries)!=int(mode!='keep'):raise ValueError('REGION_BACKGROUND_STAGE_COVERAGE_MISMATCH')
    if not entries:return None
    stage,raw,source,target,_=entries[0]
    params=(BlurParams if stage.operation=='background.blur' else ComposeParams).model_validate_json(canonical_bytes(raw))
    if params.subject.canvas!=source.canvas or params.subject.transform_chain!=source.transform_chain or params.subject.confidence<.75:
        raise ValueError('REGION_BACKGROUND_SUBJECT_INVALID')
    if mode=='blur':
        if (stage.operation!='background.blur' or params.strength!=requested.blur_strength*effective.strength
                or params.depth.canvas!=source.canvas or params.depth.transform_chain!=source.transform_chain
                or params.depth.invalid_mask is not None or target.alpha!=source.alpha):
            raise ValueError('REGION_BACKGROUND_BLUR_POLICY_MISMATCH')
    else:
        if (stage.operation!='background.compose' or params.mode!=mode or params.solid_rgb!=requested.solid_rgb
                or params.asset_scale!=1 or params.position_x!=.5 or params.position_y!=.5
                or params.shadow_opacity!=0 or params.shadow_blur_fraction!=0
                or params.shadow_offset_x!=0 or params.shadow_offset_y!=0
                or (target.alpha is not None)!=(mode=='transparent')):
            raise ValueError('REGION_BACKGROUND_COMPOSE_POLICY_MISMATCH')
        if mode=='asset' and (stage.background_asset is None or stage.background_asset.portable_asset_id!=requested.asset_id
                or entries[0][4].get('background_image') is None):
            raise ValueError('REGION_BACKGROUND_ASSET_ORIGIN_REQUIRED')
    return stage,params,source,target


@contextmanager
def prepare_region_background(artifacts,effective,verified,rgb,alpha,metadata,*,check_active=lambda:None):
    entry=background_entry(effective,verified)
    if entry is None:
        yield GeometryPixels(rgb,alpha,metadata.canvas,metadata.transform_chain);check_active();return
    stage,params,source,target=entry
    if source.canvas!=metadata.canvas or source.transform_chain!=metadata.transform_chain:
        raise ValueError('REGION_BACKGROUND_SOURCE_GEOMETRY_MISMATCH')
    with ExitStack() as scopes:
        def mapped(ref,shape,dtype='float32',unit=True):
            artifacts.require_declared_input(ref)
            local=scopes.enter_context(artifacts.input(ref,suffix='.npy'))
            return scopes.enter_context(read_array(local.path,shape,dtype,check_active,unit_interval=unit))
        subject=mapped(params.subject.artifact,rgb.shape[:2])
        if isinstance(params,BlurParams):
            depth=mapped(params.depth.artifact,rgb.shape[:2])
            prepared=scopes.enter_context(prepare_blur(rgb,subject,depth,artifacts.scratch,
                strength=params.strength,tile=params.tile,check_active=check_active))
            pixels=scopes.enter_context(read_array(prepared.rgb,rgb.shape,'float32',check_active,unit_interval=True))
            yield GeometryPixels(pixels,alpha,target.canvas,target.transform_chain)
        else:
            background_rgb=None
            if params.mode=='asset':
                actual=next(item[4]['background_image'] for item in verified if item[0]==stage)
                background_rgb=mapped(actual.pixels,(actual.canvas.height,actual.canvas.width,3))
            prepared=scopes.enter_context(prepare_background(rgb,subject,_alpha16(alpha),background_rgb,params,
                artifacts.scratch,check_active=check_active,alpha_mode='uint16'))
            pixels=scopes.enter_context(read_array(prepared.rgb,rgb.shape,'float32',check_active,unit_interval=True))
            output_alpha=None if prepared.alpha is None else scopes.enter_context(read_array(prepared.alpha,rgb.shape[:2],'uint16',check_active))
            yield GeometryPixels(pixels,None if output_alpha is None else output_alpha.astype('float32')/65535,target.canvas,target.transform_chain)
        check_active()
