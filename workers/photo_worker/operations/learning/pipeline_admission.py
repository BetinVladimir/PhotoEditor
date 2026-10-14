"""Require a complete installed holdout path before acquiring native models."""


def validate_holdout_pipeline(styles, *, reviewed_defect=False,
                              reviewed_dust=False, reviewed_detail=False,
                              reviewed_series=False):
    if sum((bool(reviewed_defect), bool(reviewed_dust),
            bool(reviewed_detail), bool(reviewed_series))) > 1:
        raise ValueError('HOLDOUT_EFFECT_EVIDENCE_CONFLICT')
    defects = tuple(style.defects for style in
                    (styles.baseline, styles.candidate, styles.approved))
    if reviewed_defect:
        if (not all(module.enabled for module in defects) or
                any(module != defects[0] for module in defects[1:]) or
                any(style.strength != styles.baseline.strength for style in
                    (styles.candidate, styles.approved)) or
                any(style.extensions.restoration is not None for style in
                    (styles.baseline, styles.candidate, styles.approved))):
            raise ValueError('HOLDOUT_DEFECT_REPLAY_BINDING_MISMATCH')
    elif any(module.enabled for module in defects):
        raise ValueError('HOLDOUT_DEFECT_EVIDENCE_REQUIRED')
    if reviewed_series:
        from .series_replay import _same_series_styles
        _same_series_styles(styles)
    for style in (styles.baseline, styles.candidate, styles.approved):
        extension = style.extensions
        if (style.geometry.enabled or
                any(value is not None and getattr(value, 'enabled', False)
                    for value in (extension.culling,
                                  extension.composition,
                                  extension.inpainting, extension.generation))):
            raise ValueError('HOLDOUT_RENDER_REQUIRES_EFFECT_PIPELINE')
        if (extension.series is not None and extension.series.enabled and
                not reviewed_series):
            raise ValueError('HOLDOUT_SERIES_EVIDENCE_REQUIRED')
        if (extension.portrait_detail is not None and
                extension.portrait_detail.enabled and not reviewed_detail):
            raise ValueError('HOLDOUT_DETAIL_EVIDENCE_REQUIRED')
        restoration = extension.restoration
        if reviewed_dust:
            if (restoration is None or restoration.upscale != 'off' or
                    restoration.denoise != 'off' or
                    restoration.sensor_dust != 'review'):
                raise ValueError('HOLDOUT_COMPLETE_RESTORATION_REQUIRED')
        elif restoration is not None and (restoration.upscale != 'off' or
                                          restoration.sensor_dust != 'off'):
            raise ValueError('HOLDOUT_COMPLETE_RESTORATION_REQUIRED')
        if extension.background is not None and extension.background.mode == 'generate':
            raise ValueError('HOLDOUT_RENDER_REQUIRES_EFFECT_PIPELINE')
    if reviewed_dust:
        # The same frozen dust operation must run on each comparison arm.
        # Check this before source and native-model acquisition; the replay
        # context repeats the check at its own trust boundary.
        from .dust_replay import _reviewed_dust_styles
        _reviewed_dust_styles(styles)
    if reviewed_detail:
        from .detail_replay import _same_detail_styles
        _same_detail_styles(styles)
