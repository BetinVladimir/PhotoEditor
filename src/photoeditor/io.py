import hashlib
import io
import json
import os
from pathlib import Path
import uuid

import numpy as np
from PIL import Image, ImageCms, ImageOps
import tifffile

SRGB = ImageCms.ImageCmsProfile(ImageCms.createProfile('sRGB')).tobytes()


def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def hash_object(obj):
    return hashlib.sha256(json.dumps(obj, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def atomic_json(path, obj):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    try:
        tmp.write_text(json.dumps(obj, ensure_ascii=False, indent=2, allow_nan=False) + '\n')
        os.replace(tmp, path)
    finally:
        tmp.unlink(missing_ok=True)


def read_rgb(path, config):
    """sRGB uint8/uint16. Reject profiles that cannot be transformed without truncation."""
    warnings = []
    with Image.open(path) as im:
        w, h = im.size
        if w * h > config.runtime.max_megapixels * 1_000_000:
            raise ValueError('Image exceeds configured megapixel limit')
        icc = im.info.get('icc_profile')
        orientation = im.getexif().get(274, 1)
        if not icc:
            if config.input.missing_icc == 'reject':
                raise ValueError('Image has no ICC profile')
            warnings.append('missing_icc_assumed_srgb')
        if path.suffix.lower() in ('.tif', '.tiff'):
            with tifffile.TiffFile(path) as tf:
                if len(tf.pages) != 1:
                    raise ValueError('Multipage TIFF is not supported')
                page = tf.pages[0]
                if page.dtype == np.uint16:
                    if page.photometric != 2 or page.samplesperpixel != 3 or page.planarconfig != 1:
                        raise ValueError('16-bit TIFF requires interleaved RGB without alpha')
                    if icc:
                        profile = ImageCms.ImageCmsProfile(io.BytesIO(icc))
                        if 'srgb' not in ImageCms.getProfileName(profile).lower():
                            raise ValueError('16-bit non-sRGB TIFF is not supported without a high-bit ICC adapter')
                    arr = page.asarray()
                    # EXIF orientations, matching ImageOps.exif_transpose.
                    transforms = {2: lambda a: a[:, ::-1], 3: lambda a: a[::-1, ::-1],
                                  4: lambda a: a[::-1], 5: lambda a: a.transpose(1, 0, 2),
                                  6: lambda a: np.rot90(a, 3), 7: lambda a: a.transpose(1, 0, 2)[::-1, ::-1],
                                  8: lambda a: np.rot90(a)}
                    return np.ascontiguousarray(transforms.get(orientation, lambda a: a)(arr)), warnings
        if im.mode not in ('RGB', 'L', 'CMYK'):
            raise ValueError(f'Unsupported mode {im.mode}; alpha/HDR inputs require explicit conversion')
        im = ImageOps.exif_transpose(im)
        if icc:
            im = ImageCms.profileToProfile(im, ImageCms.ImageCmsProfile(io.BytesIO(icc)),
                                          ImageCms.ImageCmsProfile(io.BytesIO(SRGB)), outputMode='RGB')
        elif im.mode == 'CMYK':
            raise ValueError('Unprofiled CMYK is unsupported')
        else:
            im = im.convert('RGB')
        return np.asarray(im).copy(), warnings


def proxy_rgb(arr, edge):
    import cv2
    h, w = arr.shape[:2]
    scale = min(1., edge / max(h, w))
    if scale < 1:
        arr = cv2.resize(arr, (round(w*scale), round(h*scale)), interpolation=cv2.INTER_AREA)
    if arr.dtype == np.uint16:
        arr = np.round(arr / 257).astype(np.uint8)
    return arr


def write_jpeg(path, rgb, quality=95):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    try:
        Image.fromarray(rgb).save(tmp, format='JPEG', quality=quality, subsampling=0, icc_profile=SRGB)
        with Image.open(tmp) as im:
            im.verify()
        os.replace(tmp, path)
    finally:
        tmp.unlink(missing_ok=True)


def write_tiff(path, rgb16):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    try:
        tifffile.imwrite(tmp, rgb16, photometric='rgb', compression='deflate', predictor=True,
                         metadata=None, extratags=[(34675, 'B', len(SRGB), SRGB, False)])
        with tifffile.TiffFile(tmp) as tf:
            assert tf.pages[0].shape == rgb16.shape and tf.pages[0].dtype == np.uint16
        os.replace(tmp, path)
    finally:
        tmp.unlink(missing_ok=True)
