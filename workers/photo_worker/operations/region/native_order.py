"""Refuse accepted stages the installed Region pipeline cannot reproduce."""


def validate_native_region_order(effective,verified,*,model_lock_sha,original_style=None):
    ranks={'image.denoise':0,'geometry.body':1,'image.color':2,'sensor.dust_auto_repair':3,'sensor.dust_repair':3,'image.local':4,
        'portrait.beauty':5,'portrait.detail':6,'defects.auto_repair':7,'defects.repair':7,'background.compose':8,'background.blur':8,
        'image.inpaint':9,'geometry.crop':11,'image.outpaint':11,'image.upscale':12}
    original=effective if original_style is None else original_style
    previous=-1;seen=set()
    for stage,params,_,_,policy in verified:
        rank=ranks.get(stage.operation)
        if stage.operation=='image.generate':
            rank={'background':8,'generate':10}.get(stage.phase)
        if rank is None:raise ValueError('REGION_NATIVE_STAGE_PROVIDER_UNAVAILABLE:'+stage.operation)
        if rank<previous or rank in seen:raise ValueError('REGION_NATIVE_STAGE_ORDER_INVALID')
        seen.add(rank);previous=rank
        if stage.operation=='portrait.beauty':
            resolved=policy.get('resolved')
            if not effective.beauty.enabled or resolved is None or resolved.modules.beauty!=original.beauty or resolved.strength!=effective.strength:
                raise ValueError('REGION_BEAUTY_EFFECTIVE_POLICY_MISMATCH')
        if stage.operation=='image.denoise':
            restoration=effective.extensions.restoration
            if (restoration is None or restoration.denoise=='off' or params['model_lock_id']!=model_lock_sha
                    or params.get('model_id','scunet_color_real_psnr')!=restoration.denoise
                    or params['strength']!=restoration.denoise_strength*effective.strength):
                raise ValueError('REGION_DENOISE_EFFECTIVE_POLICY_MISMATCH')
    restoration=effective.extensions.restoration
    if (5 in seen)!=bool(original.beauty.enabled):raise ValueError('REGION_BEAUTY_STAGE_COVERAGE_MISMATCH')
    if (0 in seen)!=bool(restoration is not None and restoration.denoise!='off'):
        raise ValueError('REGION_DENOISE_STAGE_COVERAGE_MISMATCH')
