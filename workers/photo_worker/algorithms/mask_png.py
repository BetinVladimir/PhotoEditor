"""Streaming grayscale PNG16 for probability masks, with a hard byte budget."""
from collections.abc import Callable
from typing import BinaryIO
import struct
import zlib
import numpy as np


def png16_bound(width:int,height:int)->int:
    if type(width) is not int or type(height) is not int or min(width,height)<1 or width*height>200000000:
        raise ValueError('Invalid mask dimensions')
    # Includes filter bytes, conservative deflate overhead and chunk headers.
    raw=height*(width*2+1)
    return raw+raw//50+1048576


def write_png16(mask:np.ndarray,output:BinaryIO,*,max_bytes:int,
                check_active:Callable[[],None]=lambda:None)->int:
    """Encode finite float[0,1] or uint16 2D, never an RGB color transform.

    Caller owns output and removes incomplete files after failure/cancellation.
    Probability quantization is round-to-nearest-even(value*65535), float64.
    """
    check_active()
    if mask.ndim!=2 or mask.dtype.kind!='f' and mask.dtype!=np.uint16:
        raise ValueError('Expected float probability or uint16 mask')
    h,w=mask.shape;png16_bound(w,h)
    if type(max_bytes) is not int or max_bytes<1:raise ValueError('Invalid byte budget')
    written=0
    def write(data):
        nonlocal written
        if written+len(data)>max_bytes:raise ValueError('PNG_BYTE_BUDGET_EXCEEDED')
        view=memoryview(data)
        while view:
            check_active();count=output.write(view)
            if type(count) is not int or count<1 or count>len(view):raise OSError('Invalid stream write')
            written+=count;view=view[count:]
    def chunk(kind,data):
        write(struct.pack('>I',len(data))+kind)
        write(data)
        write(struct.pack('>I',zlib.crc32(data,zlib.crc32(kind)) & 0xffffffff))
    def compressed(data):
        for start in range(0,len(data),65536):chunk(b'IDAT',data[start:start+65536])
    write(b'\x89PNG\r\n\x1a\n')
    chunk(b'IHDR',struct.pack('>IIBBBBB',w,h,16,0,0,0,0))
    compressor=zlib.compressobj(level=6)
    for row in range(h):
        check_active();compressed(compressor.compress(b'\0'))
        for start in range(0,w,16384):
            check_active();block=mask[row,start:start+16384]
            if mask.dtype.kind=='f':
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):
                    raise ValueError('Invalid probability mask')
                block=np.rint(block.astype(np.float64)*65535).astype('>u2')
            else:block=block.astype('>u2')
            compressed(compressor.compress(block.tobytes()))
    compressed(compressor.flush());chunk(b'IEND',b'');check_active()
    return written
