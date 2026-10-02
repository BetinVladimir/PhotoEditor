"""Bounded, exact-shape NPY mapping for immutable materialized artifacts."""
from __future__ import annotations
from contextlib import contextmanager
import os
from pathlib import Path
import stat
import struct
from typing import Callable,Iterator
import numpy as np

MAX_PIXELS=200000000
MAX_HEADER=4096

@contextmanager
def read_array(path:Path,shape:tuple[int,...],dtype:str,check_active:Callable[[],None],
               *,unit_interval:bool=False)->Iterator[np.memmap]:
    """Never unpickle, infer shapes, convert dtypes, or allocate a whole image.

    Caller owns the immutable attempt file and has verified its artifact SHA.
    Validate the NPY header and exact EOF before mapping; validate values using
    bounded row blocks. The mapping must not escape this context manager.
    """
    if (len(shape) not in (2,3) or any(type(n) is not int or n<1 for n in shape)
        or shape[0]*shape[1]>MAX_PIXELS or len(shape)==3 and shape[2]!=3):
        raise ValueError('ARRAY_SHAPE_INVALID')
    if dtype not in ('float32','uint16'):raise ValueError('ARRAY_DTYPE_INVALID')
    with _read_layout(path,shape,dtype,check_active,unit_interval) as mapped:
        yield mapped


@contextmanager
def read_lut(path:Path,size:int,check_active:Callable[[],None])->Iterator[np.memmap]:
    """Only absolute RGB_C float32 grids from resolved style artifacts."""
    if type(size) is not int or size not in (33,65):raise ValueError('LUT_SIZE_INVALID')
    with _read_layout(path,(size,size,size,3),'float32',check_active,True) as mapped:
        yield mapped


@contextmanager
def read_inverse_field(path:Path,shape:tuple[int,int],check_active:Callable[[],None])->Iterator[np.memmap]:
    """Absolute source pixel-center XY float32; coordinates may leave canvas."""
    if len(shape)!=2 or any(type(n) is not int or n<1 for n in shape) or shape[0]*shape[1]>MAX_PIXELS:
        raise ValueError('FIELD_SHAPE_INVALID')
    with _read_layout(path,(*shape,2),'float32',check_active,False) as mapped:
        yield mapped


@contextmanager
def _read_layout(path,shape,dtype,check_active,unit_interval):
    expected=np.dtype('<f4' if dtype=='float32' else '<u2')
    check_active()
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    mapped=None
    with os.fdopen(fd,'rb') as stream:
        metadata=os.fstat(stream.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink!=1:raise ValueError('ARRAY_FILE_INVALID')
        version=np.lib.format.read_magic(stream)
        length_bytes=2 if version==(1,0) else 4 if version==(2,0) else 0
        if not length_bytes:raise ValueError('ARRAY_VERSION_INVALID')
        length_raw=stream.read(length_bytes)
        if len(length_raw)!=length_bytes:raise ValueError('ARRAY_HEADER_INVALID')
        header_length=struct.unpack('<H' if length_bytes==2 else '<I',length_raw)[0]
        if not 1<=header_length<=MAX_HEADER:raise ValueError('ARRAY_HEADER_TOO_LARGE')
        stream.seek(-length_bytes,1)
        if version==(1,0):actual,fortran,stored=np.lib.format.read_array_header_1_0(stream,max_header_size=MAX_HEADER)
        elif version==(2,0):actual,fortran,stored=np.lib.format.read_array_header_2_0(stream,max_header_size=MAX_HEADER)
        else:raise ValueError('ARRAY_VERSION_INVALID')
        offset=stream.tell()
        if tuple(actual)!=shape or fortran or stored.str!=expected.str or stored.hasobject:
            raise ValueError('ARRAY_LAYOUT_INVALID')
        length=expected.itemsize
        for dimension in shape:length*=dimension
        if metadata.st_size!=offset+length:raise ValueError('ARRAY_SIZE_INVALID')
        try:
            mapped=np.memmap(stream,dtype=expected,mode='r',offset=offset,shape=shape,order='C')
            # Flat slices cap validation temporaries at 1 MiB even for a wide row.
            flat=mapped.reshape(-1)
            for start in range(0,flat.size,262144):
                check_active()
                block=flat[start:start+262144]
                if dtype=='float32' and not np.isfinite(block).all():raise ValueError('ARRAY_NONFINITE')
                if unit_interval and (np.any(block<0) or np.any(block>1)):
                    raise ValueError('ARRAY_RANGE_INVALID')
            check_active()
            yield mapped
        finally:
            if mapped is not None:mapped._mmap.close()


@contextmanager
def read_alpha_array(path:Path,shape:tuple[int,int],check_active:Callable[[],None])->Iterator[np.memmap]:
    """Strict legacy uint16 or v2 float32 opacity; no dtype conversion/inference.

    Both attempts validate the complete NPY layout. Only a layout mismatch may
    select the alternate encoding; corrupt headers, EOF and values still fail.
    """
    from contextlib import ExitStack
    with ExitStack() as stack:
        try:
            mapped=stack.enter_context(read_array(path,shape,'uint16',check_active))
        except ValueError as error:
            if str(error)!='ARRAY_LAYOUT_INVALID':raise
            mapped=stack.enter_context(read_array(path,shape,'float32',check_active,unit_interval=True))
        yield mapped
