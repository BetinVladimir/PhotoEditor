from photo_worker.contracts.style import Beauty,Defects
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.effective import EffectiveParams


def resolve_effective_params(resolved: ResolvedStyle) -> EffectiveParams:
    """Derive fresh parameters from the immutable resolved style exactly once.

    Color/local/region strength remains at the final aligned color blend.
    Geometry body_strength carries the common scale for body controls; face_slim
    is scaled directly. Effect adapters must not apply style strength again.
    """
    if not isinstance(resolved, ResolvedStyle):
        raise TypeError('Expected ResolvedStyle, not previously effective parameters')
    resolved.verify_hash()
    if resolved.mode == 'skip':
        raise ValueError('Skipped style has no effective operations')
    s = resolved.strength
    modules = resolved.modules.model_dump(mode='python')
    if modules['beauty']['enabled']:
        modules['beauty'] = effective_beauty_params(resolved.modules.beauty, s).model_dump(mode='python')
        # texture_keep is a preservation constraint, not effect intensity.
    if modules['defects']['enabled']:
        modules['defects'] = effective_defect_params(resolved.modules.defects, s).model_dump(mode='python')
    if modules['geometry']['enabled']:
        modules['geometry']['body_strength'] *= s
        modules['geometry']['face_slim'] *= s
        # Body controls are multiplied by body_strength by the field generator.
    ext = modules['extensions']
    detail = ext.get('portrait_detail')
    if detail and detail['enabled']:
        for field in ('teeth_strength', 'shine_strength', 'under_eye_strength'):
            detail[field] *= s
    restoration = ext.get('restoration')
    if restoration:
        restoration['denoise_strength'] *= s
    background = ext.get('background')
    if background:
        background['blur_strength'] *= s
    generation = ext.get('generation')
    if generation and generation['enabled']:
        generation['strength'] *= s
    return EffectiveParams(resolved_hash=resolved.hash,color_strength=s,modules=modules)


def effective_beauty_params(beauty: Beauty, strength: float) -> Beauty:
    """Scale intensity once; retain the texture preservation constraint."""
    if not isinstance(beauty, Beauty) or type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('INVALID_BEAUTY_STRENGTH')
    values=beauty.model_dump(mode='python')
    values['smoothing']*=strength
    values['tone_evenness']*=strength
    return Beauty.model_validate(values)


def effective_defect_params(defects: Defects, strength: float) -> Defects:
    """Scale repair intensity once, preserving policy and permanent-mark guards."""
    if not isinstance(defects, Defects) or type(strength) not in (int,float) or not 0<=strength<=1:
        raise ValueError('INVALID_DEFECT_STRENGTH')
    values=defects.model_dump(mode='python')
    values['strength']*=strength
    return Defects.model_validate(values)
