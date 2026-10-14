"""Bounded verification of the worker's grayscale filter0 PNG16 contract."""
import os
import stat
import struct
import zlib

import numpy as np

from .mask_png import png16_bound


def verify_png16(mask, path, *, check_active=lambda: None):
    """Compare actual CRC-checked PNG samples with probability quantization.

    Reject unknown chunks, trailing bytes, extra compressed streams and pixels.
    Both compressed chunks and inflated buffers are bounded to 64 KiB.
    """
    check_active()
    if mask.ndim != 2 or mask.dtype != np.float32:
        raise ValueError('Invalid probability canvas')
    height, width = mask.shape
    bound = png16_bound(width, height)

    def decoded():
        inflater = zlib.decompressobj()
        seen = False
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        except OSError as exc:
            raise ValueError('Invalid PNG file') from exc
        try:
            metadata = os.fstat(fd)
            if (not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1
                    or metadata.st_size < 57 or metadata.st_size > bound):
                raise ValueError('Invalid PNG file')
        except BaseException:
            os.close(fd)
            raise
        with os.fdopen(fd, 'rb') as stream:
            def read(size):
                check_active()
                body = stream.read(size)
                if len(body) != size:
                    raise ValueError('Truncated PNG')
                return body
            if read(8) != b'\x89PNG\r\n\x1a\n':
                raise ValueError('Invalid PNG signature')
            first = True
            while True:
                size, kind = struct.unpack('>I4s', read(8))
                if size > 65536:
                    raise ValueError('PNG chunk exceeds worker bound')
                body = read(size)
                crc, = struct.unpack('>I', read(4))
                if crc != zlib.crc32(body, zlib.crc32(kind)) & 0xffffffff:
                    raise ValueError('Invalid PNG CRC')
                if first:
                    if kind != b'IHDR' or body != struct.pack('>IIBBBBB', width, height, 16, 0, 0, 0, 0):
                        raise ValueError('Invalid PNG canvas')
                    first = False
                elif kind == b'IDAT':
                    seen = True
                    pending = body
                    while pending:
                        check_active()
                        if inflater.eof:
                            raise ValueError('Extra PNG compressed stream')
                        try:
                            block = inflater.decompress(pending, 65536)
                        except zlib.error as exc:
                            raise ValueError('Invalid PNG compression') from exc
                        pending = inflater.unconsumed_tail
                        if inflater.unused_data:
                            raise ValueError('Trailing compressed PNG data')
                        if block:
                            yield block
                elif kind == b'IEND':
                    if body or not seen or not inflater.eof or stream.read(1):
                        raise ValueError('Incomplete or trailing PNG')
                    return
                else:
                    raise ValueError('Unexpected PNG chunk')

    blocks = decoded()
    pending = b''
    try:
        def compare(expected):
            nonlocal pending
            offset = 0
            while offset < len(expected):
                check_active()
                if not pending:
                    try:
                        pending = next(blocks)
                    except StopIteration as exc:
                        raise ValueError('Missing PNG pixels') from exc
                count = min(len(pending), len(expected) - offset)
                if pending[:count] != expected[offset:offset + count]:
                    raise ValueError('PNG differs from probability mask')
                pending = pending[count:]
                offset += count
        for row in range(height):
            compare(b'\0')
            for start in range(0, width, 16384):
                check_active()
                block = mask[row, start:start + 16384]
                if not np.isfinite(block).all() or np.any(block < 0) or np.any(block > 1):
                    raise ValueError('Invalid probability mask')
                compare(np.rint(block.astype(np.float64) * 65535).astype('>u2').tobytes())
        if pending or next(blocks, None) is not None:
            raise ValueError('Extra PNG pixels')
        check_active()
    finally:
        blocks.close()
