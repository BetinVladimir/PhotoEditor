from __future__ import annotations
from dataclasses import dataclass
import os
from pathlib import Path
from uuid import uuid4
from typing import Callable

import numpy as np

from photo_worker.color.icc import ICCRegistry,LittleCMS
from .inspect import open_raster,read_metadata
from .verify import AssetError,MAX_PIXELS,VerifiedSource,verify_size

@dataclass(frozen=True)
class WorkingImage:
    rgb_path: Path
    alpha_path: Path | None
    width: int
    height: int
    profile_id: str
    profile_sha256: str
    source_sha256: str
    orientation_applied: bool


def orient_once(image):
    # libvips autorot removes orientation metadata after applying it.
    return image.autorot()


def decode_raw(source: VerifiedSource) -> np.ndarray:
    import rawpy
    try:
        with rawpy.imread(str(source.path)) as raw:
            if raw.sizes.raw_width*raw.sizes.raw_height > MAX_PIXELS:
                raise AssetError('PIXEL_LIMIT_EXCEEDED')
            return raw.postprocess(use_camera_wb=True,use_auto_wb=False,no_auto_bright=True,
                                   gamma=(1,1),output_bps=16,output_color=rawpy.ColorSpace.sRGB,
                                   demosaic_algorithm=rawpy.DemosaicAlgorithm.AHD)
    except rawpy.LibRawError as exc:
        raise AssetError('RAW_UNSUPPORTED') from exc


def icc_to_work(source: VerifiedSource, output: Path, registry: ICCRegistry,
                assume_profile: str | None = None, *, analyze: bool = False, tile: int = 512,
                alpha_output: Path | None = None, check_active: Callable[[],None] = lambda:None) -> WorkingImage:
    if tile not in (256,512,1024):
        raise AssetError('TILE_SIZE_INVALID')
    check_active()
    metadata = read_metadata(source)
    if alpha_output is not None and (not metadata.has_alpha or alpha_output.parent!=output.parent or alpha_output==output):
        raise AssetError('ALPHA_OUTPUT_INVALID')
    if analyze and metadata.has_alpha:
        raise AssetError('ANALYZE_ALPHA_UNSUPPORTED')
    work = registry.resolve('work_rec2020_gamma24_v1')
    if source.format == 'raw':
        rgb = decode_raw(source)
        check_active()
        height,width = rgb.shape[:2]
        original_profile = registry.resolve('linear_srgb_v1').data
        image = None
    else:
        if metadata.icc is None and assume_profile is None:
            raise AssetError('INPUT_PROFILE_REQUIRED')
        original_profile = metadata.icc or registry.resolve(assume_profile).data
        image = orient_once(open_raster(source.path))
        width,height = image.width,image.height
        rgb = None
    if os.path.lexists(output):
        raise AssetError('OUTPUT_ALREADY_EXISTS')
    output.parent.mkdir(parents=True,exist_ok=True)
    scratch = output.parent
    required = width*height*(12+(2 if metadata.has_alpha else 0))+1048576
    space = os.statvfs(scratch)
    if space.f_bavail*space.f_frsize < required:
        raise AssetError('SCRATCH_EXHAUSTED')
    temporary = output.with_name(output.name+'.'+uuid4().hex+'.partial.npy')
    alpha_output = (alpha_output or output.with_name(output.stem+'.alpha.npy')) if metadata.has_alpha else None
    alpha_temp = temporary.with_name(temporary.name+'.alpha') if metadata.has_alpha else None
    if alpha_output and os.path.lexists(alpha_output):
        raise AssetError('OUTPUT_ALREADY_EXISTS')
    engine = LittleCMS()
    target = alpha = None
    linked: list[Path] = []
    try:
        # Exclusive private temporary files; numpy subsequently opens only our files.
        for path in (temporary,alpha_temp):
            if path is not None:
                fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
                os.close(fd)
        target = np.lib.format.open_memmap(temporary,mode='w+',dtype=np.float32,shape=(height,width,3))
        if alpha_temp:
            alpha = np.lib.format.open_memmap(alpha_temp,mode='w+',dtype=np.uint16,shape=(height,width))
        for y in range(0,height,tile):
            for x in range(0,width,tile):
                check_active()
                h,w = min(tile,height-y),min(tile,width-x)
                if image is not None:
                    crop = image.crop(x,y,w,h)
                    pixels = np.frombuffer(crop.write_to_memory(),dtype=np.uint8 if crop.format=='uchar' else np.uint16).reshape(h,w,crop.bands)
                    if crop.format=='uchar':
                        pixels = pixels.astype(np.uint16)*257
                else:
                    pixels = rgb[y:y+h,x:x+w]
                target[y:y+h,x:x+w] = engine.convert(pixels[...,:3],original_profile,work.data).astype(np.float32)/65535
                if alpha is not None:
                    alpha[y:y+h,x:x+w] = pixels[...,3]
        target.flush()
        if alpha is not None:
            alpha.flush()
        verify_size(source.path,source.bytes,source.sha256,check_active=check_active)
        check_active()
        # link is an atomic no-overwrite publication inside the attempt scratch dir.
        os.link(temporary,output);linked.append(output)
        if alpha_temp and alpha_output:
            os.link(alpha_temp,alpha_output);linked.append(alpha_output)
        return WorkingImage(output,alpha_output,width,height,work.id,work.sha256,source.sha256,True)
    except Exception:
        for path in linked:
            path.unlink(missing_ok=True)
        raise
    finally:
        if target is not None:
            target._mmap.close()
        if alpha is not None:
            alpha._mmap.close()
        temporary.unlink(missing_ok=True)
        if alpha_temp:
            alpha_temp.unlink(missing_ok=True)
