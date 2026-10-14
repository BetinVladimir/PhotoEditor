"""Physical export-mask qualification; canvas comes from resolved typed evidence."""
from hashlib import sha256
from pathlib import Path
import os
import re
import stat

from photo_worker.runtime.arrays import MAX_HEADER, MAX_PIXELS, read_array, read_alpha_array, read_inverse_field, read_lut


def qualify_export_mask_file(path, width, height, expected_bytes, expected_sha,
                             *, check_active=lambda: None):
    return qualify_export_array_file(path, 'MaskPixels', width, height,
        expected_bytes, expected_sha, dtype='float32', check_active=check_active)


def qualify_export_array_file(path, contract, width, height, expected_bytes, expected_sha,
                              *, dtype, check_active=lambda: None):
    masks = contract in ('MaskPixels', 'MaskArray')
    pixels = contract in ('WorkingPixels', 'PixelArray')
    alpha = contract == 'WorkingAlpha'
    warp = contract in ('DenseWarpField', 'DenseWarpArray')
    lut = contract == 'AbsoluteLUTArray'
    if (not (masks or pixels or alpha or warp or lut)
            or dtype not in ('float32', 'uint16')
            or (masks or warp or lut) and dtype != 'float32'):
        raise ValueError('INTERCHANGE_EXPORT_ARRAY_CONTRACT')
    if lut and (type(width) is not int or type(height) is not int or width not in (33, 65) or height != width):
        raise ValueError('INTERCHANGE_EXPORT_LUT_SIZE')
    channels = 3 * width if lut else 3 if pixels else 2 if warp else 1
    # Alpha allows the existing uint16/float32 encodings without conversion.
    maximum_item_bytes = 4 if alpha else 4 if dtype == 'float32' else 2
    if (not isinstance(path, Path) or not path.is_absolute()
            or type(width) is not int or type(height) is not int
            or width < 1 or height < 1 or width * height > MAX_PIXELS
            or type(expected_bytes) is not int
            or not 1 <= expected_bytes <= width * height * channels * maximum_item_bytes + MAX_HEADER + 12
            or not isinstance(expected_sha, str)
            or re.fullmatch('[0-9a-f]{64}', expected_sha) is None
            or not callable(check_active)):
        raise ValueError('INTERCHANGE_EXPORT_MASK_REQUEST')
    check_active()
    before = path.lstat()
    if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
            or before.st_size != expected_bytes):
        raise ValueError('INTERCHANGE_EXPORT_MASK_FILE')
    def pin(value):
        return (value.st_dev, value.st_ino, value.st_size,
                value.st_mtime_ns, value.st_ctime_ns)
    checksum = sha256()
    count = 0
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        if pin(os.fstat(stream.fileno())) != pin(before):
            raise ValueError('INTERCHANGE_EXPORT_MASK_CHANGED')
        while True:
            check_active()
            block = stream.read(262144)
            if not block:
                break
            count += len(block)
            if count > expected_bytes:
                raise ValueError('INTERCHANGE_EXPORT_MASK_SIZE')
            checksum.update(block)
    if count != expected_bytes or checksum.hexdigest() != expected_sha:
        raise ValueError('INTERCHANGE_EXPORT_MASK_HASH')
    shape = (height, width)
    if lut:
        decoder = read_lut(path, width, check_active)
    elif alpha:
        decoder = read_alpha_array(path, shape, check_active)
    elif warp:
        decoder = read_inverse_field(path, shape, check_active)
    else:
        decoder = read_array(path, (*shape, 3) if pixels else shape, dtype,
                             check_active, unit_interval=masks)
    with decoder as mapped:
        actual_dtype = str(mapped.dtype)
        if not alpha and actual_dtype != dtype:
            raise ValueError('INTERCHANGE_EXPORT_ARRAY_DTYPE')
        check_active()
    if pin(path.lstat()) != pin(before):
        raise ValueError('INTERCHANGE_EXPORT_MASK_CHANGED')
    check_active()
    # This is a physical receipt, not tenant/canvas authority or runtime approval.
    return {'sha256': expected_sha, 'bytes': expected_bytes,
            'width': width, 'height': height, 'dtype': actual_dtype}
