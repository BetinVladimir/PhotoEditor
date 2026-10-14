"""Same-canvas holdout backgrounds using production linear-light math."""
import numpy as np
from photo_worker.algorithms.background import effective_alpha, solid, transparent


def render_holdout_background(source, target, alpha_out, style, *, subject=None,
                               confidence=0.0, depth=None, artifacts=None, check_active=lambda: None):
    check_active()
    background = style.extensions.background
    if background is None or background.mode == 'keep':
        return
    background = background.model_copy(deep=True)
    if background.mode not in ('solid', 'transparent', 'blur'):
        raise ValueError('HOLDOUT_COMPLETE_BACKGROUND_REQUIRED')
    h, w = target.shape[:2]
    if (source.alpha is not None and
            (not isinstance(source.alpha, np.ndarray) or
             source.alpha.shape != (h, w) or
             source.alpha.dtype not in (np.uint16, np.float32))):
        raise ValueError('HOLDOUT_BACKGROUND_ALPHA_INVALID')
    if (not isinstance(subject, np.ndarray) or subject.shape != (h, w)
            or subject.dtype != np.float32 or type(confidence) not in (float, int)
            or not .75 <= confidence <= 1
            or target.dtype != np.float32 or target.shape != source.rgb.shape
            or alpha_out.shape != (h, w) or alpha_out.dtype != np.float32
            or np.shares_memory(target, source.rgb)):
        raise ValueError('HOLDOUT_BACKGROUND_SUBJECT_REQUIRED')
    if background.mode == 'blur':
        if artifacts is None or depth is None:
            raise ValueError('HOLDOUT_COMPLETE_BACKGROUND_REQUIRED')
        from photo_worker.operations.background.prepare_blur import prepare_blur
        from photo_worker.runtime.arrays import read_array
        with prepare_blur(target, subject, depth, artifacts.scratch,
                strength=background.blur_strength*style.strength,
                check_active=check_active) as prepared:
            with read_array(prepared.rgb, target.shape, 'float32', check_active,
                            unit_interval=True) as result:
                for y in range(0,h,512):
                    check_active()
                    part = np.s_[y:y+512,:]
                    target[part] = result[part]
                    protected = source.protected[part] > 0
                    target[part][protected] = source.rgb[part][protected]
        check_active()
        return
    for y in range(0, h, 512):
        for x in range(0, w, 512):
            check_active()
            part = np.s_[y:y+512, x:x+512]
            coverage = effective_alpha(subject[part])
            original_alpha = (np.ones(coverage.shape, np.float32) if source.alpha is None else
                source.alpha[part].astype(np.float32) / np.float32(65535)
                if source.alpha.dtype == np.uint16 else source.alpha[part])
            if (not np.isfinite(original_alpha).all()
                    or np.any(original_alpha < 0) or np.any(original_alpha > 1)):
                raise ValueError('HOLDOUT_BACKGROUND_ALPHA_INVALID')
            coverage = coverage * original_alpha
            if background.mode == 'solid':
                target[part] = solid(target[part], coverage, background.solid_rgb)
                alpha_out[part] = 1
            else:
                target[part] = transparent(target[part], coverage)
                alpha_out[part] = coverage
            # Match final production protection: every positive value restores
            # original bits, rather than blending protected pixels.
            protected = source.protected[part] > 0
            target[part][protected] = source.rgb[part][protected]
            alpha_out[part][protected] = original_alpha[protected]
    check_active()
