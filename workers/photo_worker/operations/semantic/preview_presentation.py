"""Qualified ICC display conversion; editing output retains its working pixels."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.operations.export.encode import _resize_rgba
from .exposure_color import local_exposure_icc_verifier


@dataclass(frozen=True)
class ExposureDisplay:
    rgb: np.ndarray
    alpha: np.ndarray | None
    profile: bytes
    profile_sha256: str


def prepare_exposure_display(image, rgb, alpha, *, width, height, icc_root,
        icc_lock_sha, srgb_sha, verify_runtime, check_active, max_bytes):
    """Reuse qualified linear/premultiplied resize then locked LCMS to sRGB.

    Display clipping matches existing media derivatives. This conversion never
    replaces full-range working output or its numerical verification evidence.
    """
    if not callable(verify_runtime) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INSTALLATION_REQUIRED')
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    shape=(image.canvas.height,image.canvas.width)
    if (type(width) is not int or type(height) is not int
            or not 1<=width<=min(shape[1],4096) or not 1<=height<=min(shape[0],4096)
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1
            or max_bytes<512*shape[0]*shape[1]+64*1024**2
            or not isinstance(icc_root,Path)
            or not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.shape!=(*shape,3)
            or (image.alpha is None)!=(alpha is None)
            or (alpha is not None and (not isinstance(alpha,np.ndarray)
                or alpha.dtype!=np.float32 or alpha.shape!=shape))):
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    def active():
        check_active()
        if verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_DISPLAY_UNQUALIFIED')
        check_active()
    active()
    lock=(icc_root/'profiles.lock.json').read_bytes()
    if sha256(lock).hexdigest()!=icc_lock_sha:
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_ICC_MISMATCH')
    registry=ICCRegistry(icc_root)
    if local_exposure_icc_verifier(registry)(image.color) is not True:
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_ICC_MISMATCH')
    source=registry.resolve(image.color.profile_id);target=registry.resolve('srgb_v1')
    if target.sha256!=srgb_sha or sha256(target.data).hexdigest()!=srgb_sha:
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_ICC_MISMATCH')
    for y in range(0,shape[0],256):
        active()
        values=rgb[y:y+256]
        if not np.isfinite(values).all() or np.any(values<0):
            raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
        if alpha is not None:
            values=alpha[y:y+256]
            if not np.isfinite(values).all() or np.any(values<0) or np.any(values>1):
                raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    resized,opacity=_resize_rgba(rgb,alpha,width,height,encoding=image.color.encoding)
    active()
    converted=LittleCMS().convert(np.round(np.clip(resized,0,1)*65535).astype(np.uint16),source.data,target.data)
    active()
    if (icc_root/'profiles.lock.json').read_bytes()!=lock:
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_ICC_MISMATCH')
    if converted.dtype!=np.uint16 or converted.shape!=(height,width,3):
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    return ExposureDisplay(np.array(converted,dtype=np.float32)/65535,
        np.array(opacity,dtype=np.float32,copy=True) if opacity is not None else None,
        bytes(target.data),target.sha256)


def write_exposure_display(path, display, *, srgb_sha, max_bytes, max_output_bytes,
        verify_runtime, check_active):
    """Lossless display PNG with exact ICC, alpha and stripped photo metadata."""
    from PIL import Image
    if (not isinstance(display,ExposureDisplay) or not isinstance(path,Path)
            or path.suffix!='.png' or not callable(verify_runtime) or not callable(check_active)
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1
            or type(max_output_bytes) is not int or not 1<=max_output_bytes<=2**53-1
            or not isinstance(display.profile,bytes) or not 1<=len(display.profile)<=1048576
            or display.profile_sha256!=srgb_sha or sha256(display.profile).hexdigest()!=srgb_sha):
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    rgb=display.rgb;alpha=display.alpha
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3
            or rgb.shape[2]!=3 or not 1<=rgb.shape[0]<=4096 or not 1<=rgb.shape[1]<=4096
            or (alpha is not None and (not isinstance(alpha,np.ndarray)
                or alpha.dtype!=np.float32 or alpha.shape!=rgb.shape[:2]))):
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    pixels=rgb.shape[0]*rgb.shape[1]
    if max_bytes<64*pixels+16*1024**2 or max_output_bytes<5*pixels+len(display.profile)+65536:
        raise ValueError('SEMANTIC_PREVIEW_DISPLAY_BUDGET')
    def active():
        check_active()
        if verify_runtime() is not True:raise ValueError('SEMANTIC_PREVIEW_DISPLAY_UNQUALIFIED')
        check_active()
    active()
    for y in range(rgb.shape[0]):
        check_active()
        for values in ([rgb[y]] if alpha is None else [rgb[y],alpha[y]]):
            if not np.isfinite(values).all() or np.any(values<0) or np.any(values>1):
                raise ValueError('SEMANTIC_PREVIEW_DISPLAY_INVALID')
    encoded=np.round(rgb*255).astype(np.uint8)
    if alpha is not None:encoded=np.dstack((encoded,np.round(alpha*255).astype(np.uint8)))
    mode='RGB' if alpha is None else 'RGBA'
    active()
    with Image.fromarray(encoded,mode=mode) as image:
        image.save(path,format='PNG',icc_profile=display.profile,compress_level=6,optimize=False)
    active()
    size=path.stat().st_size
    if not 1<=size<=max_output_bytes:raise ValueError('SEMANTIC_PREVIEW_DISPLAY_OUTPUT_INVALID')
    with Image.open(path) as image:
        if (image.format!='PNG' or image.mode!=mode or image.size!=(encoded.shape[1],encoded.shape[0])
                or set(image.info)!={'icc_profile'}
                or sha256(image.info['icc_profile']).hexdigest()!=srgb_sha):
            raise ValueError('SEMANTIC_PREVIEW_DISPLAY_OUTPUT_INVALID')
        decoded=np.asarray(image)
        for y in range(decoded.shape[0]):
            check_active()
            if not np.array_equal(decoded[y],encoded[y]):raise ValueError('SEMANTIC_PREVIEW_DISPLAY_OUTPUT_INVALID')
    active()
    return size
