"""Bounded linear-luma registration proxies with explicit opacity support."""
from dataclasses import dataclass
import cv2
import numpy as np


@dataclass(frozen=True)
class MultiProxy:
    pixels: np.ndarray
    mask: np.ndarray


def linear_registration_proxy(rgb, alpha, *, luma, check_active=lambda: None):
    coefficients = np.asarray(luma, np.float64)
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
            or min(rgb.shape[:2]) < 8 or rgb.shape[0]*rgb.shape[1] > 200000000
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]
            or coefficients.shape != (3,) or not np.isfinite(coefficients).all()
            or np.any(coefficients < 0) or abs(float(coefficients.sum())-1) > 1e-6):
        raise ValueError('MULTI_PROXY_INVALID')
    height, width = alpha.shape
    for y in range(0, height, 256):
        for x in range(0, width, 256):
            check_active(); a = alpha[y:y+256, x:x+256]
            if not np.isfinite(rgb[y:y+256, x:x+256]).all() or not np.isfinite(a).all() or np.any(a < 0) or np.any(a > 1):
                raise ValueError('MULTI_PROXY_INVALID')
    scale = min(1., 2048/max(height, width)); ph, pw = max(8, round(height*scale)), max(8, round(width*scale))
    check_active()
    resized = cv2.resize(rgb, (pw, ph), interpolation=cv2.INTER_AREA) if (ph, pw) != (height, width) else rgb
    opacity = cv2.resize(alpha, (pw, ph), interpolation=cv2.INTER_AREA) if (ph, pw) != (height, width) else alpha
    gray = np.empty((ph, pw), np.uint8)
    for y in range(0, ph, 256):
        for x in range(0, pw, 256):
            check_active(); part = np.s_[y:y+256, x:x+256]
            signal = np.sum(resized[part].astype(np.float64)*coefficients, axis=-1)
            gray[part] = np.rint(np.clip(signal, 0, 1)*255).astype(np.uint8)
    # Exclude transparent borders and patches crossing an opacity boundary.
    visible = (opacity >= 1-1e-6).astype(np.uint8)*255
    mask = cv2.erode(visible, np.ones((63, 63), np.uint8), borderType=cv2.BORDER_CONSTANT, borderValue=0)
    gray[visible == 0] = 0
    gray.flags.writeable = False; mask.flags.writeable = False; check_active()
    return MultiProxy(gray, mask)
