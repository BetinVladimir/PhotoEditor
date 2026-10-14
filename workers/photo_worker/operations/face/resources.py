"""Conservative CPU working-set admission for full-frame face analysis."""


def required_face_analysis_ram(height: int, width: int, has_alpha: bool) -> int:
    if (type(height) is not int or type(width) is not int or type(has_alpha) is not bool
            or height < 1 or width < 1):
        raise ValueError('FACE_ANALYSIS_SIZE_INVALID')
    return 512 * 1024**2 + height * width * (160 + (32 if has_alpha else 0))
