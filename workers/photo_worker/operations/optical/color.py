"""V2 optical transfer functions; legacy color pipelines are left unchanged."""
import numpy as np
from photo_worker.contracts.images import ColorSpace


def _rgb(value):
    if (not isinstance(value,np.ndarray) or value.dtype!=np.float32 or
            value.ndim!=3 or value.shape[-1]!=3 or not np.isfinite(value).all()):
        raise ValueError('OPTICAL_COLOR_INVALID')
    return value


def to_working_linear(rgb: np.ndarray, color: ColorSpace) -> np.ndarray:
    """Apply transfer only, retaining the input primaries and HDR range.

    Signed power extends gamma24 reversibly to negative RGB. This does not
    perform a Rec2020/sRGB gamut conversion or change an ICC identity. Caller
    processes bounded tiles and restores the same encoding before publishing.
    """
    rgb=_rgb(rgb)
    if color.encoding=='linear':return rgb.copy()
    if color.profile_id!='work_rec2020_gamma24_v1' or color.encoding!='gamma24':
        raise ValueError('OPTICAL_COLOR_UNSUPPORTED')
    with np.errstate(over='ignore',invalid='ignore'):
        output=np.copysign(np.power(np.abs(rgb.astype(np.float64)),2.4),rgb).astype(np.float32)
    if not np.isfinite(output).all():raise ValueError('OPTICAL_COLOR_OVERFLOW')
    return output


def from_working_linear(rgb: np.ndarray, color: ColorSpace) -> np.ndarray:
    rgb=_rgb(rgb)
    if color.encoding=='linear':return rgb.copy()
    if color.profile_id!='work_rec2020_gamma24_v1' or color.encoding!='gamma24':
        raise ValueError('OPTICAL_COLOR_UNSUPPORTED')
    output=np.copysign(np.power(np.abs(rgb.astype(np.float64)),1/2.4),rgb).astype(np.float32)
    if not np.isfinite(output).all():raise ValueError('OPTICAL_COLOR_OVERFLOW')
    return output
